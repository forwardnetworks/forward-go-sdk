package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// forward-skills' requirement: read ONE property (never the whole /api/config map, which carries ALLOWED_IPS and the password policy), find
// its value whichever case the answer's key uses, and tell an unknown property from a failure.
func TestPropertiesCurrentValue(t *testing.T) {
	var uri string
	calls := 0
	status, body := http.StatusOK, `{"predict_model_config":true}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.RequestURI()
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	raw, _, err := client.Properties.CurrentValue(ctx, " predict_model_config ")
	if err != nil || string(raw) != "true" {
		t.Fatalf("raw=%s err=%v", raw, err)
	}
	if uri != "/api/config/PREDICT_MODEL_CONFIG" {
		t.Fatalf("only the one property may be requested, upper-cased: %s", uri)
	}
	body = `{"PREDICT_MODEL_CONFIG":false}`
	if raw, _, err = client.Properties.CurrentValue(ctx, "PREDICT_MODEL_CONFIG"); err != nil || string(raw) != "false" {
		t.Fatalf("an upper-case key is found too: %s %v", raw, err)
	}

	status, body = http.StatusBadRequest, `{"message":"No enum constant com.forwardnetworks.cv.config.OrgProperty.NOT_A_PROPERTY"}`
	if _, _, err = client.Properties.CurrentValue(ctx, "NOT_A_PROPERTY"); !IsUnknownOrgProperty(err) {
		t.Fatalf("an unknown property is ErrUnknownOrgProperty: %v", err)
	}
	status, body = http.StatusOK, `{"SOMETHING_ELSE":1}`
	if _, _, err = client.Properties.CurrentValue(ctx, "PREDICT_MODEL_CONFIG"); err == nil {
		t.Fatal("an answer without the asked property must be an error, never another property's value")
	}
	for _, bad := range []string{"", "PREDICT_MODELING/../ALLOWED_IPS", "A B", "ALLOWED_IPS?x=1", "../config", "1ABC"} {
		before := calls
		if _, _, err := client.Properties.CurrentValue(ctx, OrgProperty(bad)); err == nil || calls != before {
			t.Errorf("%q must be refused before any request (err %v)", bad, err)
		}
	}
}
