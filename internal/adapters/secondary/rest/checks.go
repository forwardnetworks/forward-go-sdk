package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ChecksService manages checks attached to snapshots. Checks are not created on
// a network resource: they attach to one real snapshot. When created with
// CreatePersistent, Forward carries them onto later real and predicted snapshots
// of that network.
type Checks service

const checkPrimeAttempts = 4

// List returns checks evaluated on snapshotID, optionally filtered. The first GET primes Forward's
// evaluation cache and can outlive an http.Client deadline while computation
// continues server-side, so client-side timeout failures are re-issued up to
// four times. Cancellation of the caller's context is never retried.
func (s *Checks) List(
	ctx context.Context,
	snapshotID string,
	options ...ports.CheckListOptions,
) ([]ports.Check, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if len(options) > 1 {
		return nil, nil, errors.New("forward: at most one check list option set is allowed")
	}
	if err := s.client.requireCapability(ports.CapabilityPersistentSnapshotChecks); err != nil {
		return nil, nil, err
	}
	path, err := checksPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	if len(options) == 1 {
		if query := checkListOptionsQuery(options[0]); len(query) != 0 {
			path += "?" + query.Encode()
		}
	}
	var lastResponse *ports.Response
	for attempt := 1; attempt <= checkPrimeAttempts; attempt++ {
		req, requestErr := s.client.NewRequest(ctx, http.MethodGet, path, nil)
		if requestErr != nil {
			return nil, nil, requestErr
		}
		result := listResponse[ports.Check]{Keys: []string{"checks", "items"}}
		response, requestErr := s.client.Do(req, &result)
		lastResponse = response
		if requestErr == nil {
			s.client.observeCapability(ports.CapabilityPersistentSnapshotChecks)
			return result.Items, response, nil
		}
		if ctx.Err() != nil || attempt == checkPrimeAttempts || !isClientTimeout(requestErr) {
			return nil, response, requestErr
		}
	}
	return nil, lastResponse, errors.New("forward: unreachable check retry state")
}

// CreatePersistent creates a check on snapshotID with persistent=true. The
// snapshot must be a real snapshot; a successful check is inherited and
// evaluated by later snapshots of that same network, including Predict output.
func (s *Checks) CreatePersistent(ctx context.Context, snapshotID string, check ports.NewCheck) (ports.Identifier, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	if err := s.client.requireCapability(ports.CapabilityPersistentSnapshotChecks); err != nil {
		return "", nil, err
	}
	path, err := checksPath(snapshotID)
	if err != nil {
		return "", nil, err
	}
	if check.Definition == nil {
		return "", nil, errors.New("forward: check definition is required")
	}
	path += "?persistent=true"
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, check)
	if err != nil {
		return "", nil, err
	}
	var created struct {
		ID ports.Identifier `json:"id"`
	}
	response, err := s.client.Do(req, &created)
	if err != nil {
		return "", response, err
	}
	if created.ID == "" {
		return "", response, errors.New("forward: persistent check create returned no ID")
	}
	s.client.observeCapability(ports.CapabilityPersistentSnapshotChecks)
	return created.ID, response, nil
}

func checksPath(snapshotID string) (string, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required")
	}
	return "/api/snapshots/" + url.PathEscape(snapshotID) + "/checks", nil
}

func isClientTimeout(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || isPermanent(err) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// Get returns one check with its diagnosis.
func (s *Checks) Get(ctx context.Context, snapshotID, checkID string) (*ports.CheckDetail, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := checkPath(snapshotID, checkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	detail := new(ports.CheckDetail)
	resp, err := s.client.Do(req, detail)
	return detail, resp, err
}

// Create adds a check to snapshotID and returns it as evaluated.
//
// A persistent check is inherited and re-evaluated by later snapshots of the
// same network, including Predict output; a non-persistent one belongs to this
// snapshot alone. persistent is a pointer so that leaving it unstated defers
// to whatever the appserver defaults to, rather than asserting a default here.
func (s *Checks) Create(
	ctx context.Context,
	snapshotID string,
	check ports.NewCheck,
	persistent *bool,
) (*ports.CheckDetail, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.client.requireCapability(ports.CapabilityPersistentSnapshotChecks); err != nil {
		return nil, nil, err
	}
	path, err := checksPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	if check.Definition == nil {
		return nil, nil, errors.New("forward: check definition is required")
	}
	if persistent != nil {
		path += "?persistent=" + strconv.FormatBool(*persistent)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, check)
	if err != nil {
		return nil, nil, err
	}
	created := new(ports.CheckDetail)
	resp, err := s.client.Do(req, created)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(ports.CapabilityPersistentSnapshotChecks)
	return created, resp, nil
}

// Deactivate disables one check on a snapshot.
//
// Forward deactivates rather than deletes: the check stops evaluating but its
// history stays readable, so a later snapshot can still be explained.
func (s *Checks) Deactivate(ctx context.Context, snapshotID, checkID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := checkPath(snapshotID, checkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// DeactivateAll disables every check on a snapshot.
func (s *Checks) DeactivateAll(ctx context.Context, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := checksPath(snapshotID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func checkPath(snapshotID, checkID string) (string, error) {
	base, err := checksPath(snapshotID)
	if err != nil {
		return "", err
	}
	if checkID = strings.TrimSpace(checkID); checkID == "" {
		return "", errors.New("forward: check ID is required")
	}
	return base + "/" + url.PathEscape(checkID), nil
}

// ListPredefined returns the built-in checks this appserver offers. GET
// /api/predefinedChecks (getAvailablePredefinedChecks, published;
// CheckController on primary 15398425a69 and stable 67e89c87124). It needs the
// VIEW_PREDEFINED_CHECKS org permission.
func (s *Checks) ListPredefined(ctx context.Context) ([]ports.PredefinedCheck, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/predefinedChecks", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Checks.ListPredefined")
	var out []ports.PredefinedCheck
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

func checkListOptionsQuery(o ports.CheckListOptions) url.Values {
	query := url.Values{}
	for key, values := range map[string][]string{
		"type": o.Types, "status": o.Statuses, "priority": o.Priorities,
	} {
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				query.Add(key, value)
			}
		}
	}
	return query
}
