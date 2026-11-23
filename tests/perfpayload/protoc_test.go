package perfpayload_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forwardnetworks/forward-go-sdk/domain/perfpayload"
)

// This oracle runs protoc, a process, so it lives outside the domain package it checks: the domain may not import os or
// os/exec, not even in a test. It moved here unchanged from domain/perfpayload/encode_test.go but for the package
// qualifiers.

func mustEncode(t *testing.T, p perfpayload.Payload) []byte {
	t.Helper()
	b, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The independent check: decode our bytes with protoc against Forward's real PerfProtos.proto. A wrong field number, type
// or nesting fails here. Runs only when FORWARD_SOURCE names a Forward checkout and protoc is installed.
func TestDecodesWithProtocAgainstForwardsRealProto(t *testing.T) {
	src := os.Getenv("FORWARD_SOURCE")
	if src == "" {
		t.Skip("FORWARD_SOURCE not set")
	}
	protoc, err := exec.LookPath("protoc")
	if err != nil {
		t.Skip("protoc not installed")
	}
	proto := filepath.Join(src, "client-structs/src/main/proto/com/forwardnetworks/client/web/PerfProtos.proto")
	if _, err := os.Stat(proto); err != nil {
		t.Fatalf("FORWARD_SOURCE is set but %s is missing: %v", proto, err)
	}
	p := perfpayload.Payload{Time: time.Unix(1793046570, 500), Devices: []perfpayload.DevicePerf{
		{DeviceName: "core1", VendorOS: perfpayload.VendorOS["ARISTA_EOS"], Time: time.Unix(1793046569, 0), UptimeSeconds: 11621, CPU: 0.5, Memory: 0.25,
			Interfaces: map[string]perfpayload.Counters{"Ethernet1": {InOctets: 123456789, InDiscards: 1, InErrors: 2, OutOctets: 987654321, OutDiscards: 3, OutErrors: 4, SpeedMbps: 1000, InterfaceType: 6},
				"Ethernet2": {InOctets: 5}}},
	}}
	cmd := exec.Command(protoc, "--proto_path="+filepath.Join(src, "client-structs/src/main/proto"),
		"--proto_path="+filepath.Join(src, "base/src/main/proto"), "--decode=com.forwardnetworks.client.web.PerfDataPB", proto)
	cmd.Stdin = bytes.NewReader(mustEncode(t, p))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("protoc could not decode the payload: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{"seconds: 1793046570", "nanos: 500", `device_name: "core1"`, "vendor_os: VENDOR_OS_ARISTA_EOS",
		"uptime_seconds: 11621", "cpu_usage: 0.5", "memory_usage: 0.25", `key: "Ethernet1"`, "in_octets: 123456789",
		"out_octets: 987654321", "in_discards: 1", "out_errors: 4", "speed_mbps: 1000", "interface_type: 6", `key: "Ethernet2"`} {
		if !strings.Contains(text, want) {
			t.Errorf("decoded output lacks %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "error_details") {
		t.Error("an error_details was emitted")
	}
}
