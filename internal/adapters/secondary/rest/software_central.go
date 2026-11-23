package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SoftwareCentralService is the Forward Software Central surface: the catalog
// of installable and upgradable Forward software (appliance OVAs, upgrade
// bundles, air-gap packages) that Forward publishes from fwd.app.
//
// WHERE THIS LIVES, AND WHY IT IS NOT THE APPLIANCE. Every other service in
// this client talks to a Forward APPSERVER -- a customer's own instance.
// Software Central is served by DeploymentController under @Profile(SAAS):
// it only exists on Forward's hosted fwd.app, it authenticates a Software
// Central account with the same HTTP basic auth as everything else, and it
// refuses (403, "SOFTWARE_CENTRAL is not enabled for organization N") when
// the org holds no Software Central entitlement. Construct the client against
// https://fwd.app with the Software Central credential, not against an
// on-prem base URL; an on-prem appserver 404s these routes.
//
// TWO WAYS TO FETCH, ONE OF THEM CLOSED. The artifact route answers in two
// forms:
//
//   - GET /api/deployment-artifacts/{id}?versionId=V  -> 302 to the file.
//   - GET /api/deployment-artifacts/{id}?versionId=V&as=url -> {"url": "..."}.
//
// The 302 lands on a presigned object-store URL on ANOTHER HOST. This client's
// redirect policy (secureRedirects in client.go) deliberately refuses to
// follow cross-origin redirects so a Forward credential can never be replayed
// to a third party, which means the 302 form surfaces here as an API error
// carrying the 3xx. This service therefore only ever asks for `as=url` and
// fetches the returned URL in a separate, UNAUTHENTICATED request
// (DownloadDeploymentArtifact). Do not "fix" this by relaxing the redirect
// policy; the policy is the point.
//
// Unauthenticated calls come back as HTTP 401 with an EMPTY body (verified
// against fwd.app 2026-09-11). newErrorResponse classifies a 401 as
// ErrAuthentication regardless of body, so errors.Is(err, ErrAuthentication)
// holds even though there is no message to read.
type SoftwareCentral service

