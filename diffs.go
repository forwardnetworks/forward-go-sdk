package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// DiffsService wraps Forward's snapshot-to-snapshot diff family,
// GET /api/diffs/{snapshotAId}/{snapshotBId}/... (appserver DiffController).
// A is the OLDER snapshot, B the NEWER one. The cheap `?count` variants answer
// "did anything material change?" without pulling the diff bodies; the
// summary/detail variants follow the same paths without `count`.
type DiffsService service

// DiffCount is Forward's DiffCount: how many entries differ and whether the
// computation finished (an incomplete count is a lower bound).
type DiffCount struct {
	Count    int64 `json:"count"`
	Complete bool  `json:"complete"`
}

// FilesDiffCount is the files variant: device count plus a per-file-type
// breakdown (e.g. CONFIG vs collected show-command output). State files change
// on every collection (timers, counters), so callers that want "material"
// change read Types and ignore the state kinds.
type FilesDiffCount struct {
	Count    int64            `json:"count"`
	Complete bool             `json:"complete"`
	Types    map[string]int64 `json:"types"`
}

// DiffEntry is one added/removed/modified object in a full diff listing.
type DiffEntry[T any] struct {
	Type   string `json:"type"`
	Before *T     `json:"before,omitempty"`
	After  *T     `json:"after,omitempty"`
}

