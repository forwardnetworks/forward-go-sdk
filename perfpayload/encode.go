// Package perfpayload builds the performance upload Forward's collectors send: a serialized PerfDataPB, the body of
// POST /api/networks/{id}/performance (see forward.PerformanceService.UploadWithIdentity).
//
// The wire format is written by hand from Forward's PerfProtos.proto (messages PerfDataPB, DevicePerfPB and
// InterfaceCountersPB), so the package has no protobuf dependency. The schema is small and numbered; encode_test.go
// checks the bytes by decoding them with protoc against Forward's real proto when protoc and a Forward source
// checkout (FORWARD_SOURCE) are available. Forward can change the proto between releases: re-check it before relying
// on an upgrade, and regenerate vendoros_gen.go from the same checkout.
//
// What Forward does with an upload (PerformanceService.save and PerformanceRepo), which Encode enforces where it can:
//   - A device sample whose CPU or memory is above 1 is dropped whole. Encode refuses it.
//   - An interface whose utilization comes out above 1 is dropped on its own. That follows from the counters and the
//     time between samples, so it cannot be checked here.
//   - A sample older than the device's newest stored sample is dropped without an error (the upload is still
//     answered 204). Two samples at the same time inside one payload overwrite each other; a re-send in a later batch
//     does not, because each batch's lag shifts its stored times. See Watermarks for a guard.
//   - Interface counters are cumulative. Rates and utilization are derived from the difference to the device's
//     previous sample, so the first sample of a device has no utilization. A decrease is read as a counter reset.
//   - Forward shifts each stored time by the lag between the batch timestamp and its own clock, so set
//     Payload.Time to when the payload is built and sent, not to when the data was collected.
package perfpayload

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// Counters are cumulative interface counters, as an SNMP poll would read them. Forward diffs consecutive samples
// itself (and handles wrap), so only the deltas matter, not the absolute values.
type Counters struct {
	InOctets, InDiscards, InErrors    uint64
	OutOctets, OutDiscards, OutErrors uint64
	SpeedMbps                         uint32
	InterfaceType                     uint32 // IANA ifType
}

// DevicePerf is one device's metrics at one time.
type DevicePerf struct {
	DeviceName    string
	VendorOS      int32 // VendorOsPB number; see VendorOS and LookupVendorOS. Forward normalizes interface names per vendor, so 0 (unknown) may not match its model.
	Time          time.Time
	Interfaces    map[string]Counters // keyed by ifName, as Forward's collector keys them
	UptimeSeconds uint32
	CPU           float32 // fraction 0..1; zero is sent as absent
	Memory        float32 // fraction 0..1; zero is sent as absent
}

// Payload is one upload.
type Payload struct {
	// Time is the batch timestamp. Forward compares it with its own clock to shift every stored time, so set it to
	// the moment the payload is built and sent.
	Time time.Time
	// Devices holds the samples. Entries of one device must be in chronological order.
	Devices []DevicePerf
}

// Validation errors returned by Encode. They wrap with the device name, so test them with errors.Is.
var (
	ErrNoTime            = errors.New("perfpayload: payload and device times must be set and after 1970")
	ErrNoDeviceName      = errors.New("perfpayload: device name is required")
	ErrCPUOrMemoryAbove1 = errors.New("perfpayload: cpu and memory must be fractions from 0 to 1 (not a percent); Forward drops the whole device sample above 1")
	ErrOutOfOrder        = errors.New("perfpayload: a device's entries must be in time order; Forward drops a sample older than the device's newest")
)

