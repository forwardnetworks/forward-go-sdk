package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// DiffsService wraps Forward's snapshot-to-snapshot diff family,
// GET /api/diffs/{snapshotAId}/{snapshotBId}/... (appserver DiffController).
// A is the OLDER snapshot, B the NEWER one. The cheap `?count` variants answer
// "did anything material change?" without pulling the diff bodies; the
// summary/detail variants follow the same paths without `count`.
type Diffs service

var diffKindPattern = regexp.MustCompile(`^[a-z0-9]+(?:[-/][a-z0-9]+)*$`)

func diffsPath(snapshotA, snapshotB string, tail string) (string, error) {
	snapshotA = strings.TrimSpace(snapshotA)
	snapshotB = strings.TrimSpace(snapshotB)
	if snapshotA == "" || snapshotB == "" {
		return "", errors.New("forward: both snapshot IDs are required for a diff")
	}
	return fmt.Sprintf("/api/diffs/%s/%s/%s", url.PathEscape(snapshotA), url.PathEscape(snapshotB), strings.TrimLeft(tail, "/")), nil
}

// Count returns the diff count for one family. routing-loop's path already
// ends in /count and takes no query. kind is a path tail, so it must be lowercase letters and digits separated by single dashes
// or slashes (every family in CountableDiffKinds is); anything else is refused before a request is sent. Count is for the
// families that answer ?count, plus routing-loop.
func (s *Diffs) Count(ctx context.Context, snapshotA, snapshotB string, kind ports.DiffKind) (*ports.DiffCount, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if kind == "" {
		return nil, nil, errors.New("forward: diff kind is required")
	}
	// The kind is a path tail that is not escaped (routing-loop/count has a slash), so anything that could end or reshape the
	// path -- a query, fragment, escape or space -- is refused rather than sent as a different request.
	if !diffKindPattern.MatchString(string(kind)) {
		return nil, nil, fmt.Errorf("forward: %q is not a valid diff kind (lowercase letters, digits, dash and slash)", kind)
	}
	path, err := diffsPath(snapshotA, snapshotB, string(kind))
	if err != nil {
		return nil, nil, err
	}
	if kind != ports.DiffRoutingLoop {
		path += "?count"
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.DiffCount)
	resp, err := s.client.Do(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// FilesCount returns the collected-files diff count with its per-type breakdown.
func (s *Diffs) FilesCount(ctx context.Context, snapshotA, snapshotB string) (*ports.FilesDiffCount, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := diffsPath(snapshotA, snapshotB, "files?count")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.FilesDiffCount)
	resp, err := s.client.Do(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// ChecksCount returns the per-check-type diff counts
// (GET /api/diffs/{a}/{b}/checks?counts), keyed by Forward's CheckType.
func (s *Diffs) ChecksCount(ctx context.Context, snapshotA, snapshotB string) (map[string]ports.DiffCount, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := diffsPath(snapshotA, snapshotB, "checks?counts")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]ports.DiffCount{}
	resp, err := s.client.Do(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Devices returns the full device inventory diff.
func (s *Diffs) Devices(ctx context.Context, snapshotA, snapshotB string) ([]ports.DiffEntry[ports.InventoryDeviceInfo], *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := diffsPath(snapshotA, snapshotB, "devices")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	var out []ports.DiffEntry[ports.InventoryDeviceInfo]
	resp, err := s.client.Do(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Checks returns the per-check diff rows of one check type between two
// snapshots (GET /api/diffs/{a}/{b}/checks?type=). checkType is a Forward
// CheckType such as PREDEFINED, EXISTS, ISOLATION, REACHABILITY or NQE. Bare
// arrays and {"checks": [...]} are both accepted.
func (s *Diffs) Checks(ctx context.Context, snapshotA, snapshotB, checkType string) ([]ports.CheckDiffEntry, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	checkType = strings.TrimSpace(checkType)
	if checkType == "" {
		return nil, nil, errors.New("forward: check type is required for a check diff")
	}
	path, err := diffsPath(snapshotA, snapshotB, "checks")
	if err != nil {
		return nil, nil, err
	}
	path += "?" + url.Values{"type": []string{checkType}}.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Diffs.Checks")
	result := listResponse[ports.CheckDiffEntry]{Keys: []string{"checks", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}
