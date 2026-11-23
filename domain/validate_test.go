package domain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// A call that addresses a network or a device must be refused before anything is sent when the ID or name is blank, and must
// address the trimmed value otherwise; the network is checked before the device, so a call missing both names the network.
func TestRequireNetworkIDAndDeviceRefRefuseBlanksAndTrim(t *testing.T) {
	if id, err := RequireNetworkID("  n1 "); err != nil || id != "n1" {
		t.Errorf("RequireNetworkID(\"  n1 \") = %q, %v", id, err)
	}
	if _, err := RequireNetworkID(" \t"); err == nil {
		t.Error("a blank network ID was accepted")
	}
	n, d, err := RequireDeviceRef(" n1", " core1 ")
	if err != nil || n != "n1" || d != "core1" {
		t.Errorf("RequireDeviceRef = %q, %q, %v", n, d, err)
	}
	if _, _, err := RequireDeviceRef("n1", "  "); err == nil || err.Error() != "forward: device name is required" {
		t.Errorf("a blank device name: %v", err)
	}
	if _, _, err := RequireDeviceRef("", ""); err == nil || err.Error() != "forward: network ID is required" {
		t.Errorf("neither given must name the network first: %v", err)
	}
	if RequireDecoratorName("fwd-x") != nil || RequireDecoratorName("   ") == nil {
		t.Error("RequireDecoratorName must accept a name and refuse a blank one")
	}
}

// A classic device needs both a name and a host; Forward refuses either missing, so the SDK refuses it first.
func TestClassicDeviceRequestValidate(t *testing.T) {
	for _, tc := range []struct {
		r  ClassicDeviceRequest
		ok bool
	}{
		{ClassicDeviceRequest{Name: "r1", Host: "10.0.0.1"}, true},
		{ClassicDeviceRequest{Name: " ", Host: "10.0.0.1"}, false},
		{ClassicDeviceRequest{Name: "r1", Host: ""}, false},
	} {
		if err := tc.r.Validate(); (err == nil) != tc.ok {
			t.Errorf("%+v: Validate() = %v", tc.r, err)
		}
	}
}

// Forward's org property names are case-insensitive: an exact key wins, a key differing only in case is found, and a missing
// property is nil (not an empty value), so "unset" stays distinguishable.
func TestPropertyValuesLookupIsCaseInsensitiveAndPrefersExact(t *testing.T) {
	p := PropertyValues{"predictModeling": json.RawMessage(`true`), "PREDICTMODELING": json.RawMessage(`false`), "Other": json.RawMessage(`1`)}
	if got := string(p.Lookup("predictModeling")); got != "true" {
		t.Errorf("exact key: %q", got)
	}
	if got := string(p.Lookup("other")); got != "1" {
		t.Errorf("case-insensitive key: %q", got)
	}
	if got := p.Lookup("absent"); got != nil {
		t.Errorf("missing property: %q, want nil", got)
	}
}

// A poll pause ends with the interval, or at once with the context's error when the caller gives up.
func TestWaitIntervalReturnsOnCancel(t *testing.T) {
	if err := WaitInterval(context.Background(), time.Millisecond); err != nil {
		t.Errorf("a completed wait returned %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := WaitInterval(ctx, time.Hour); !errors.Is(err, context.Canceled) || time.Since(start) > time.Minute {
		t.Errorf("a cancelled wait returned %v after %v", err, time.Since(start))
	}
}

// A nil service's error names the family, in the words the SDK has always used, so callers matching on it keep working.
func TestNilServiceErrorNamesTheFamily(t *testing.T) {
	if got := NilServiceError("collector tasks").Error(); got != "forward: collector tasks service is nil" {
		t.Errorf("NilServiceError = %q", got)
	}
}
