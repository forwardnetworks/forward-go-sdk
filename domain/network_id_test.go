package domain

import "testing"

// A network-scoped call addresses the explicit network when one is given, the bound network when it is not, and nothing at
// all when there is neither: sending "" would address /api/networks// and read as a missing network on Forward's side.
func TestResolveNetworkIDPrefersExplicitThenBoundThenRefuses(t *testing.T) {
	for _, tc := range []struct {
		explicit, bound, want string
		wantErr               bool
	}{
		{explicit: "n1", bound: "n2", want: "n1"},
		{explicit: "  n1\t", bound: "n2", want: "n1"},
		{explicit: "", bound: "n2", want: "n2"},
		{explicit: "   ", bound: "n2", want: "n2"},
		{explicit: "", bound: "", wantErr: true},
		{explicit: " ", bound: "", wantErr: true},
	} {
		got, err := ResolveNetworkID(tc.explicit, tc.bound)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ResolveNetworkID(%q, %q) = %q, want an error", tc.explicit, tc.bound, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ResolveNetworkID(%q, %q) = %q, %v; want %q", tc.explicit, tc.bound, got, err, tc.want)
		}
	}
}
