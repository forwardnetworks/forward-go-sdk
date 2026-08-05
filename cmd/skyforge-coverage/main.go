package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type manifest struct {
	ConsumerTreeSHA256 string     `json:"consumer_go_sha256"`
	Endpoints          []endpoint `json:"endpoints"`
}

type endpoint struct {
	Method string `json:"method"`
	Route  string `json:"route"`
}

var (
	auditRowRE = regexp.MustCompile(`^\| ((?:F|D|T|X|E|S|A|G)[0-9]{3}) \|`)
	quotedRE   = regexp.MustCompile("`([^`]+)`")
)

func main() {
	auditPath := flag.String("audit", "", "Skyforge forward-api-sdk-migration-audit.md (required)")
	manifestPath := flag.String("manifest", "coverage_manifest.json", "SDK coverage manifest")
	skyforgeRoot := flag.String("skyforge", "", "Skyforge repository root (inferred from audit when omitted)")
	ignoreTree := flag.Bool("ignore-tree-drift", false, "compare routes but do not require the recorded consumer Go-tree hash")
	flag.Parse()
	if strings.TrimSpace(*auditPath) == "" {
		fatalf("-audit is required; missing inventory input is never skipped")
	}

	auditRoutes, rows, ids, err := parseAudit(*auditPath)
	must(err)
	if rows != 209 {
		fatalf("audit has %d inventory rows, want the evidenced baseline 209", rows)
	}
	if _, ok := ids["F001"]; !ok {
		fatalf("positive control F001 was not parsed from the audit")
	}
	if _, ok := auditRoutes["POST /api/snapshots/{snapshotId}"]; !ok {
		fatalf("positive control POST /api/snapshots/{snapshotId} was not derived")
	}
	// Correction found during implementation: this wired call post-dates or was
	// omitted from the 209-row audit. The audit correction cites both locations.
	auditRoutes["POST /api/users/{userId}/supported-orgs"] = struct{}{}

	data, err := os.ReadFile(*manifestPath)
	must(err)
	var sdk manifest
	must(json.Unmarshal(data, &sdk))
	manifestRoutes := map[string]struct{}{}
	for _, endpoint := range sdk.Endpoints {
		manifestRoutes[endpoint.Method+" "+endpoint.Route] = struct{}{}
	}
	missing := difference(auditRoutes, manifestRoutes)
	extra := difference(manifestRoutes, auditRoutes)
	fmt.Printf("audit_rows=%d audit_routes_with_correction=%d manifest_routes=%d\n", rows, len(auditRoutes), len(manifestRoutes))
	printDifference("missing_from_manifest", missing)
	printDifference("not_in_audit_inventory", extra)
	if len(missing) != 0 || len(extra) != 0 {
		os.Exit(1)
	}

	root := strings.TrimSpace(*skyforgeRoot)
	if root == "" {
		root = filepath.Dir(filepath.Dir(*auditPath))
	}
	if !*ignoreTree {
		hash, err := hashConsumerGo(root)
		must(err)
		fmt.Printf("consumer_go_sha256=%s\n", hash)
		if strings.TrimSpace(sdk.ConsumerTreeSHA256) == "" {
			fatalf("manifest has no consumer_go_sha256; tree drift would be silent")
		}
		if hash != sdk.ConsumerTreeSHA256 {
			fatalf("Skyforge production Go tree changed: manifest records %s", sdk.ConsumerTreeSHA256)
		}
	}
}

func parseAudit(path string) (map[string]struct{}, int, map[string]struct{}, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, err
	}
	defer file.Close()
	routes := map[string]struct{}{}
	ids := map[string]struct{}{}
	rows := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		match := auditRowRE.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		rows++
		ids[match[1]] = struct{}{}
		columns := strings.Split(line, "|")
		if len(columns) < 5 {
			return nil, 0, nil, fmt.Errorf("malformed audit row %s", match[1])
		}
		for _, quoted := range quotedRE.FindAllStringSubmatch(columns[3], -1) {
			for _, key := range normalizedKeys(quoted[1]) {
				routes[key] = struct{}{}
			}
		}
	}
	return routes, rows, ids, scanner.Err()
}

func normalizedKeys(value string) []string {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return nil
	}
	method, route := fields[0], fields[1]
	if index := strings.IndexAny(route, "?["); index >= 0 {
		route = route[:index]
	}
	route = strings.ReplaceAll(route, "{workspaceId}", "{networkId}")
	if method == "PATCH-or-PUT" {
		return []string{"PATCH " + route, "PUT " + route}
	}
	return []string{method + " " + route}
}

func hashConsumerGo(root string) (string, error) {
	server := filepath.Join(root, "components", "server")
	var files []string
	err := filepath.WalkDir(server, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".claude" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "encore.gen.go" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no production Go files found beneath %s", server)
	}
	sort.Strings(files)
	hash := sha256.New()
	for _, path := range files {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		hash.Write([]byte(filepath.ToSlash(relative)))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func difference(left, right map[string]struct{}) []string {
	var values []string
	for value := range left {
		if _, ok := right[value]; !ok {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	return values
}

func printDifference(label string, values []string) {
	for _, value := range values {
		fmt.Printf("%s: %s\n", label, value)
	}
}

func must(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
