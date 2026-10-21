package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type seenRequest struct{ method, uri, body string }

func seenRequestServer(t *testing.T, reply func(w http.ResponseWriter, r *http.Request)) (*Client, *[]seenRequest) {
	t.Helper()
	var requests []seenRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, seenRequest{r.Method, r.URL.RequestURI(), string(body)})
		reply(w, r)
	}))
	t.Cleanup(server.Close)
	return newTestClient(t, server.URL), &requests
}

// The routes are ThreatExposureController's: definitions CRUD, then analyses on the same definition id selected by action= or view=. The
// facts that matter on the wire: the snapshot is always sent (Forward would default to a latest that may not be analyzed), assetId rides
// as a query parameter, paging is a POST body only when set, and an EMPTY filter list in a patch is sent as [] because it means "select
// all" while an absent one means "unchanged".
func TestThreatExposureWire(t *testing.T) {
	t.Parallel()

	client, requests := seenRequestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/threat-exposure-definitions"):
			_, _ = w.Write([]byte(`{"definitions":[{"definition":{"id":"6f1c","name":"edge","exposurePoints":[{"exposurePoint":"internet","level":"HIGH"}],"criticalities":["CROWN_JEWEL"],"maxDurationSeconds":600,"createdById":"7","createdAt":"2026-10-01T00:00:00Z"}}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/threat-exposure-definitions") && r.URL.Query().Get("action") == "":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"6f1c","name":"edge","exposurePoints":[{"exposurePoint":"internet","level":"HIGH"}],"maxDurationSeconds":600}`))
		case r.Method == http.MethodPost && r.URL.Query().Get("action") == "evaluate":
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch, r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = w.Write([]byte(`{"rows":[]}`))
		}
	})
	ctx := context.Background()
	te := client.ThreatExposure

	list, _, err := te.ListDefinitions(ctx, "N1")
	if err != nil || len(list) != 1 || list[0].Definition.ID != "6f1c" || list[0].Definition.ExposurePoints[0].Level != ExposureHigh || list[0].Definition.MaxDurationSeconds != 600 || list[0].LatestAnalyzedSnapshotID != "" {
		t.Fatalf("list: %+v %v", list, err)
	}
	created, _, err := te.CreateDefinition(ctx, "N1", NewThreatExposureDefinition{Name: "edge", ExposurePoints: []ExposurePointAndLevel{{"internet", ExposureHigh}}, Criticalities: []string{CriticalityCrownJewel}, MaxDurationSeconds: 600})
	if err != nil || created.ID != "6f1c" {
		t.Fatalf("create: %+v %v", created, err)
	}
	if _, err := te.UpdateDefinition(ctx, "N1", "6f1c", ThreatExposureDefinitionPatch{Locations: []string{}, Name: Ptr("renamed")}); err != nil {
		t.Fatal(err)
	}
	if _, err := te.Evaluate(ctx, "N1", "6f1c", "S1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := te.Status(ctx, "N1", "6f1c", "S1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := te.CriticalAssetsSummary(ctx, "N1", "6f1c", "S1", ThreatExposurePaging{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := te.DirectExposureDetails(ctx, "N1", "6f1c", "S1", 42); err != nil {
		t.Fatal(err)
	}
	if _, _, err := te.TransitiveExposureDetails(ctx, "N1", "6f1c", "S1", 42, ThreatExposurePaging{Offset: 10, Limit: 5}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := te.ResolveDefinition(ctx, "N1", "S1", NewThreatExposureDefinition{Name: "preview", ExposurePoints: []ExposurePointAndLevel{{"internet", ExposureSevere}}, MaxDurationSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	if _, err := te.DeleteDefinition(ctx, "N1", "6f1c"); err != nil {
		t.Fatal(err)
	}

	base := "/api/networks/N1/threat-exposure-"
	want := []seenRequest{
		{"GET", base + "definitions", ""},
		{"POST", base + "definitions", `{"name":"edge","exposurePoints":[{"exposurePoint":"internet","level":"HIGH"}],"criticalities":["CROWN_JEWEL"],"maxDurationSeconds":600}`},
		{"PATCH", base + "definitions/6f1c", `{"locations":[],"name":"renamed"}`},
		{"POST", base + "analyses/6f1c?action=evaluate&snapshotId=S1", ""},
		{"GET", base + "analyses/6f1c?snapshotId=S1&view=status", ""},
		{"POST", base + "analyses/6f1c?snapshotId=S1&view=criticalAssetsExposureSummary", ""},
		{"GET", base + "analyses/6f1c?assetId=42&snapshotId=S1&view=criticalAssetDirectExposureDetails", ""},
		{"POST", base + "analyses/6f1c?assetId=42&snapshotId=S1&view=criticalAssetTransitiveExposureDetails", `{"limit":5,"offset":10}`},
		{"POST", base + "definitions?action=resolve&snapshotId=S1&view=summary", `{"name":"preview","exposurePoints":[{"exposurePoint":"internet","level":"SEVERE"}],"maxDurationSeconds":60}`},
		{"DELETE", base + "definitions/6f1c", ""},
	}
	if len(*requests) != len(want) {
		t.Fatalf("%d requests, want %d: %+v", len(*requests), len(want), *requests)
	}
	for i, w := range want {
		if (*requests)[i] != w {
			t.Errorf("request %d = %+v, want %+v", i, (*requests)[i], w)
		}
	}
}

func TestThreatExposureRefusesWhatForwardRefuses(t *testing.T) {
	t.Parallel()

	client, requests := seenRequestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ctx := context.Background()
	te := client.ThreatExposure
	point := ExposurePointAndLevel{"internet", ExposureHigh}
	ok := NewThreatExposureDefinition{Name: "d", ExposurePoints: []ExposurePointAndLevel{point}, MaxDurationSeconds: 60}
	bad := map[string]NewThreatExposureDefinition{
		"blank name":           {Name: " ", ExposurePoints: ok.ExposurePoints, MaxDurationSeconds: 60},
		"no exposure points":   {Name: "d", MaxDurationSeconds: 60},
		"five exposure points": {Name: "d", MaxDurationSeconds: 60, ExposurePoints: []ExposurePointAndLevel{{"a", ExposureHigh}, {"b", ExposureHigh}, {"c", ExposureHigh}, {"d", ExposureHigh}, {"e", ExposureHigh}}},
		"duplicate point":      {Name: "d", MaxDurationSeconds: 60, ExposurePoints: []ExposurePointAndLevel{point, point}},
		"unknown level":        {Name: "d", MaxDurationSeconds: 60, ExposurePoints: []ExposurePointAndLevel{{"a", "EXTREME"}}},
		"missing duration":     {Name: "d", ExposurePoints: ok.ExposurePoints},
		"unknown criticality":  {Name: "d", MaxDurationSeconds: 60, ExposurePoints: ok.ExposurePoints, Criticalities: []string{"VIP"}},
	}
	for name, def := range bad {
		if _, _, err := te.CreateDefinition(ctx, "N1", def); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, err := te.UpdateDefinition(ctx, "N1", "6f1c", ThreatExposureDefinitionPatch{}); err == nil {
		t.Error("an empty patch must be refused")
	}
	for name, patch := range map[string]ThreatExposureDefinitionPatch{
		"empty exposure points": {ExposurePoints: []ExposurePointAndLevel{}},
		"blank name":            {Name: Ptr(" ")},
		"unknown criticality":   {Criticalities: []string{"VIP"}},
	} {
		if _, err := te.UpdateDefinition(ctx, "N1", "6f1c", patch); err == nil {
			t.Errorf("a patch with %s must be refused", name)
		}
	}
	if _, err := te.UpdateDefinition(ctx, "N1", "6f1c", ThreatExposureDefinitionPatch{MaxDurationSeconds: Ptr(int64(0))}); err == nil {
		t.Error("a zero duration must be refused")
	}
	if _, err := te.Evaluate(ctx, "N1", "6f1c", ""); err == nil {
		t.Error("evaluate needs an explicit snapshot")
	}
	if _, _, err := te.Status(ctx, "N1", "6f1c", " "); err == nil {
		t.Error("status needs an explicit snapshot")
	}
	if _, _, err := te.CriticalAssetsSummary(ctx, "N1", "6f1c", "S1", ThreatExposurePaging{Limit: 10001}); err == nil {
		t.Error("a limit above 10000 must be refused")
	}
	if _, err := te.DeleteDefinition(ctx, "N1", ""); err == nil {
		t.Error("a definition id is required")
	}
	if len(*requests) != 0 {
		t.Errorf("refused calls sent %d request(s)", len(*requests))
	}
}

func TestEdgeAssetsWire(t *testing.T) {
	t.Parallel()

	client, requests := seenRequestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"edgeAssets":[{"name":"web 1","include":true,"ips":["10.0.0.5"],"macAddress":"aa:bb:cc:dd:ee:ff","criticality":"CROWN_JEWEL","enrichmentSources":["RAPID7"],"includedInLatestSnapshot":true,"createdById":"7"}]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	ctx := context.Background()
	ea := client.EdgeAssets
	assets, _, err := ea.List(ctx, "N1")
	if err != nil || len(assets) != 1 || assets[0].Name != "web 1" || !assets[0].Include || assets[0].Criticality != CriticalityCrownJewel || assets[0].EnrichmentSources[0] != "RAPID7" || !assets[0].IncludedInLatestSnapshot {
		t.Fatalf("list: %+v %v", assets, err)
	}
	if _, err := ea.Update(ctx, "N1", "web 1", EdgeAssetPatch{Include: Ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := ea.SetCriticality(ctx, "N1", []string{"web 1", " ", "db"}, CriticalityBusinessCritical); err != nil {
		t.Fatal(err)
	}
	if _, err := ea.RemoveCriticality(ctx, "N1", []string{"web 1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ea.Delete(ctx, "N1", "web 1"); err != nil {
		t.Fatal(err)
	}
	want := []seenRequest{
		{"GET", "/api/networks/N1/edge-assets", ""},
		{"PATCH", "/api/networks/N1/edge-assets/web%201", `{"include":false}`},
		{"PATCH", "/api/networks/N1/assets?action=modifyCriticality", `{"names":["web 1","db"],"criticality":"BUSINESS_CRITICAL"}`},
		// Removal is the same route with an explicit null, which Forward reads as "remove the criticality".
		{"PATCH", "/api/networks/N1/assets?action=modifyCriticality", `{"names":["web 1"],"criticality":null}`},
		{"DELETE", "/api/networks/N1/edge-assets/web%201", ""},
	}
	if len(*requests) != len(want) {
		t.Fatalf("%d requests: %+v", len(*requests), *requests)
	}
	for i, w := range want {
		if (*requests)[i] != w {
			t.Errorf("request %d = %+v, want %+v", i, (*requests)[i], w)
		}
	}
	before := len(*requests)
	if _, err := ea.Update(ctx, "N1", "web 1", EdgeAssetPatch{}); err == nil {
		t.Error("an empty patch must be refused")
	}
	if _, err := ea.Update(ctx, "N1", "web 1", EdgeAssetPatch{Name: "  "}); err == nil {
		t.Error("a blank rename must be refused")
	}
	if _, err := ea.SetCriticality(ctx, "N1", []string{"web 1"}, "VIP"); err == nil {
		t.Error("an unknown criticality must be refused")
	}
	if _, err := ea.SetCriticality(ctx, "N1", []string{" "}, CriticalityCrownJewel); err == nil {
		t.Error("at least one asset name is required")
	}
	if _, err := ea.Delete(ctx, "N1", ""); err == nil {
		t.Error("an asset name is required")
	}
	if len(*requests) != before {
		t.Errorf("refused calls sent requests")
	}
}
