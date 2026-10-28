package forward

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Only the two /orgs/{orgId}/platform-usage reads and the /admin reads name an org; the rest act on the credential's own org, so they
// must send no org id anywhere. The per-license CCH of ANOTHER org is in OrgCloudUsage's periods.
func TestLicenseUsageWire(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	reply := `{"devices":[{"licensed":true,"lite":true,"deviceSignature":{"x":1}}],"cloudCreditHoursUsed":12,"updatedAt":"2026-10-01T00:00:00Z","periods":[{"start":"2026-01-01T00:00:00Z","licenseId":"4153","cchUsed":30.5,"endReason":"LICENSE_REPLACED"},{"start":"2026-06-01T00:00:00Z","licenseId":null,"cchUsed":2}],"licenses":[{"id":"4153","cch":100,"replaces":true}]}`
	client := newTestClient(t, predictRecorder(t, 200, reply, &seen).URL)
	ctx := context.Background()

	cloud, _, err := client.Licensing.OrgCloudUsage(ctx, "1707")
	if err != nil || len(cloud.Periods) != 2 || cloud.Periods[0].LicenseID != "4153" || cloud.Periods[0].CCHUsed != 30.5 || cloud.Periods[1].LicenseID != "" || cloud.Periods[1].GracePeriod || cloud.Licenses[0].CCH != 100 {
		t.Fatalf("cloud = %+v, %v", cloud, err)
	}
	usage, _, err := client.Licensing.OrgUsage(ctx, "1707")
	if err != nil || usage.CloudCreditHoursUsed != 12 || !usage.Devices[0].Licensed || !usage.Devices[0].Lite {
		t.Fatalf("usage = %+v, %v", usage, err)
	}
	if _, _, err := client.Licensing.OrgUsage(ctx, " "); err == nil {
		t.Fatal("a blank org was sent")
	}
	if _, _, err := client.Licensing.Usage(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Licensing.CCHUsage(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Licensing.CCHUsedSince(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Licensing.CCHUsedSince(ctx, 0); err == nil {
		t.Fatal("zero days was sent")
	}
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, _, err := client.Licensing.CCHHistory(ctx, start, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Licensing.CCHHistory(ctx, time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Licensing.CCHDebug(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Licensing.DeviceCounts(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Licensing.LicensedDevices(ctx); err != nil {
		t.Fatal(err)
	}
	var seenHistory []predictSeen
	history := newTestClient(t, predictRecorder(t, 200, `[{"timestamp":1760000000500,"standardDeviceCap":50,"standardDeviceUsage":3,"edgeAssetUsage":2}]`, &seenHistory).URL)
	points, _, err := history.Licensing.UsageHistory(ctx, start, start.Add(time.Hour))
	if err != nil || len(points) != 1 || points[0].StandardDeviceUsage != 3 || points[0].EdgeAssetUsage != 2 || points[0].Time().UnixMilli() != 1760000000500 || points[0].Time().Year() != 2025 {
		t.Fatalf("points = %+v, %v", points, err)
	}
	if _, _, err := history.Licensing.UsageHistory(ctx, start, start); err == nil || len(seenHistory) != 1 {
		t.Fatal("an empty period was sent")
	}
	if seenHistory[0].uri != "/api/metrics/license-usage?end=2026-01-02T04%3A04%3A05Z&start=2026-01-02T03%3A04%3A05Z" {
		t.Errorf("history uri = %q", seenHistory[0].uri)
	}
	want := []string{
		"/api/orgs/1707/platform-usage?view=vpc",
		"/api/orgs/1707/platform-usage",
		"/api/platform-usage",
		"/api/platform-usage?view=cch",
		"/api/platform-usage?view=cch&days=7",
		"/api/platform-usage?start=2026-01-02T03%3A04%3A05Z&view=cch-history",
		"/api/platform-usage?view=cch-history",
		"/api/platform-usage?view=cch-debug",
		"/api/licensed-devices?view=count",
		"/api/licensed-devices",
	}
	if len(seen) != len(want) {
		t.Fatalf("seen %d requests: %+v", len(seen), seen)
	}
	for i, w := range want {
		if seen[i].method != "GET" || seen[i].uri != w {
			t.Errorf("request %d = %s %s, want GET %s", i, seen[i].method, seen[i].uri, w)
		}
	}
}

// The admin reads decode Forward's flattened device counts (the DeviceCounts record is unwrapped with a DeviceCount suffix) and keep the
// caps nil unless the org is ACTIVE, so "no cap stated" is not read as a cap of zero.
func TestLicenseAdminUsageDecoding(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	client := newTestClient(t, predictRecorder(t, 200, `[{"orgId":1707,"standardDeviceCount":40,"liteDeviceCount":5,"edgeAssetCount":9,"cloudCreditHoursUsed":120}]`, &seen).URL)
	ctx := context.Background()
	all, _, err := client.Licensing.AllOrgUsage(ctx)
	if err != nil || len(all) != 1 || all[0].OrgID != "1707" || all[0].StandardDeviceCount != 40 || all[0].LiteDeviceCount != 5 || all[0].EdgeAssetCount != 9 || all[0].CloudCreditHoursUsed != 120 {
		t.Fatalf("all = %+v, %v", all, err)
	}

	var seen2 []predictSeen
	client = newTestClient(t, predictRecorder(t, 200, `[{"orgId":1707,"tier":"NP","status":"ACTIVE","standardDeviceCap":50,"liteDeviceCap":0,"cloudCreditHours":300,"edgeAssetCap":10},{"orgId":2,"tier":null,"status":"EXPIRED"}]`, &seen2).URL)
	infos, _, err := client.Licensing.SaaSOrgInfos(ctx)
	if err != nil || len(infos) != 2 || *infos[0].StandardDeviceCap != 50 || *infos[0].LiteDeviceCap != 0 || infos[1].StandardDeviceCap != nil || infos[1].Tier != "" {
		t.Fatalf("infos = %+v, %v", infos, err)
	}
	if seen[0].uri != "/api/admin/platform-usage" || seen2[0].uri != "/api/admin/licenses?view=saas" {
		t.Fatalf("uris %q %q", seen[0].uri, seen2[0].uri)
	}
	if f := infos[0].Facets(); len(f) != 3 || f[2] != LicenseFacetPredict {
		t.Fatalf("NP facets = %v", f)
	}
}

// CCH arithmetic: over is debt, remaining is what is left, never negative; a history's last value is the newest cumulative figure.
func TestCCHUsageArithmeticAndHistory(t *testing.T) {
	t.Parallel()
	if u := (CCHUsage{CCHUsed: 130, CCHLicensed: 100}); u.Over() != 30 || u.Remaining() != 0 {
		t.Errorf("over: %d %d", u.Over(), u.Remaining())
	}
	if u := (CCHUsage{CCHUsed: 40, CCHLicensed: 100}); u.Over() != 0 || u.Remaining() != 60 {
		t.Errorf("within: %d %d", u.Over(), u.Remaining())
	}
	var seen []predictSeen
	client := newTestClient(t, predictRecorder(t, 200, `{"instanceCounts":{"2026-01-01T00:00:00Z":{"aws":3}},"licenses":[{"id":"4153","startsAt":"2026-01-01T00:00:00Z","cch":100,"replaced":true,"usage":{"2026-02-01T00:00:00Z":10,"2026-03-01T00:00:00Z":25}}]}`, &seen).URL)
	h, _, err := client.Licensing.CCHHistory(context.Background(), time.Time{}, time.Time{})
	if err != nil || len(h.Licenses) != 1 || h.Licenses[0].ID != "4153" || !h.Licenses[0].Replaced || h.Licenses[0].LastUsed() != 25 || h.Licenses[0].Remaining() != 75 || h.Licenses[0].CCH != 100 {
		t.Fatalf("history = %+v, %v", h, err)
	}
}

// A tier licence keeps its licence metadata and its tier names the facets it covers; an unknown tier covers none rather than guessing.
func TestLicenseTierFacetsAndMeta(t *testing.T) {
	t.Parallel()
	var l License
	if err := l.UnmarshalJSON([]byte(`{"id":"9","type":"TIER","tier":"NSP","createdAt":"2026-01-01T00:00:00Z","licensedTo":"Acme","purchaseOrder":"PO-1","supportEndsOn":"2027-06-30","replaces":false,"cloudCreditHours":50}`)); err != nil {
		t.Fatal(err)
	}
	if l.LicensedTo != "Acme" || l.PurchaseOrder != "PO-1" || l.SupportEndsOn != "2027-06-30" || l.CreatedAt == "" || !l.Stackable {
		t.Fatalf("license = %+v", l)
	}
	got := make([]string, 0, 4)
	for _, f := range l.Facets() {
		got = append(got, string(f))
	}
	if strings.Join(got, ",") != "BASE,NETWORK,SECURITY,PREDICT" {
		t.Fatalf("NSP facets = %v", got)
	}
	if TierFacets("X") != nil || TierFacets("") != nil {
		t.Fatal("an unknown tier named facets")
	}
}

// Each own-org read decodes Forward's own field names, not a shape this SDK made up: the device counts come out of an unwrapped record
// with the suffixes DeviceCount, UnlicensedDeviceCount and DeviceCap; the CCH figure carries a grace-period end only when one is running.
func TestOwnOrgUsageDecoding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var seen []predictSeen
	c := newTestClient(t, predictRecorder(t, 200, `{"standardDeviceCount":40,"liteDeviceCount":5,"standardUnlicensedDeviceCount":2,"liteUnlicensedDeviceCount":1,"standardDeviceCap":50,"liteDeviceCap":9}`, &seen).URL)
	d, _, err := c.Licensing.DeviceCounts(ctx)
	if err != nil || *d != (DeviceCounts{40, 5, 2, 1, 50, 9}) {
		t.Fatalf("device counts = %+v, %v", d, err)
	}
	c = newTestClient(t, predictRecorder(t, 200, `{"cchUsed":120,"cchLicensed":100,"instant":"2026-10-28T00:00:00Z","gracePeriodEndTime":"2026-11-30T00:00:00Z"}`, &seen).URL)
	u, _, err := c.Licensing.CCHUsage(ctx)
	if err != nil || u.CCHUsed != 120 || u.Over() != 20 || u.GracePeriodEndTime == "" {
		t.Fatalf("cch = %+v, %v", u, err)
	}
	c = newTestClient(t, predictRecorder(t, 200, `{"edgeAssetCount":9,"unlicensedEdgeAssetCount":3,"edgeAssetCap":10}`, &seen).URL)
	e, _, err := c.Licensing.EdgeAssetCounts(ctx)
	if err != nil || *e != (EdgeAssetCounts{9, 3, 10}) || seen[len(seen)-1].uri != "/api/licensed-edge-assets?view=count" {
		t.Fatalf("edge = %+v, %v, %v", e, err, seen)
	}
	c = newTestClient(t, predictRecorder(t, 200, `{"devices":[{"names":["a"],"networkIds":[7],"lite":true}],"unlicensedDevices":[],"networkNames":{"7":"n"},"currentTime":"2026-10-28T00:00:00Z"}`, &seen).URL)
	l, _, err := c.Licensing.LicensedDevices(ctx)
	if err != nil || len(l.Devices) != 1 || !l.Devices[0].Lite || l.Devices[0].NetworkIDs[0] != "7" || l.NetworkNames["7"] != "n" {
		t.Fatalf("licensed devices = %+v, %v", l, err)
	}
	if _, _, err := c.Licensing.CCHHistory(ctx, time.Unix(100, 0), time.Unix(50, 0)); err == nil {
		t.Fatal("an inverted period was sent")
	}
}
