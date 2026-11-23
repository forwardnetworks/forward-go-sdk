package domain_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/forwardnetworks/forward-go-sdk/domain"
)

// An access table is keyed by the qualified "Scope.Name" form, so the same operation name in the network and the org scope must
// stay two distinct keys.
func TestAccessOperationQualified(t *testing.T) {
	network := domain.AccessOperation{Scope: "NetworkOperation", Name: "VIEW_CHECKS"}
	org := domain.AccessOperation{Scope: "OrgOperation", Name: "VIEW_CHECKS"}
	if got := network.Qualified(); got != "NetworkOperation.VIEW_CHECKS" {
		t.Fatalf("Qualified() = %q", got)
	}
	if got := org.Qualified(); got != "OrgOperation.VIEW_CHECKS" {
		t.Fatalf("Qualified() = %q", got)
	}
}

// A Reachability check is the same IntentCheck body as an Existential or Isolation one (the path query as "filters"); only the
// checkType differs, and a return path is an Existential-only option.
func TestReachabilityCheckDefinition(t *testing.T) {
	q := domain.TracePathQuery{
		From: &domain.TraceEndpoint{Location: domain.TraceSubnet("10.10.10.10/32"), Headers: []domain.TraceHeaders{{"ip_proto": {"6"}, "tp_dst": {"443"}}}},
		To:   &domain.TraceEndpoint{Location: domain.TraceSubnet("10.20.20.20/32")},
	}
	opts := domain.IntentCheckOptions{NoiseTypes: []string{"DROP"}}
	reach, err := domain.ReachabilityCheckDefinition(q, opts)
	if err != nil {
		t.Fatal(err)
	}
	if reach["checkType"] != "Reachability" {
		t.Fatalf("checkType = %v", reach["checkType"])
	}
	exists, err := domain.ExistentialCheckDefinition(q, opts)
	if err != nil {
		t.Fatal(err)
	}
	reachFilters, _ := json.Marshal(reach["filters"])
	existsFilters, _ := json.Marshal(exists["filters"])
	if string(reachFilters) != string(existsFilters) || !strings.Contains(string(reachFilters), `"tp_dst":["443"]`) {
		t.Fatalf("filters differ or lack the query: reach=%s exists=%s", reachFilters, existsFilters)
	}
	if fmt.Sprint(reach["noiseTypes"]) != "[DROP]" {
		t.Fatalf("noiseTypes = %v", reach["noiseTypes"])
	}
	if _, err := domain.ReachabilityCheckDefinition(q, domain.IntentCheckOptions{ReturnPath: "ANY"}); err == nil {
		t.Fatal("a return path on a Reachability check must be refused")
	}
	if _, err := domain.ReachabilityCheckDefinition(domain.TracePathQuery{}, domain.IntentCheckOptions{}); err == nil {
		t.Fatal("an empty path query must be refused")
	}
}

// An unset boost means no boost (0), while an explicit value is kept as stated.
func TestOrgCollectionSettingsEffectivePerDeviceConcurrencyBoost(t *testing.T) {
	if got := (domain.OrgCollectionSettings{}).EffectivePerDeviceConcurrencyBoost(); got != 0 {
		t.Fatalf("unset boost = %d, want 0", got)
	}
	three := 3
	if got := (domain.OrgCollectionSettings{PerDeviceConcurrencyBoost: &three}).EffectivePerDeviceConcurrencyBoost(); got != 3 {
		t.Fatalf("boost = %d, want 3", got)
	}
}

// A fail-closed client's error must let callers test the reason with errors.Is rather than its text.
func TestUnavailableErrorUnwrap(t *testing.T) {
	cause := errors.New("tenant credentials missing")
	err := error(&domain.UnavailableError{NetworkID: "n1", Cause: cause})
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is must reach the cause")
	}
	if got := errors.Unwrap(err); got != cause {
		t.Fatalf("Unwrap() = %v", got)
	}
	var nilErr *domain.UnavailableError
	if nilErr.Unwrap() != nil {
		t.Fatal("a nil *UnavailableError unwraps to nil")
	}
}

// The base snapshot's advertisement is absent for an injection, so nil must mean "Forward sent none", not a decode failure; when
// present it decodes to the typed advertisement.
func TestBGPAdvertisementDiffOriginalAdvertisement(t *testing.T) {
	var injected domain.BGPAdvertisementDiff
	if err := json.Unmarshal([]byte(`{"change":"INJECTED","updateAdded":{"prefix":"10.0.0.0/24"}}`), &injected); err != nil {
		t.Fatal(err)
	}
	if got, err := injected.OriginalAdvertisement(); err != nil || got != nil {
		t.Fatalf("no original: got %#v, %v", got, err)
	}
	var modified domain.BGPAdvertisementDiff
	body := `{"change":"MODIFIED","original":{"vrf":"default","externalPeer":"peer1","prefix":"10.1.0.0/16","nextHop":"192.0.2.1","localPref":200,"asPath":[65001,65002],"communities":["65001:100"]},"updateAdded":{"prefix":"10.9.0.0/16"}}`
	if err := json.Unmarshal([]byte(body), &modified); err != nil {
		t.Fatal(err)
	}
	got, err := modified.OriginalAdvertisement()
	if err != nil || got == nil {
		t.Fatalf("original: %#v, %v", got, err)
	}
	if got.Prefix != "10.1.0.0/16" || got.VRF != "default" || got.NextHop != "192.0.2.1" || got.LocalPref == nil || *got.LocalPref != 200 ||
		len(got.ASPath) != 2 || got.ASPath[1] != 65002 {
		t.Fatalf("original = %#v", got)
	}
	bad := domain.BGPAdvertisementDiff{Original: json.RawMessage(`"not an object"`)}
	if _, err := bad.OriginalAdvertisement(); err == nil {
		t.Fatal("an undecodable original must be an error")
	}
}

// The obfuscation key is a secret: %#v of the options (as a debug log would print them) must never show it.
func TestSnapshotExportOptionsGoStringRedactsKey(t *testing.T) {
	opts := domain.SnapshotExportOptions{ObfuscationKey: "s3cret-key", ObfuscateNames: true, IncludeDevices: []string{"r1"}}
	got := fmt.Sprintf("%#v", opts)
	if strings.Contains(got, "s3cret-key") {
		t.Fatalf("%%#v leaks the key: %s", got)
	}
	if !strings.Contains(got, "redacted") {
		t.Fatalf("%%#v does not say the key was redacted: %s", got)
	}
	if got := opts.GoString(); strings.Contains(got, "s3cret-key") {
		t.Fatalf("GoString leaks the key: %s", got)
	}
}

// The suffix mirrors Forward's DeploymentArtifactFileType (app/.../cv/deployment/DeploymentArtifactFileType.java: OVA(".ova"),
// TAR_GZ(".tar.gz"), AIRGAP(".airgap"), PACKAGE(".package")); a type this SDK does not know gets no extension rather than a guess.
func TestDeploymentArtifactFileTypeSuffix(t *testing.T) {
	for typ, want := range map[domain.DeploymentArtifactFileType]string{
		"OVA":     ".ova",
		"TAR_GZ":  ".tar.gz",
		"AIRGAP":  ".airgap",
		"PACKAGE": ".package",
		"QCOW2":   "",
		"":        "",
	} {
		if got := typ.Suffix(); got != want {
			t.Errorf("%q.Suffix() = %q, want %q", typ, got, want)
		}
	}
}
