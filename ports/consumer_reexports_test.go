package ports_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// An adapter of a consumer imports ports and nothing else, so each domain name it needs must come through ports as the SAME
// thing: the same constant value, the same variable (so errors.Is and slice contents agree), the same behaviour for a function,
// and the identical type for a struct. A re-export that drifted (a retyped constant, a copied variable) would make the same
// request mean something else depending on which package a consumer imported.
func TestConsumerReexportsAreTheDomainNames(t *testing.T) {
	for name, pair := range map[string][2]string{
		"PropertyFilterOff":               {string(ports.PropertyFilterOff), string(domain.PropertyFilterOff)},
		"PropertyFilterNondefault":        {string(ports.PropertyFilterNondefault), string(domain.PropertyFilterNondefault)},
		"PropertyFilterConfigured":        {string(ports.PropertyFilterConfigured), string(domain.PropertyFilterConfigured)},
		"PropertyFilterOverridden":        {string(ports.PropertyFilterOverridden), string(domain.PropertyFilterOverridden)},
		"ClientPackageHeadlessLinux":      {string(ports.ClientPackageHeadlessLinux), string(domain.ClientPackageHeadlessLinux)},
		"ClientPackageLinux":              {string(ports.ClientPackageLinux), string(domain.ClientPackageLinux)},
		"ClientPackageOSX":                {string(ports.ClientPackageOSX), string(domain.ClientPackageOSX)},
		"ClientPackageWindowsX64":         {string(ports.ClientPackageWindowsX64), string(domain.ClientPackageWindowsX64)},
		"ClientPackageHeadlessWindowsX64": {string(ports.ClientPackageHeadlessWindowsX64), string(domain.ClientPackageHeadlessWindowsX64)},
	} {
		if pair[0] == "" || pair[0] != pair[1] {
			t.Errorf("%s: ports says %q, domain says %q", name, pair[0], pair[1])
		}
	}
	if ports.DefaultCollectorConcurrency != domain.DefaultCollectorConcurrency || ports.DefaultCollectorConcurrency <= 0 {
		t.Errorf("DefaultCollectorConcurrency: ports %d, domain %d", ports.DefaultCollectorConcurrency, domain.DefaultCollectorConcurrency)
	}

	if !errors.Is(ports.ErrConnectivityDiffPartial, domain.ErrConnectivityDiffPartial) || !errors.Is(domain.ErrConnectivityDiffPartial, ports.ErrConnectivityDiffPartial) {
		t.Error("ErrConnectivityDiffPartial is not the same error through ports and domain")
	}
	if len(ports.CountableDiffKinds) == 0 || !reflect.DeepEqual(ports.CountableDiffKinds, domain.CountableDiffKinds) {
		t.Errorf("CountableDiffKinds: ports %v, domain %v", ports.CountableDiffKinds, domain.CountableDiffKinds)
	}

	for in, want := range map[string]string{`true`: "true", `"x"`: "x", `3`: "3", `[1,2]`: "[1,2]", `null`: ""} {
		got := ports.PropertyValueString(json.RawMessage(in))
		if got != want || got != domain.PropertyValueString(json.RawMessage(in)) {
			t.Errorf("PropertyValueString(%s) = %q through ports, %q through domain, want %q", in, got, domain.PropertyValueString(json.RawMessage(in)), want)
		}
	}
	if ports.IsUnknownOrgProperty(errors.New("some other error")) || ports.IsUnknownOrgProperty(nil) {
		t.Error("IsUnknownOrgProperty is true for an error that is not Forward's unknown-property 400")
	}

	// Struct types are aliases: a value of either type is assignable to the other.
	var (
		_ domain.NQECommitMessage        = ports.NQECommitMessage{}
		_ domain.NQEDiffEntry            = ports.NQEDiffEntry{}
		_ domain.DiffFile                = ports.DiffFile{}
		_ domain.ACLMatch                = ports.ACLMatch{}
		_ domain.ACLDiffEntry            = ports.ACLDiffEntry{}
		_ domain.DeviceChanges           = ports.DeviceChanges{}
		_ domain.DeviceMetric            = ports.DeviceMetric{}
		_ domain.DeviceMetricHistory     = ports.DeviceMetricHistory{}
		_ domain.InterfaceMetric         = ports.InterfaceMetric{}
		_ domain.InterfaceMetricHistory  = ports.InterfaceMetricHistory{}
		_ domain.InterfaceWithDirection  = ports.InterfaceWithDirection{}
		_ domain.NetworkPathResult       = ports.NetworkPathResult{}
		_ domain.CommandError            = ports.CommandError{}
		_ domain.ConnectivityWaitOptions = ports.ConnectivityWaitOptions{}
	)
}
