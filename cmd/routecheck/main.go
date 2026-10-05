// Command routecheck checks that every route in coverage_manifest.json is mapped by a controller in Forward's own source
// at each pinned build. The manifest is the SDK's claim that a route exists; this is the check that the claim is true,
// done against the code that serves it rather than against a table somebody generated earlier.
//
//	go run ./cmd/routecheck -fwd ~/src/fwd -pins scripts/forward-pins -manifest coverage_manifest.json -allow scripts/routecheck-allow.txt
//
// It extracts the routes with a port of Skyforge's routes.py and security_routes.py (see extract.go), compares by method
// and path segment (a variable's name and regex are ignored), and exits non-zero when a manifest route is not mapped at
// some pin and not listed, with a reason, in the allow file. Routes that only a profile-gated controller serves are
// printed as information: they exist on some deployments, not all.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

const minMappings = 500 // fewer than this means the extractor, not Forward, is wrong (generate.sh's guard)

func main() {
	fwd := flag.String("fwd", "", "Forward source git directory (required)")
	pinsFile := flag.String("pins", "scripts/forward-pins", "file of name=commit lines")
	manifestFile := flag.String("manifest", "coverage_manifest.json", "route manifest")
	allowFile := flag.String("allow", "scripts/routecheck-allow.txt", "allow file; a missing file means no exceptions")
	flag.Parse()
	if *fwd == "" {
		fmt.Fprintln(os.Stderr, "routecheck: -fwd is required")
		os.Exit(2)
	}
	if err := run(*fwd, *pinsFile, *manifestFile, *allowFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(fwd, pinsFile, manifestFile, allowFile string) error {
	pins, err := readPins(pinsFile)
	if err != nil {
		return err
	}
	endpoints, err := readManifest(manifestFile)
	if err != nil {
		return err
	}
	var allow []allowEntry
	if text, err := os.ReadFile(allowFile); err == nil {
		if allow, err = parseAllow(string(text)); err != nil {
			return fmt.Errorf("%s: %w", allowFile, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	mappings := map[string][]Route{}
	for _, pin := range pins {
		resolved, sources, err := archiveSources(fwd, pin.commit)
		if err != nil {
			return err
		}
		routes, err := extractRoutes(sources)
		if err != nil {
			return err
		}
		if len(routes) < minMappings {
			return fmt.Errorf("routecheck: only %d mappings extracted at %s %s; refusing to check against them", len(routes), pin.name, resolved)
		}
		mappings[pin.name] = routes
		fmt.Printf("%s %s: %d mappings\n", pin.name, resolved[:12], len(routes))
	}
	report := check(endpoints, mappings, allow)
	fmt.Printf("%d manifest routes checked at %d pins\n", len(endpoints), len(pins))
	writeFindings(os.Stdout, "Served only by a profile-gated controller (not on every deployment)", report.Conditions)
	writeFindings(os.Stdout, "Not mapped, allowed", report.Allowed)
	for _, entry := range report.Unused {
		fmt.Printf("note: allow entry %s %s is no longer needed (every pin maps it): remove it\n", entry.method, entry.route)
	}
	if len(report.Missing) > 0 {
		writeFindings(os.Stderr, "NOT MAPPED by any controller at the pin", report.Missing)
		return fmt.Errorf("routecheck: %d manifest route(s) are not mapped at a pinned Forward build; fix the route, or list it in %s with a reason", len(report.Missing), allowFile)
	}
	fmt.Println("routecheck OK")
	return nil
}

type pin struct{ name, commit string }

func readPins(path string) ([]pin, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pins []pin
	for _, line := range strings.Split(string(text), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, commit, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || name == "" || commit == "" {
			return nil, fmt.Errorf("%s: %q is not name=commit", path, line)
		}
		pins = append(pins, pin{strings.TrimSpace(name), strings.TrimSpace(commit)})
	}
	if len(pins) == 0 {
		return nil, fmt.Errorf("%s lists no pins", path)
	}
	return pins, nil
}

func readManifest(path string) ([]Endpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Endpoints []struct {
			Method  string   `json:"method"`
			Route   string   `json:"route"`
			Symbols []string `json:"symbols"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(manifest.Endpoints) == 0 {
		return nil, fmt.Errorf("%s has no endpoints", path)
	}
	endpoints := make([]Endpoint, len(manifest.Endpoints))
	for i, e := range manifest.Endpoints {
		endpoints[i] = Endpoint{Method: e.Method, Route: e.Route, Symbols: e.Symbols}
	}
	return endpoints, nil
}
