package usecases

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"strings"
)

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
func (s *Diffs) MaterialSummary(ctx context.Context, snapshotA, snapshotB string) (domain.MaterialDiffSummary, error) {
	if err := s.failClosed(); err != nil {
		return domain.MaterialDiffSummary{}, err
	}
	out := domain.MaterialDiffSummary{SnapshotA: strings.TrimSpace(snapshotA), SnapshotB: strings.TrimSpace(snapshotB), Counts: map[string]int64{}}
	if out.SnapshotA == "" || out.SnapshotB == "" {
		return out, errors.New("forward: both snapshot IDs are required for a diff")
	}
	for _, kind := range domain.CountableDiffKinds {
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