// InventoryDeviceInfo is the device record a devices diff carries. Fields are
// the subset every Forward version has; unknown fields are ignored on decode.
type InventoryDeviceInfo struct {
	Name     string `json:"name"`
	Vendor   string `json:"vendor,omitempty"`
	OS       string `json:"os,omitempty"`
	Model    string `json:"model,omitempty"`
	Type     string `json:"type,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// DiffKind names one countable diff family under /api/diffs.
type DiffKind string

const (
	DiffDevices      DiffKind = "devices"
	DiffInterfaces   DiffKind = "interfaces"
	DiffTopology     DiffKind = "topology"
	DiffL2           DiffKind = "l2"
	DiffACL          DiffKind = "acl"
	DiffCloudObjects DiffKind = "cloud-objects"
	DiffCloudACL     DiffKind = "cloud-acl"
	DiffRoutingLoop  DiffKind = "routing-loop/count"
)

// CountableDiffKinds is every family Count supports, in the order a caller
// would report them.
var CountableDiffKinds = []DiffKind{DiffDevices, DiffInterfaces, DiffTopology, DiffL2, DiffACL, DiffCloudObjects, DiffCloudACL, DiffRoutingLoop}

func diffsPath(snapshotA, snapshotB string, tail string) (string, error) {
	snapshotA = strings.TrimSpace(snapshotA)
	snapshotB = strings.TrimSpace(snapshotB)
	if snapshotA == "" || snapshotB == "" {
		return "", errors.New("forward: both snapshot IDs are required for a diff")
	}
	return fmt.Sprintf("/api/diffs/%s/%s/%s", url.PathEscape(snapshotA), url.PathEscape(snapshotB), strings.TrimLeft(tail, "/")), nil
}

// Count returns the diff count for one family. routing-loop's path already
// ends in /count and takes no query.
func (s *DiffsService) Count(ctx context.Context, snapshotA, snapshotB string, kind DiffKind) (*DiffCount, *Response, error) {
	if kind == "" {
		return nil, nil, errors.New("forward: diff kind is required")
	}
	path, err := diffsPath(snapshotA, snapshotB, string(kind))
	if err != nil {
		return nil, nil, err
	}
	if kind != DiffRoutingLoop {
		path += "?count"
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(DiffCount)
	resp, err := s.client.Do(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// FilesCount returns the collected-files diff count with its per-type breakdown.
func (s *DiffsService) FilesCount(ctx context.Context, snapshotA, snapshotB string) (*FilesDiffCount, *Response, error) {
	path, err := diffsPath(snapshotA, snapshotB, "files?count")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(FilesDiffCount)
	resp, err := s.client.Do(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// ChecksCount returns the per-check-type diff counts
// (GET /api/diffs/{a}/{b}/checks?counts), keyed by Forward's CheckType.
func (s *DiffsService) ChecksCount(ctx context.Context, snapshotA, snapshotB string) (map[string]DiffCount, *Response, error) {
	path, err := diffsPath(snapshotA, snapshotB, "checks?counts")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]DiffCount{}
	resp, err := s.client.Do(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Devices returns the full device inventory diff.
func (s *DiffsService) Devices(ctx context.Context, snapshotA, snapshotB string) ([]DiffEntry[InventoryDeviceInfo], *Response, error) {
	path, err := diffsPath(snapshotA, snapshotB, "devices")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	var out []DiffEntry[InventoryDeviceInfo]
	resp, err := s.client.Do(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// MaterialDiffSummary is the answer to "is this snapshot worth looking at
// versus that one": per-family counts with state-file churn excluded.
type MaterialDiffSummary struct {
	SnapshotA  string            `json:"snapshotA"`
	SnapshotB  string            `json:"snapshotB"`
	Counts     map[string]int64  `json:"counts"`          // family -> count (families with 0 omitted)
	Files      map[string]int64  `json:"files,omitempty"` // file type -> devices changed (config kinds only)
	Checks     map[string]int64  `json:"checks,omitempty"`
	Incomplete []string          `json:"incomplete,omitempty"` // families whose count was a lower bound
	Errors     map[string]string `json:"errors,omitempty"`     // families that could not be computed
}

// Material reports whether anything in the summary is a real change.
func (m MaterialDiffSummary) Material() bool {
	for _, n := range m.Counts {
		if n > 0 {
			return true
		}
	}
	for _, n := range m.Files {
		if n > 0 {
			return true
		}
	}
	for _, n := range m.Checks {
		if n > 0 {
			return true
		}
	}
	return false
}

// superficialFileTypes are collected-file kinds that change on every
// collection without the network having changed (show-command output with
// uptimes, counters, timestamps). Anything else (CONFIG and friends) counts.
var superficialFileTypes = map[string]bool{
	"SHOW":        true,
	"SHOW_OUTPUT": true,
	"STATE":       true,
	"COLLECTED":   true,
	"OPERATIONAL": true,
	"PERFORMANCE": true,
	"COMMAND":     true,
	"CLI":         true,
	"SNMP":        true,
}

// MaterialSummary computes the material diff between two snapshots with one
// count call per family. A family that errors is reported, not fatal: the
// caller decides whether an unknowable family counts as "changed".
func (s *DiffsService) MaterialSummary(ctx context.Context, snapshotA, snapshotB string) (MaterialDiffSummary, error) {
	out := MaterialDiffSummary{SnapshotA: strings.TrimSpace(snapshotA), SnapshotB: strings.TrimSpace(snapshotB), Counts: map[string]int64{}}
	if out.SnapshotA == "" || out.SnapshotB == "" {
		return out, errors.New("forward: both snapshot IDs are required for a diff")
	}
	for _, kind := range CountableDiffKinds {
		c, _, err := s.Count(ctx, snapshotA, snapshotB, kind)
		if err != nil {
			if out.Errors == nil {
				out.Errors = map[string]string{}
			}
			out.Errors[string(kind)] = err.Error()
			continue
		}
		if c.Count > 0 {
			out.Counts[string(kind)] = c.Count
		}
		if !c.Complete {
			out.Incomplete = append(out.Incomplete, string(kind))
		}
	}
	if fc, _, err := s.FilesCount(ctx, snapshotA, snapshotB); err != nil {
		if out.Errors == nil {
			out.Errors = map[string]string{}
		}
		out.Errors["files"] = err.Error()
	} else {
		for typ, n := range fc.Types {
			if n > 0 && !superficialFileTypes[strings.ToUpper(strings.TrimSpace(typ))] {
				if out.Files == nil {
					out.Files = map[string]int64{}
				}
				out.Files[typ] = n
			}
		}
		if !fc.Complete {
			out.Incomplete = append(out.Incomplete, "files")
		}
	}
	if cc, _, err := s.ChecksCount(ctx, snapshotA, snapshotB); err != nil {
		if out.Errors == nil {
			out.Errors = map[string]string{}
		}
		out.Errors["checks"] = err.Error()
	} else {
		for typ, c := range cc {
			if c.Count > 0 {
				if out.Checks == nil {
					out.Checks = map[string]int64{}
				}
				out.Checks[typ] = c.Count
			}
		}
	}
	return out, nil
}
