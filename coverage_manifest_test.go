package forward

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

type coverageManifest struct {
	SchemaVersion        int                `json:"schema_version"`
	SemanticCallSites    int                `json:"semantic_call_sites"`
	DistinctMethodRoutes int                `json:"distinct_method_routes"`
	Endpoints            []coverageEndpoint `json:"endpoints"`
}

type coverageEndpoint struct {
	Method   string   `json:"method"`
	Route    string   `json:"route"`
	Symbols  []string `json:"symbols"`
	Coverage string   `json:"coverage"`
}

var routeParameterRE = regexp.MustCompile(`\{[A-Za-z][A-Za-z0-9]*\}`)

func TestCoverageManifest(t *testing.T) {
	data, err := os.ReadFile("coverage_manifest.json")
	if err != nil {
		t.Fatalf("coverage manifest is required: %v", err)
	}
	var manifest coverageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode coverage manifest: %v", err)
	}
	if manifest.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", manifest.SchemaVersion)
	}
	if len(manifest.Endpoints) == 0 {
		t.Fatal("coverage manifest has no endpoints")
	}
	if got := len(manifest.Endpoints); got != manifest.DistinctMethodRoutes {
		t.Fatalf("manifest has %d endpoints, distinct_method_routes says %d", got, manifest.DistinctMethodRoutes)
	}
	if manifest.SemanticCallSites < manifest.DistinctMethodRoutes {
		t.Fatalf("semantic_call_sites %d is smaller than endpoint count %d", manifest.SemanticCallSites, manifest.DistinctMethodRoutes)
	}

	clientType := reflect.TypeOf(Client{})
	seenEndpoints := map[string]struct{}{}
	seenSymbols := map[string][]string{}
	for index, endpoint := range manifest.Endpoints {
		key := endpoint.Method + " " + endpoint.Route
		if endpoint.Coverage != "COVERED" {
			t.Errorf("endpoint %s has coverage %q, want COVERED", key, endpoint.Coverage)
		}
		if endpoint.Method == "" || endpoint.Method != strings.ToUpper(endpoint.Method) {
			t.Errorf("endpoint[%d] has invalid method %q", index, endpoint.Method)
		}
		if !strings.HasPrefix(endpoint.Route, "/") || strings.Contains(endpoint.Route, "?") {
			t.Errorf("endpoint %s has non-normalized route %q", key, endpoint.Route)
		}
		if strings.Contains(endpoint.Route, "{") {
			withoutParameters := routeParameterRE.ReplaceAllString(endpoint.Route, "")
			if strings.ContainsAny(withoutParameters, "{}") {
				t.Errorf("endpoint %s has malformed route parameter", key)
			}
		}
		if _, duplicate := seenEndpoints[key]; duplicate {
			t.Errorf("duplicate endpoint %s", key)
		}
		seenEndpoints[key] = struct{}{}
		if len(endpoint.Symbols) == 0 {
			t.Errorf("endpoint %s has no SDK symbol", key)
		}
		for _, symbol := range endpoint.Symbols {
			if err := requireExportedSDKMethod(clientType, symbol); err != nil {
				t.Errorf("endpoint %s: %v", key, err)
			}
			seenSymbols[symbol] = append(seenSymbols[symbol], key)
			catalogEntry, ok := sdkCoverageCatalog[symbol]
			if !ok {
				t.Errorf("endpoint %s: SDK catalog is missing symbol %q; run go generate ./...", key, symbol)
			} else if catalogEntry.Method != endpoint.Method || catalogEntry.Route != endpoint.Route {
				t.Errorf("endpoint %s: SDK catalog maps %q to %s %s", key, symbol, catalogEntry.Method, catalogEntry.Route)
			}
		}
	}
	for symbol := range sdkCoverageCatalog {
		if _, ok := seenSymbols[symbol]; !ok {
			t.Errorf("SDK catalog symbol %q is absent from the manifest", symbol)
		}
	}

	// Positive controls: prove the reflection check finds established methods
	// in addition to the newly added coverage families.
	for _, symbol := range []string{"Checks.List", "Predict.StageBGPAdvertisement", "Collectors.Register", "Browser.LoginAPI"} {
		if _, ok := seenSymbols[symbol]; !ok {
			t.Errorf("positive-control SDK symbol %s is absent from the manifest", symbol)
		}
	}
}

func requireExportedSDKMethod(clientType reflect.Type, symbol string) error {
	serviceName, methodName, ok := strings.Cut(symbol, ".")
	if !ok || serviceName == "" || methodName == "" || strings.Contains(methodName, ".") {
		return fmt.Errorf("invalid SDK symbol %q", symbol)
	}
	if serviceName == "Raw" {
		return fmt.Errorf("raw-only symbol %q cannot be COVERED", symbol)
	}
	field, ok := clientType.FieldByName(serviceName)
	if !ok || !field.IsExported() {
		return fmt.Errorf("SDK service %q is not an exported Client field", serviceName)
	}
	if field.Type.Kind() != reflect.Pointer {
		return fmt.Errorf("SDK service %q has non-pointer type %s", serviceName, field.Type)
	}
	method, ok := field.Type.MethodByName(methodName)
	if !ok || !method.IsExported() {
		return fmt.Errorf("SDK symbol %q has no corresponding exported method", symbol)
	}
	rawRequestType := reflect.TypeOf(RawRequest{})
	for i := 0; i < method.Type.NumIn(); i++ {
		if method.Type.In(i) == rawRequestType {
			return fmt.Errorf("SDK symbol %q accepts RawRequest and is not typed coverage", symbol)
		}
	}
	return nil
}
