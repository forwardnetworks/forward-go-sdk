package perfpayload

import (
	"bytes"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustEncode(t *testing.T, p Payload) []byte {
	t.Helper()
	b, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Expected bytes worked out by hand from PerfProtos.proto, not from the encoder:
//
//	PerfDataPB.timestamp = 1 (LEN):  0A 02 | Timestamp.seconds = 1 (varint): 08 01
//	PerfDataPB.device_perf = 2 (LEN): 12 09 | DevicePerfPB:
//	  device_name = 1 "a":   0A 01 61
//	  vendor_os   = 2 (1):   10 01
//	  timestamp   = 3 (LEN): 1A 02 08 02
func TestEncodeMatchesTheWireFormatWorkedOutFromTheProto(t *testing.T) {
	t.Parallel()
	p := Payload{Time: time.Unix(1, 0), Devices: []DevicePerf{{DeviceName: "a", VendorOS: 1, Time: time.Unix(2, 0)}}}
	want := []byte{0x0A, 0x02, 0x08, 0x01, 0x12, 0x09, 0x0A, 0x01, 'a', 0x10, 0x01, 0x1A, 0x02, 0x08, 0x02}
	if got := mustEncode(t, p); !bytes.Equal(got, want) {
		t.Fatalf("got % X\nwant % X", got, want)
	}
}

// uptime_seconds = 5 (varint 300 = AC 02), cpu_usage = 6 (fixed32 0.5), memory_usage = 7 (0.25) and one map entry (field 4):
// key "e1" (field 1), value InterfaceCountersPB{in_octets=1:300, speed_mbps=7:1000}.
func TestEncodeScalarsFloatsAndMapEntries(t *testing.T) {
	t.Parallel()
	d := DevicePerf{DeviceName: "x", UptimeSeconds: 300, CPU: 0.5, Memory: 0.25,
		Interfaces: map[string]Counters{"e1": {InOctets: 300, SpeedMbps: 1000}}}
	got := d.encode()
	for name, frag := range map[string][]byte{
		"uptime": {0x28, 0xAC, 0x02},
		"cpu":    {0x35, 0x00, 0x00, 0x00, 0x3F},
		"memory": {0x3D, 0x00, 0x00, 0x80, 0x3E},
		"map":    {0x22, 0x0C, 0x0A, 0x02, 'e', '1', 0x12, 0x06, 0x08, 0xAC, 0x02, 0x38, 0xE8, 0x07},
	} {
		if !bytes.Contains(got, frag) {
			t.Errorf("%s: % X not found in % X", name, frag, got)
		}
	}
}

// proto3 omits zero values, and the output must not depend on map iteration order.
func TestZeroValuesAreOmittedAndOutputIsDeterministic(t *testing.T) {
	t.Parallel()
	if got := mustEncode(t, Payload{Time: time.Unix(1, 0)}); !bytes.Equal(got, []byte{0x0A, 0x02, 0x08, 0x01}) {
		t.Fatalf("% X", got)
	}
	p := Payload{Time: time.Unix(5, 0), Devices: []DevicePerf{{DeviceName: "d", Time: time.Unix(4, 0),
		Interfaces: map[string]Counters{"b": {InOctets: 1}, "a": {InOctets: 2}, "c": {InOctets: 3}}}}}
	first := mustEncode(t, p)
	for i := 0; i < 20; i++ {
		if !bytes.Equal(first, mustEncode(t, p)) {
			t.Fatal("encoding differs between runs")
		}
	}
}

// Forward drops a whole device sample whose CPU or memory is above 1, and drops a sample older than the device's newest
// one, silently. Both must be refused before anything is sent; the same device later or at the same time, and different
// devices in any order, are fine.
func TestEncodeRefusesWhatForwardWouldDrop(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0)
	device := func(name string, at time.Time, cpu, mem float32) DevicePerf {
		return DevicePerf{DeviceName: name, Time: at, CPU: cpu, Memory: mem}
	}
	cases := []struct {
		name    string
		devices []DevicePerf
		want    error
	}{
		{"cpu above one", []DevicePerf{device("a", now, 1.01, 0)}, ErrCPUOrMemoryAbove1},
		{"memory above one", []DevicePerf{device("a", now, 0, 1.5)}, ErrCPUOrMemoryAbove1},
		{"percent sent as a fraction", []DevicePerf{device("a", now, 45, 0)}, ErrCPUOrMemoryAbove1},
		{"negative", []DevicePerf{device("a", now, -0.1, 0)}, ErrCPUOrMemoryAbove1},
		{"nan", []DevicePerf{device("a", now, float32(math.NaN()), 0)}, ErrCPUOrMemoryAbove1},
		{"out of order", []DevicePerf{device("a", now, 0, 0), device("b", now.Add(-time.Hour), 0, 0), device("a", now.Add(-time.Second), 0, 0)}, ErrOutOfOrder},
		{"no name", []DevicePerf{device("", now, 0, 0)}, ErrNoDeviceName},
		{"no device time", []DevicePerf{device("a", time.Time{}, 0, 0)}, ErrNoTime},
	}
	for _, c := range cases {
		if _, err := (Payload{Time: now, Devices: c.devices}).Encode(); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := (Payload{Devices: []DevicePerf{device("a", now, 0, 0)}}).Encode(); !errors.Is(err, ErrNoTime) {
		t.Errorf("no payload time: err = %v", err)
	}
	ok := []DevicePerf{device("a", now, 1, 1), device("b", now.Add(-time.Hour), 0.2, 0.3), device("a", now, 0.5, 0.5), device("a", now.Add(time.Minute), 0, 0)}
	if _, err := (Payload{Time: now, Devices: ok}).Encode(); err != nil {
		t.Errorf("a valid payload (cpu and memory exactly 1, same-time repeat, devices interleaved): %v", err)
	}
}

func TestVendorOSTableCoversTheCommonPlatforms(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]int32{"ARISTA_EOS": 1, "CISCO_IOS": 2, "CISCO_NXOS": 3, "PAN_OS": 19, "CISCO_IOS_XE": 20} {
		if VendorOS[name] != want {
			t.Errorf("%s = %d, want %d", name, VendorOS[name], want)
		}
	}
}

func TestLookupVendorOSAcceptsNQEsCiscoShortNames(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]int32{"ARISTA_EOS": 1, "IOS": 2, "IOS_XE": 20, "IOS_XR": 22, "NXOS": 3} {
		if got, ok := LookupVendorOS(name); !ok || got != want {
			t.Errorf("%s -> %d %v, want %d", name, got, ok, want)
		}
	}
	if _, ok := LookupVendorOS("NOT_A_REAL_OS"); ok {
		t.Error("an unknown OS must not map")
	}
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
	p := Payload{Time: time.Unix(1793046570, 500), Devices: []DevicePerf{
		{DeviceName: "core1", VendorOS: VendorOS["ARISTA_EOS"], Time: time.Unix(1793046569, 0), UptimeSeconds: 11621, CPU: 0.5, Memory: 0.25,
			Interfaces: map[string]Counters{"Ethernet1": {InOctets: 123456789, InDiscards: 1, InErrors: 2, OutOctets: 987654321, OutDiscards: 3, OutErrors: 4, SpeedMbps: 1000, InterfaceType: 6},
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
