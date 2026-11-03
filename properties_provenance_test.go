package forward

import (
	"context"
	"errors"
	"testing"
)

// A global write to a property that is only settable per org is Forward's 403 with the property in the message; the SDK names it so a caller
// can fall back to a per-org write instead of reading the text.
func TestSetGlobalToAnOrgLevelPropertyIsClassified(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	props := newTestClient(t, predictRecorder(t, 403, `{"message":"Permission denied. Global default value cannot be changed for edge_assets_in_sources"}`, &seen).URL).Properties
	_, _, err := props.SetGlobal(context.Background(), "EDGE_ASSETS_IN_SOURCES", "true")
	if !IsGlobalDefaultNotAllowed(err) || !errors.Is(err, ErrGlobalDefaultNotAllowed) || !IsErrorKind(err, ErrorKindGlobalDefaultNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.GateProperty != "EDGE_ASSETS_IN_SOURCES" {
		t.Fatalf("property = %+v", apiErr)
	}
	if IsErrorKind(err, ErrorKindPermissionDenied) {
		t.Error("a global-default refusal also read as a missing permission")
	}
}

// The cross-org and network property reads: one property across every org it is overridden in, one org's one property, and a network's.
func TestPropertyProvenanceReads(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	props := newTestClient(t, predictRecorder(t, 200, `{"101":true,"102":false}`, &seen).URL).Properties
	ctx := context.Background()

	overrides, _, err := props.OrganizationOverrides(ctx, "EDGE_ASSETS_IN_SOURCES")
	if err != nil || len(overrides) != 2 || string(overrides["101"]) != "true" || string(overrides["102"]) != "false" {
		t.Fatalf("overrides = %v, %v", overrides, err)
	}
	if _, _, err := props.OrganizationValue(ctx, "101", "THREAT_EXPOSURE"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := props.NetworkValue(ctx, "7", "SOME_NETWORK_PROPERTY"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := props.NetworkOverrides(ctx, "SOME_NETWORK_PROPERTY"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/global-config/EDGE_ASSETS_IN_SOURCES?view=overrides",
		"GET /api/orgs/101/config/THREAT_EXPOSURE",
		"GET /api/networks/7/config/SOME_NETWORK_PROPERTY",
		"GET /api/config/SOME_NETWORK_PROPERTY/overrides",
	}
	if len(seen) != len(want) {
		t.Fatalf("seen %+v", seen)
	}
	for i, w := range want {
		if got := seen[i].method + " " + seen[i].uri; got != w {
			t.Errorf("request %d = %s, want %s", i, got, w)
		}
	}
	// A build without the view answers the plain property read; that must not read as an override by an org named after the property.
	plain := newTestClient(t, predictRecorder(t, 200, `{"edge_assets_in_sources":false}`, &seen).URL).Properties
	if _, _, err := plain.OrganizationOverrides(ctx, "EDGE_ASSETS_IN_SOURCES"); !errors.Is(err, ErrEndpointNotServed) {
		t.Errorf("plain answer: err = %v, want ErrEndpointNotServed", err)
	}
	seen = seen[:len(seen)-1]
	before := len(seen)
	if _, _, err := props.OrganizationOverrides(ctx, " "); err == nil {
		t.Error("a blank property was sent")
	}
	if _, _, err := props.OrganizationValue(ctx, " ", "X"); err == nil || len(seen) != before {
		t.Error("a blank org was sent")
	}
}
