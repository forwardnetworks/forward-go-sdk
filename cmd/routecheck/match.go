package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// segments splits a route into path segments with every {variable}, including a {name:regex} one, reduced to "{}": the
// variable's name and pattern do not matter, only that a variable sits there.
func segments(route string) []string {
	var normalized strings.Builder
	depth := 0
	for _, r := range route {
		switch {
		case r == '{':
			if depth == 0 {
				normalized.WriteString("{}")
			}
			depth++
		case r == '}':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			normalized.WriteRune(r)
		}
	}
	var out []string
	for _, segment := range strings.Split(normalized.String(), "/") {
		if segment != "" {
			out = append(out, segment)
		}
	}
	return out
}

// matches reports whether a mapped route can serve what the manifest claims. A manifest literal may be served by a
// mapped {variable} (the manifest names /roles/org/ADMIN where Forward maps /roles/org/{role}); a manifest {variable}
// needs a mapped one, because a mapped literal alone does not show the variable route exists. ANY matches any method.
func matches(manifestMethod, manifestRoute string, mapped Route) bool {
	if mapped.Method != "ANY" && !strings.EqualFold(mapped.Method, manifestMethod) {
		return false
	}
	want, have := segments(manifestRoute), segments(mapped.Path)
	if len(want) != len(have) {
		return false
	}
	for i := range want {
		if have[i] == "{}" {
			continue
		}
		if want[i] == "{}" || want[i] != have[i] {
			return false
		}
	}
	return true
}

// Endpoint is one manifest entry.
type Endpoint struct {
	Method, Route string
	Symbols       []string
}

// allowEntry is one line of the allow file: a manifest route that is knowingly not mapped at some pins.
type allowEntry struct {
	method, route string
	pins          []string // empty: every pin
	reason        string
}

func (a allowEntry) covers(method, route, pin string) bool {
	if !strings.EqualFold(a.method, method) || a.route != route {
		return false
	}
	if len(a.pins) == 0 {
		return true
	}
	for _, p := range a.pins {
		if p == pin {
			return true
		}
	}
	return false
}

// parseAllow reads "METHOD /route [pin=a,b]  # reason" lines. A reason is required: an exception nobody can explain is a
// route that was never checked.
func parseAllow(text string) ([]allowEntry, error) {
	var entries []allowEntry
	for n, raw := range strings.Split(text, "\n") {
		line, reason, _ := strings.Cut(raw, "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 2 || len(fields) > 3 {
			return nil, fmt.Errorf("allow file line %d: want METHOD /route [pin=NAME[,NAME]] # reason", n+1)
		}
		entry := allowEntry{method: strings.ToUpper(fields[0]), route: fields[1], reason: strings.TrimSpace(reason)}
		if len(fields) == 3 {
			names, ok := strings.CutPrefix(fields[2], "pin=")
			if !ok {
				return nil, fmt.Errorf("allow file line %d: %q is not pin=NAME[,NAME]", n+1, fields[2])
			}
			entry.pins = strings.Split(names, ",")
		}
		if entry.reason == "" {
			return nil, fmt.Errorf("allow file line %d: every exception needs a # reason", n+1)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Finding is one problem or note about a manifest route at a pin.
type Finding struct {
	Pin, Method, Route string
	Symbols            []string
	Reason             string
}

// Report is the result of checking a manifest against the mappings at each pin.
type Report struct {
	Missing    []Finding // not mapped at a pin and not allow-listed: a failure
	Allowed    []Finding // not mapped, allow-listed with a reason
	Conditions []Finding // mapped only by a controller a deployment profile gates: informational
	Unused     []allowEntry
}

// check compares every manifest endpoint with the mappings of every pin. mappings is keyed by pin name.
func check(endpoints []Endpoint, mappings map[string][]Route, allow []allowEntry) Report {
	var report Report
	used := make([]bool, len(allow))
	pins := make([]string, 0, len(mappings))
	for pin := range mappings {
		pins = append(pins, pin)
	}
	sort.Strings(pins)
	for _, endpoint := range endpoints {
		for _, pin := range pins {
			var hits []Route
			for _, route := range mappings[pin] {
				if matches(endpoint.Method, endpoint.Route, route) {
					hits = append(hits, route)
				}
			}
			finding := Finding{Pin: pin, Method: endpoint.Method, Route: endpoint.Route, Symbols: endpoint.Symbols}
			if len(hits) == 0 {
				allowed := false
				for i, entry := range allow {
					if entry.covers(endpoint.Method, endpoint.Route, pin) {
						used[i], allowed = true, true
						finding.Reason = entry.reason
					}
				}
				if allowed {
					report.Allowed = append(report.Allowed, finding)
				} else {
					report.Missing = append(report.Missing, finding)
				}
				continue
			}
			gated := true
			var profile string
			for _, hit := range hits {
				if hit.Profile == "" {
					gated = false
					break
				}
				profile = hit.Profile
			}
			if gated {
				finding.Reason = profile
				report.Conditions = append(report.Conditions, finding)
			}
		}
	}
	for i, entry := range allow {
		if !used[i] {
			report.Unused = append(report.Unused, entry)
		}
	}
	return report
}

func writeFindings(w *os.File, title string, findings []Finding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(w, "%s (%d)\n", title, len(findings))
	for _, f := range findings {
		line := fmt.Sprintf("  [%s] %s %s", f.Pin, f.Method, f.Route)
		if len(f.Symbols) > 0 {
			line += "  <- " + strings.Join(f.Symbols, ", ")
		}
		if f.Reason != "" {
			line += "  :: " + f.Reason
		}
		fmt.Fprintln(w, line)
	}
}
