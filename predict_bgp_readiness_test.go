package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Predict models nothing, without an error, when the org property is off, when the device does not collect received routes, or when
// it collects only ADJ_RIB_OUT; the preflight must name each, and must still answer the device half when the org config is forbidden.
func TestBGPAdvertisementReadiness(t *testing.T) {
	config := `{"PREDICT_MODELING":true,"PREDICT_MODEL_EXT_ADV":true}`
	configStatus := http.StatusOK
	devices := `[{"name":"Edge1","host":"10.0.0.1","collectBgpAdvertisements":true}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/config":
			w.WriteHeader(configStatus)
			if configStatus != http.StatusOK {
				_, _ = w.Write([]byte(`{"message":"Missing permission: Organization.ADMINISTER"}`))
				return
			}
			_, _ = w.Write([]byte(config))
		case "/api/networks/n1/classic-devices":
			_, _ = w.Write([]byte(devices))
		case "/api/networks/n1/bgp-collection-requirements":
			_, _ = w.Write([]byte(`{"requirements":[{"devices":["edge1"]}]}`))
		default:
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	r, _, err := client.Predict.BGPAdvertisementReadiness(ctx, "n1", "edge1")
	if err != nil || !r.Ready() || !r.RequiredByNetwork || r.BGPTableType != "BOTH" || r.PredictModelExtAdv == nil || !*r.PredictModelExtAdv {
		t.Fatalf("ready case: %+v, %v", r, err)
	}

	config = `{"PREDICT_MODELING":true,"PREDICT_MODEL_EXT_ADV":false}`
	devices = `[{"name":"edge1","host":"10.0.0.1","collectBgpAdvertisements":true,"bgpTableType":"ADJ_RIB_OUT"}]`
	r, _, err = client.Predict.BGPAdvertisementReadiness(ctx, "n1", "edge1")
	if err != nil || r.Ready() || len(r.Problems) != 2 || !strings.Contains(r.Problems[0], "PREDICT_MODEL_EXT_ADV") || !strings.Contains(r.Problems[1], "ADJ_RIB_OUT") {
		t.Fatalf("problem case: %+v, %v", r, err)
	}

	configStatus = http.StatusForbidden
	r, _, err = client.Predict.BGPAdvertisementReadiness(ctx, "n1", "nosuch")
	if err != nil || r.PredictModelExtAdv != nil || len(r.Notes) != 1 || r.DeviceFound || r.Ready() {
		t.Fatalf("forbidden config / missing device: %+v, %v", r, err)
	}
}