// ListDeploymentArtifacts reads the catalog. deploymentType narrows it to
// artifacts deployable on that platform; the empty value lists everything.
func (s *SoftwareCentral) ListDeploymentArtifacts(ctx context.Context, deploymentType ports.DeploymentType) ([]ports.DeploymentArtifact, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path := "/api/deployment-artifacts"
	if value := strings.TrimSpace(string(deploymentType)); value != "" {
		path += "?type=" + url.QueryEscape(value)
	}
	result := listResponse[ports.DeploymentArtifact]{Keys: []string{"artifacts", "deploymentArtifacts", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SoftwareCentral.ListDeploymentArtifacts")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// DeploymentArtifactURL resolves the download URL for one artifact version
// using the `as=url` form (see the service comment for why the 302 form is
// unusable here). An empty versionID asks the server for its latest version.
//
// The URL is presigned and short-lived: resolve it right before the download,
// not at planning time.
func (s *SoftwareCentral) DeploymentArtifactURL(ctx context.Context, artifactID, versionID ports.Identifier) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	id := strings.TrimSpace(artifactID.String())
	if id == "" {
		return "", nil, ports.ErrDeploymentArtifactIDRequired
	}
	query := url.Values{"as": []string{"url"}}
	if version := strings.TrimSpace(versionID.String()); version != "" {
		query.Set("versionId", version)
	}
	path := "/api/deployment-artifacts/" + url.PathEscape(id) + "?" + query.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", nil, err
	}
	req = markOperation(req, "SoftwareCentral.DeploymentArtifactURL")
	var out struct {
		URL string `json:"url"`
	}
	response, err := s.client.doRequired(req, &out)
	if err != nil {
		return "", response, err
	}
	if strings.TrimSpace(out.URL) == "" {
		// A 200 with no url is a shape we do not understand; saying so beats
		// handing back "" for the caller to GET.
		return "", response, errors.New("forward: deployment artifact URL response carried no url")
	}
	return out.URL, response, nil
}

// downloadErrorBodyLimit bounds how much of a file host's error body is
// quoted back. Object stores answer refusals with a short XML document; a
// kilobyte is enough to see "AccessDenied" or "Request has expired" and not
// enough to paste a whole HTML error page into a log line.
const downloadErrorBodyLimit = 1024

// DownloadDeploymentArtifact resolves the artifact's URL and streams the file
// into w, returning the bytes written.
//
// THE SECOND REQUEST CARRIES NO FORWARD CREDENTIAL. The resolved URL points
// at a presigned object-store location on a host that is not Forward. The
// authorisation is IN the URL (the signature in its query string); the
// Forward basic-auth header is neither needed nor wanted there, and sending
// it would replay a Forward credential to a third party -- exactly the leak
// secureRedirects exists to prevent on the 302 path. So this method does not
// go through NewRequest (which would set the header, and which refuses
// absolute URLs for the same reason); it builds a bare request and sends it
// through the client's transport directly.
//
// The transport's redirect policy still applies: any redirect the file host
// issues to a different origin than Forward is refused and surfaces as a
// non-2xx error here. Object stores serve presigned objects without
// redirecting, so in practice this never triggers.
//
// The client's request timeout does NOT apply: an appliance OVA is gigabytes
// and the default 60s would cut every real download short. Cancellation is
// the caller's ctx.
func (s *SoftwareCentral) DownloadDeploymentArtifact(ctx context.Context, artifactID, versionID ports.Identifier, w io.Writer) (int64, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return 0, err
	}
	if w == nil {
		return 0, errors.New("forward: download destination is nil")
	}
	location, _, err := s.DeploymentArtifactURL(ctx, artifactID, versionID)
	if err != nil {
		return 0, err
	}
	target, err := url.Parse(location)
	if err != nil {
		return 0, fmt.Errorf("forward: parse deployment artifact URL: %w", err)
	}
	if target.Scheme != "http" && target.Scheme != "https" || target.Host == "" {
		return 0, fmt.Errorf("forward: deployment artifact URL %q is not an absolute http(s) URL", redactedURL(target))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("forward: create download request: %w", err)
	}
	req.Header.Set("User-Agent", s.client.userAgent)
	// Deliberately no SetBasicAuth and no Accept: application/json -- this is
	// a file, from a host that is not Forward.

	httpClient := *s.client.httpClient
	httpClient.Timeout = 0
	// The Forward cookie jar (browser mode) must not travel to the file host
	// either.
	httpClient.Jar = nil

	const operation = "SoftwareCentral.DownloadDeploymentArtifact"
	started := time.Now()
	s.client.emit(ctx, Event{Type: EventRequest, Method: req.Method, Path: target.Path, Operation: operation, AuthMode: ports.AuthModeNone})
	resp, err := httpClient.Do(req)
	if err != nil {
		s.client.emit(ctx, Event{Type: EventResponse, Method: req.Method, Path: target.Path, Duration: time.Since(started), Err: errors.New("forward: transport error"), Operation: operation, AuthMode: ports.AuthModeNone})
		return 0, fmt.Errorf("forward: download deployment artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, downloadErrorBodyLimit))
		s.client.emit(ctx, Event{Type: EventResponse, Method: req.Method, Path: target.Path, StatusCode: resp.StatusCode, Duration: time.Since(started), Err: fmt.Errorf("forward: HTTP status %d", resp.StatusCode), Operation: operation, AuthMode: ports.AuthModeNone})
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			return 0, fmt.Errorf("forward: deployment artifact host returned %s", resp.Status)
		}
		return 0, fmt.Errorf("forward: deployment artifact host returned %s: %s", resp.Status, detail)
	}

	n, err := io.Copy(w, resp.Body)
	if err != nil {
		s.client.emit(ctx, Event{Type: EventResponse, Method: req.Method, Path: target.Path, StatusCode: resp.StatusCode, Duration: time.Since(started), Err: errors.New("forward: response copy error"), Operation: operation, AuthMode: ports.AuthModeNone})
		return n, fmt.Errorf("forward: download deployment artifact: %w", err)
	}
	s.client.emit(ctx, Event{Type: EventResponse, Method: req.Method, Path: target.Path, StatusCode: resp.StatusCode, Duration: time.Since(started), Operation: operation, AuthMode: ports.AuthModeNone})
	return n, nil
}

// redactedURL renders a URL for an error message without its query string,
// which on a presigned URL is the credential.
func redactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	clone.RawQuery = ""
	clone.Fragment = ""
	return clone.String()
}