// Validate reports the first reason Forward would drop or mis-store part of the payload.
func (p Payload) Validate() error {
	if !validTime(p.Time) {
		return fmt.Errorf("%w (payload time)", ErrNoTime)
	}
	latest := map[string]time.Time{}
	for i, d := range p.Devices {
		if d.DeviceName == "" {
			return fmt.Errorf("%w (entry %d)", ErrNoDeviceName, i)
		}
		if !validTime(d.Time) {
			return fmt.Errorf("%w (device %q)", ErrNoTime, d.DeviceName)
		}
		if !fraction(d.CPU) || !fraction(d.Memory) {
			return fmt.Errorf("%w (device %q: cpu %v, memory %v)", ErrCPUOrMemoryAbove1, d.DeviceName, d.CPU, d.Memory)
		}
		if prev, ok := latest[d.DeviceName]; ok && d.Time.Before(prev) {
			return fmt.Errorf("%w (device %q)", ErrOutOfOrder, d.DeviceName)
		}
		latest[d.DeviceName] = d.Time
	}
	return nil
}

func validTime(t time.Time) bool { return !t.IsZero() && t.Unix() > 0 }

func fraction(v float32) bool { return v >= 0 && v <= 1 } // false for NaN too

type buf struct{ b []byte }

func (w *buf) varint(v uint64)     { w.b = binary.AppendUvarint(w.b, v) }
func (w *buf) tag(field, wire int) { w.varint(uint64(field<<3 | wire)) }
func (w *buf) uint(field int, v uint64) {
	if v != 0 {
		w.tag(field, 0)
		w.varint(v)
	}
}
func (w *buf) float(field int, v float32) {
	if v != 0 {
		w.tag(field, 5)
		w.b = binary.LittleEndian.AppendUint32(w.b, math.Float32bits(v))
	}
}
func (w *buf) bytes(field int, v []byte) {
	w.tag(field, 2)
	w.varint(uint64(len(v)))
	w.b = append(w.b, v...)
}
func (w *buf) str(field int, v string) {
	if v != "" {
		w.bytes(field, []byte(v))
	}
}

func timestamp(t time.Time) []byte {
	var w buf
	w.uint(1, uint64(t.Unix())) // google.protobuf.Timestamp.seconds
	w.uint(2, uint64(t.Nanosecond()))
	return w.b
}

func (c Counters) encode() []byte {
	var w buf
	w.uint(1, c.InOctets)
	w.uint(2, c.InDiscards)
	w.uint(3, c.InErrors)
	w.uint(4, c.OutOctets)
	w.uint(5, c.OutDiscards)
	w.uint(6, c.OutErrors)
	w.uint(7, uint64(c.SpeedMbps))
	w.uint(8, uint64(c.InterfaceType))
	return w.b
}

func (d DevicePerf) encode() []byte {
	var w buf
	w.str(1, d.DeviceName)
	w.uint(2, uint64(d.VendorOS)) // enum, a varint
	w.bytes(3, timestamp(d.Time))
	names := make([]string, 0, len(d.Interfaces))
	for n := range d.Interfaces {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names { // map<string, InterfaceCountersPB>: one entry message per key
		var e buf
		e.str(1, n)
		e.bytes(2, d.Interfaces[n].encode())
		w.bytes(4, e.b)
	}
	w.uint(5, uint64(d.UptimeSeconds))
	w.float(6, d.CPU)
	w.float(7, d.Memory)
	return w.b
}

// Encode validates the payload and returns the serialized PerfDataPB. The output is deterministic: interfaces are
// written in name order, devices in the order given.
func (p Payload) Encode() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var w buf
	w.bytes(1, timestamp(p.Time))
	for _, d := range p.Devices {
		w.bytes(2, d.encode())
	}
	return w.b, nil
}

// LookupVendorOS maps the OS name Forward's NQE reports (for example ARISTA_EOS, or IOS_XR for Cisco) to a VendorOsPB
// number. NQE drops the vendor prefix for some Cisco platforms, so CISCO_<name> is tried second.
func LookupVendorOS(name string) (int32, bool) {
	if v, ok := VendorOS[name]; ok {
		return v, true
	}
	v, ok := VendorOS["CISCO_"+name]
	return v, ok
}
