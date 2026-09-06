package forward

import (
	"context"
	"net/http"
	"testing"
)

// Forward has returned a classic-device list as a bare array, as a single
// object, and wrapped under several different keys. A reader that knows only
// one of those shapes does not error on the others -- it returns an EMPTY
// list, and an empty list is indistinguishable from "this network has no
// classic devices". Skyforge prunes by diffing this list against the
// topology, so empty means "nothing to prune" and the drift survives.
func TestClassicDevicesListAcceptsEveryEnvelopeShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"bare array", `[{"name":"a"},{"name":"b"}]`, 2},
		{"wrapped devices", `{"devices":[{"name":"a"}]}`, 1},
		{"wrapped classicDevices", `{"classicDevices":[{"name":"a"},{"name":"b"}]}`, 2},
		{"wrapped items", `{"items":[{"name":"a"}]}`, 1},
		{"wrapped data", `{"data":[{"name":"a"}]}`, 1},
		{"wrapped results", `{"results":[{"name":"a"}]}`, 1},
		{"single object", `{"name":"only-one"}`, 1},
		{"empty array", `[]`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			})
			got, _, err := c.ClassicDevices.List(context.Background(), "net-1")
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != tc.want {
				t.Fatalf("got %d devices, want %d (an under-read here reads as 'nothing to prune')", len(got), tc.want)
			}
		})
	}
}
