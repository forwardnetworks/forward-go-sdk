package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Predict models nothing, without an error, when the org property is off, when the device does not collect received routes, or when
// it collects only ADJ_RIB_OUT; the preflight must name each, and must still answer the device half when the org config is forbidden.
func TestBGPAdvertisementReadiness(t *testing.T) {
	config := `{"PREDICT_MODELING":true,"PREDICT_MODEL_EXT_ADV":true,"PREDICT_MODEL_BGP":true}`
	configStatus := http.StatusOK
	devices := `[{"name":"Edge1","host":"10.0.0.1","collectBgpAdvertisements":true}]`
	var counts atomic.Int32
	var classicStatus atomic.Int32
	classicStatus.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts.Add(1)
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
			if status := int(classicStatus.Load()); status != http.StatusOK {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"Missing permission: NetworkOperation.VIEW_DEVICE_CREDENTIALS"}`))
				return
			}
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
	if err != nil || r.PredictModelExtAdv != nil || len(r.Notes) != 2 || r.DeviceFound || r.Ready() || r.Blocked() {
		t.Fatalf("forbidden config / missing device: %+v, %v", r, err)
	}

	// 26.11.0-03 sends the keys in lower case (seen live): they must still be found.
	config, configStatus = `{"predict_modeling":true,"predict_model_ext_adv":false,"predict_model_bgp":true}`, http.StatusOK
	devices = `[{"name":"edge1","host":"10.0.0.1","collectBgpAdvertisements":true}]`
	r, _, err = client.Predict.BGPAdvertisementReadiness(ctx, "n1", "edge1")
	if err != nil || r.PredictModelExtAdv == nil || *r.PredictModelExtAdv || len(r.Unknown) != 0 || !r.Blocked() {
		t.Fatalf("lower-case keys: %+v, %v", r, err)
	}

	// A property the API does not return is unknown, never a pass.
	config, configStatus = `{"SOME_OTHER_PROPERTY":true}`, http.StatusOK
	devices = `[{"name":"edge1","host":"10.0.0.1","collectBgpAdvertisements":true}]`
	r, _, err = client.Predict.BGPAdvertisementReadiness(ctx, "n1", "edge1")
	if err != nil || r.Ready() || r.Blocked() || len(r.Unknown) != 3 || len(r.Notes) != 1 || r.PredictModeling != nil {
		t.Fatalf("absent properties: %+v, %v", r, err)
	}

	// A refused classic-devices or requirements read is Unknown too, not a silent pass; several devices share the three reads.
	configStatus = http.StatusOK
	config = `{"predict_modeling":true,"predict_model_ext_adv":true,"predict_model_bgp":true}`
	devices = `[{"name":"edge1","host":"10.0.0.1","collectBgpAdvertisements":true},{"name":"edge2","host":"10.0.0.2"}]`
	counts.Store(0)
	reports, _, err := client.Predict.BGPAdvertisementReadinessFor(ctx, "n1", []string{"edge1", "edge2", "ctl1"})
	if err != nil || len(reports) != 3 || !reports[0].Ready() || !reports[1].Blocked() || reports[2].Blocked() || reports[2].Ready() {
		t.Fatalf("multi-device: %+v, %v", reports, err)
	}
	if counts.Load() != 3 {
		t.Fatalf("the org-wide reads must be made once for all devices: %d requests", counts.Load())
	}
	if reports[2].UnknownReasons["collection-source:ctl1"] == "" || len(reports[2].UnknownReasons) != len(reports[2].Unknown) {
		t.Fatalf("every Unknown entry must carry its reason: %+v", reports[2])
	}
	classicStatus.Store(http.StatusForbidden)
	r, _, err = client.Predict.BGPAdvertisementReadiness(ctx, "n1", "edge1")
	if err != nil || r.Ready() || len(r.Unknown) != 1 || r.Unknown[0] != "classic-devices" || r.UnknownReasons["classic-devices"] == "" {
		t.Fatalf("refused classic-devices: %+v, %v", r, err)
	}
}
