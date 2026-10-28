package perfpayload

import (
	"testing"
	"time"
)

// A device with rows is only uploaded onto when the caller claimed it, and even then nothing that is not newer than its
// newest sample goes out (older is dropped by Forward without telling anyone, and a same-time re-send in a later batch does
// not overwrite). A device with no rows is free.
func TestWatermarkFilter(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	w := Watermarks{
		Newest:  map[string]time.Time{"owned": t0, "foreign": t0},
		Claimed: map[string]bool{"owned": true},
	}
	in := Payload{Time: t0.Add(time.Hour), Devices: []DevicePerf{
		{DeviceName: "new", Time: t0.Add(-time.Hour)},
		{DeviceName: "foreign", Time: t0.Add(time.Minute)},
		{DeviceName: "owned", Time: t0.Add(-time.Second)},
		{DeviceName: "owned", Time: t0},
		{DeviceName: "owned", Time: t0.Add(time.Minute)},
	}}
	out, refused := w.Filter(in)
	if len(in.Devices) != 5 {
		t.Fatal("Filter changed its input")
	}
	if !out.Time.Equal(in.Time) || len(out.Devices) != 2 || out.Devices[0].DeviceName != "new" || !out.Devices[1].Time.Equal(t0.Add(time.Minute)) {
		t.Fatalf("kept = %+v", out.Devices)
	}
	if len(refused) != 3 || refused[0].Device != "foreign" || refused[0].Reason != RefusedUnclaimed || refused[1].Reason != RefusedStale || refused[2].Reason != RefusedStale {
		t.Fatalf("refused = %+v", refused)
	}
	var none Watermarks
	if out, refused := none.Filter(in); len(out.Devices) != 5 || len(refused) != 0 {
		t.Fatalf("with no state nothing is held back: %+v %+v", out.Devices, refused)
	}
}

// Advance moves the watermark to what was sent without touching the caller's state, and claims the devices it created rows for:
// without that the next Filter would refuse the caller's own devices as "already has data".
func TestWatermarkAdvance(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	w := Watermarks{Newest: map[string]time.Time{"a": t0, "b": t0}}
	got := w.Advance(Payload{Devices: []DevicePerf{
		{DeviceName: "a", Time: t0.Add(time.Minute)}, {DeviceName: "a", Time: t0.Add(time.Second)},
		{DeviceName: "b", Time: t0.Add(-time.Minute)}, {DeviceName: "c", Time: t0},
	}})
	next := got.Newest
	if !next["a"].Equal(t0.Add(time.Minute)) || !next["b"].Equal(t0) || !next["c"].Equal(t0) {
		t.Fatalf("next = %v", next)
	}
	if !got.Claimed["c"] || got.Claimed["a"] || got.Claimed["b"] {
		t.Fatalf("claimed = %v, want only the device Advance created (c)", got.Claimed)
	}
	if !w.Newest["a"].Equal(t0) || len(w.Newest) != 2 {
		t.Fatalf("Advance changed the caller's map: %v", w.Newest)
	}
}

// The loop a caller follows -- Filter, upload, Advance, persist -- must not lock the caller out of the devices it created.
func TestWatermarkLoopKeepsTheCallersOwnDevices(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	var state Watermarks
	for cycle := 0; cycle < 3; cycle++ {
		in := Payload{Time: t0, Devices: []DevicePerf{{DeviceName: "d", Time: t0.Add(time.Duration(cycle) * time.Minute)}}}
		out, refused := state.Filter(in)
		if len(out.Devices) != 1 || len(refused) != 0 {
			t.Fatalf("cycle %d: kept %d, refused %+v", cycle, len(out.Devices), refused)
		}
		state = state.Advance(out)
	}
}
