package perfpayload

import "time"

// Watermarks is the caller's view of what Forward already holds, used to decide what is safe to upload. It is pure: the
// caller reads the state (for example from the device-metrics read routes, or its own store of what it sent), passes it
// in, and persists the result of Advance. Device names are matched exactly as Forward stores them.
//
// Keep Newest in ONE time basis. Forward stores each sample at its own time plus a per-batch lag (the batch timestamp
// against its own clock), so times read back from Forward's metrics routes are shifted and are not the times this package
// was given. Either track only what you sent (Advance records your own times) or only what Forward returns, not both.
type Watermarks struct {
	// Newest is the time of the newest sample held for each device that has any. A device missing from the map has no rows.
	Newest map[string]time.Time
	// Claimed lists devices the caller owns the telemetry of: it may upload onto them even though they already have rows.
	// Advance adds every device it first records, because the rows that make a device "already have data" are then the caller's own.
	Claimed map[string]bool
}

// Refusal says why part of a payload was held back.
type Refusal struct {
	Device string
	Time   time.Time
	Reason RefusalReason
}

// RefusalReason names why an entry was held back.
type RefusalReason string

const (
	// RefusedUnclaimed: the device already has rows and the caller has not claimed it, so uploading would mix a second source into it.
	RefusedUnclaimed RefusalReason = "device already has data and is not claimed"
	// RefusedStale: the entry is not newer than the device's newest sample. Forward drops an older sample without an error, and
	// a re-send at the same time does not overwrite across batches (the stored time carries the batch's lag), so it would either
	// be dropped or become a near-duplicate row that skews the next rate.
	RefusedStale RefusalReason = "not newer than the device's newest sample"
)

// Filter returns the payload without the entries that must not be sent, and a refusal for each one held back. An entry
// is refused when its device has rows and is not claimed, or when it is not strictly newer than the device's newest sample.
// The input is not changed. Entries of one device in the same payload may share a time (Forward overwrites within a batch).
func (w Watermarks) Filter(p Payload) (Payload, []Refusal) {
	out := Payload{Time: p.Time}
	var refused []Refusal
	for _, d := range p.Devices {
		newest, has := w.Newest[d.DeviceName]
		switch {
		case has && !w.Claimed[d.DeviceName]:
			refused = append(refused, Refusal{d.DeviceName, d.Time, RefusedUnclaimed})
		case has && !d.Time.After(newest):
			refused = append(refused, Refusal{d.DeviceName, d.Time, RefusedStale})
		default:
			out.Devices = append(out.Devices, d)
		}
	}
	return out, refused
}

// Advance returns the state after p was uploaded: Newest moved forward to the newest entry of each device, and every device
// that had no rows before claimed, since its rows are now the caller's. Call it with the payload that was actually sent,
// after the upload succeeded, and persist the result. The receiver is not changed.
func (w Watermarks) Advance(p Payload) Watermarks {
	next := Watermarks{
		Newest:  make(map[string]time.Time, len(w.Newest)),
		Claimed: make(map[string]bool, len(w.Claimed)),
	}
	for k, v := range w.Newest {
		next.Newest[k] = v
	}
	for k, v := range w.Claimed {
		next.Claimed[k] = v
	}
	for _, d := range p.Devices {
		cur, ok := next.Newest[d.DeviceName]
		if !ok {
			next.Claimed[d.DeviceName] = true
		}
		if !ok || d.Time.After(cur) {
			next.Newest[d.DeviceName] = d.Time
		}
	}
	return next
}
