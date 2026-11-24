package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forwardnetworks/forward-go-sdk/domain/perfpayload"
)

// TestCoverageManifestRoutesMatchTheWire calls every manifest symbol against
// a recording server and fails when the method sends a request the manifest
// does not declare for it.
//
// TestCoverageManifestIsComplete proves a method issues SOME request; it
// cannot see WHICH. A wrong route in the manifest therefore passed every
// check: NQE.ListQueries was declared as GET
// /api/nqe/repos/fwd/commits/head/queries from the first commit while it
// sent GET /api/nqe/queries, so Skyforge's route-liveness gate spent that
// whole time checking a route the method never calls.
//
// Arguments are synthesised by reflection (non-empty strings, one-element
// slices, filled structs). A method whose validation rejects them sends
// nothing and is skipped rather than failed -- the check is "everything sent
// is declared", which a wrong manifest route cannot survive once the method
// is exercised. minExercised stops that escape hatch from silently widening.
func TestCoverageManifestRoutesMatchTheWire(t *testing.T) {
	const minExercised = 765

	symbols := make([]string, 0, len(sdkCoverageCatalog))
	for symbol := range sdkCoverageCatalog {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	var mu sync.Mutex
	exercised := 0
	t.Run("symbols", func(t *testing.T) {
		for _, symbol := range symbols {
			symbol := symbol
			t.Run(symbol, func(t *testing.T) {
				t.Parallel()
				sent := recordRequests(t, symbol)
				if len(sent) == 0 {
					t.Skip("synthesised arguments sent no request")
				}
				mu.Lock()
				exercised++
				mu.Unlock()
				declared := sdkCoverageCatalog[symbol]
				for _, request := range sent {
					if !routeDeclared(declared, request.method, request.path) {
						t.Errorf("%s sends %s %s, which the manifest does not declare for it (declared: %v)",
							symbol, request.method, request.path, declared)
					}
				}
			})
		}
	})
	if exercised < minExercised {
		t.Errorf("only %d of %d manifest symbols sent a request with synthesised arguments; want at least %d",
			exercised, len(symbols), minExercised)
	}
	t.Logf("%d of %d manifest symbols exercised", exercised, len(symbols))
}

type wireRequest struct{ method, path string }

// wireArgOverrides pins arguments the synthesiser cannot guess: values the
// method validates against an enum, and dynamic segments the manifest pins to
// the one value its consumer sends. Keyed by symbol, then argument index
// (index 0 is the context).
var wireArgOverrides = map[string]map[int]any{
	"EdgeAssets.AddInferredHosts":                           {3: []int64{101, 102}},
	"CloudManagedSetups.CreateMeraki":                       {2: domain.NewMerakiSetup{Name: "m1", APIKeyID: "k1", CredentialID: "c1", TestResult: &domain.MerakiTestResult{SavedAt: "2026-01-01T00:00:00Z"}}},
	"CloudManagedSetups.PatchMeraki":                        {3: domain.MerakiSetupPatch{Collect: domain.Ptr(false)}},
	"Performance.PatchSettings":                             {2: domain.PerformanceSettingsPatch{Enabled: domain.Ptr(true)}},
	"Performance.RawSNMPData":                               {2: domain.RawSNMPRequest{Device: "d1", CredentialID: "c1", IncludeDevicePerfData: true}},
	"Credentials.UpdateSNMPCredential":                      {3: domain.SNMPCredentialPatch{Port: domain.Ptr(161)}},
	"Diffs.Count":                                           {3: domain.DiffDevices},
	"Properties.ClearOrganization":                          {2: domain.OrgProperty("software_central")},
	"SaseSetups.Create":                                     {2: domain.ZscalerSetup{Name: "z1", BaseURL: "https://x"}},
	"Decorators.Put":                                        {2: domain.DecoratorPut{Type: domain.DecoratorEntityDevice}},
	"Decorators.Decorate":                                   {3: domain.DecoratorEntityDevice, 4: domain.DecorateRequest{EntityIDs: []string{"r1"}, DecoratorNames: []string{"d"}}},
	"Integrations.AddCheckIncident":                         {3: domain.ServiceNowIncidentInput{Summary: "s", Impact: domain.ServiceNowIncidentLevelHigh, Urgency: domain.ServiceNowIncidentLevelLow}},
	"Integrations.AddCheckIncidents":                        {2: map[string]domain.ServiceNowIncidentInput{"c1": {Summary: "s", Impact: domain.ServiceNowIncidentLevelHigh, Urgency: domain.ServiceNowIncidentLevelLow}}},
	"Integrations.UpdateServiceNow":                         {1: domain.ServiceNowSettingsUpdate{Enabled: new(bool)}},
	"NQERepository.ExportOrgSources":                        {1: []string{"/"}},
	"NQERepository.ExportForwardSources":                    {1: []string{"/"}},
	"Trace.Facets":                                          {3: domain.TraceQuery{SrcIP: "10.0.0.1", DstIP: "10.0.0.2"}},
	"Trace.Trace":                                           {3: domain.TraceQuery{SrcIP: "10.0.0.1", DstIP: "10.0.0.2"}, 4: domain.TraceSelection{Path: []string{"r1"}, TraceType: "VALID"}},
	"Trace.Explain":                                         {3: domain.TraceQuery{SrcIP: "10.0.0.1", DstIP: "10.0.0.2"}},
	"Properties.PatchNetworkOverrides":                      {2: map[string]any{"n1": true}},
	"Users.SetNetworkRole":                                  {3: domain.NetworkRoleAdmin},
	"Users.AddNetworkRole":                                  {3: domain.NetworkRoleAdmin},
	"Users.RemoveNetworkRole":                               {3: domain.NetworkRoleAdmin},
	"AccessControl.SetGroupNetworkRole":                     {3: domain.NetworkRoleAdmin},
	"Vulnerabilities.ListDevices":                           {2: domain.DeviceVulnerabilityListOptions{Severity: domain.VulnerabilitySeverityHigh, Age: domain.CVEAgeYear}},
	"Networks.SetSnapshotRetentionPolicy":                   {2: domain.SnapshotRetentionPolicy{Enabled: true, LastWeek: domain.SnapshotRetainAll, LastMonth: domain.SnapshotRetainPerDay, LastQuarter: domain.SnapshotRetainPerWeek, LastYear: domain.SnapshotRetainPerMonth, Older: domain.SnapshotRetainPerQuarter}},
	"Endpoints.UpdateProfile":                               {1: "CLI-1", 2: domain.EndpointProfilePatch{Prompt: domain.Ptr("#")}},
	"DeviceTags.UpdateTag":                                  {3: domain.DeviceTagPatch{Color: domain.Ptr("#00ff00")}},
	"CollectionSchedules.Create":                            {2: domain.CollectionScheduleDefinition{Enabled: true, DaysOfTheWeek: []int{1}, Times: []string{"02:00"}}},
	"CollectionSchedules.Replace":                           {3: domain.CollectionScheduleDefinition{Enabled: true, DaysOfTheWeek: []int{1}, Times: []string{"02:00"}}},
	"Snapshots.Export":                                      {2: domain.SnapshotExportOptions{IncludeDevices: []string{"d1"}}, 3: io.Discard},
	"Integrations.UpdateInfoblox":                           {1: "7"},
	"Integrations.DeleteInfoblox":                           {1: "7"},
	"Webhooks.Update":                                       {2: domain.WebhookPatch{Enabled: domain.Ptr(true)}},
	"CloudAccounts.Patch":                                   {3: domain.CloudAccountPatch{Type: "AWS"}},
	"Collectors.Metrics":                                    {2: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 3: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), 4: 0},
	"Collectors.PatchOrganizationSettings":                  {1: domain.OrgCollectionSettingsPatch{CommandDelayMS: domain.Ptr(1)}},
	"ClassicDevices.PatchMany":                              {2: []string{"sw1"}, 3: domain.ClassicDevicePatch{Note: domain.Ptr("v1")}},
	"ClassicDevices.GetMany":                                {2: []string{"sw1"}},
	"ConnectivityTests.BulkStart":                           {2: []string{"sw1"}},
	"Performance.UploadPayload":                             {4: perfpayload.Payload{Time: time.Unix(1, 0)}},
	"Performance.UploadPayloadWithIdentity":                 {3: perfpayload.Payload{Time: time.Unix(1, 0)}},
	"Integrations.CreateTenableSC":                          {2: domain.TenableSourceRequest{Name: "sc", BaseURL: "https://sc.example", CredentialID: "H-5"}},
	"Integrations.TestTenableSC":                            {2: domain.TenableSourceTest{Name: "sc", BaseURL: "https://sc.example", CredentialID: "H-5"}},
	"Predict.CommitWithOptions":                             {3: domain.CommitOptions{Note: "n", AnalysisMode: domain.AnalysisModeConfigOnly}},
	"Integrations.CreateChangeSetChangeRequest":             {3: domain.ServiceNowChangeRequestInput{Summary: "s", Impact: domain.ServiceNowImpactHigh}},
	"Integrations.DownloadChangeSetChangeRequestAttachment": {5: io.Discard},
	"Predict.RunIEntryChecks":                               {2: []domain.IEntryCheck{{Name: "c", DeviceName: "r1", Entry: json.RawMessage(`{}`), EntryType: domain.IEntryTypeIPv4, CheckType: domain.IEntryCheckExists}}},
	"Licensing.CCHUsedSince":                                {1: 7},
	"Licensing.CCHHistory":                                  {1: time.Unix(1, 0), 2: time.Unix(100, 0)},
	"Licensing.UsageHistory":                                {1: time.Unix(1, 0), 2: time.Unix(100, 0)},
	"Predict.CreateChangeSet":                               {2: domain.ChangeSetCreateRequest{Name: "n", SnapshotID: "s", Directory: "/a"}},
	"Predict.AddChangeSetDirectory":                         {2: "/", 3: "d"},
	"Predict.RenameChangeSetDirectory":                      {2: "/a", 3: "b"},
	"Predict.MoveChangeSetDirectory":                        {2: "/a", 3: "/"},
	"Predict.MoveChangeSetToDirectory":                      {2: "/a"},
	"Predict.MoveChangeSetsToDirectory":                     {2: "/a", 3: []string{"1"}},
	"Predict.DeleteChangeSetDirectory":                      {2: "/a"},
	"Predict.DeleteChangeSets":                              {2: []string{"1"}},
	"Predict.PatchChangeSetsTags":                           {2: []string{"1"}, 3: domain.ChangeSetTagsAdd, 4: []string{"t"}},
	"Predict.AllowedCommands":                               {2: domain.PredictProtocolBGP},
	"Predict.FirewallObjects":                               {4: domain.FirewallZones},
	"Predict.ExportChangeSetZip":                            {3: io.Discard},
	"Predict.BGPRules":                                      {4: "10.0.0.2"},
	"Integrations.ListNetworkVulnerabilityScanners":         {1: "n1"},
	"Integrations.SetNetworkVulnerabilityScannerInclude":    {1: "n1", 2: "tvm", 3: true},
	"Integrations.CreateVulnerabilityScanner":               {1: domain.NewVulnerabilityScanner{Name: "tvm", CredentialID: "h1", CollectorID: "c1", Schedule: domain.IntervalSchedule{IntervalDays: 1}, AllowedForAllNetworks: true}},
	"Integrations.UpdateVulnerabilityScanner":               {1: "tvm", 2: domain.VulnerabilityScannerPatch{CollectionEnabled: new(bool)}},
	"Integrations.DeleteVulnerabilityScanner":               {1: "tvm"},
	"Integrations.CollectVulnerabilityScanner":              {1: "tvm"},
	"Integrations.TestVulnerabilityScanner":                 {1: domain.VulnerabilityScannerTest{Name: "tvm", CredentialID: "h1", CollectorID: "c1"}},
	"Credentials.CreateOrgHTTP":                             {1: domain.HTTPCredentialRequest{Name: "tvm", Username: "AK", Password: "SK"}},
	"Credentials.GetOrgHTTP":                                {1: "H-0"},
	"Credentials.UpdateOrgHTTP":                             {1: "H-0"},
	"Credentials.DeleteOrgHTTP":                             {1: "H-0"},
	"Trace.FacetsQuery":                                     {3: domain.TracePathQuery{From: &domain.TraceEndpoint{Location: domain.TraceDevice("d1")}}},
	"Routing.PrefixSearch":                                  {3: domain.RoutingPrefixSearch{Prefix: "10.0.0.0/8"}},
	"Routing.PrefixTrace":                                   {3: domain.RoutingTraceRequest{Prefix: "10.0.0.0/8", Router: domain.DeviceVRF{Device: "r1"}, OriginatedFrom: domain.DeviceVRF{Device: "r2"}, Outcome: domain.RouteOutcomeInstalled}},
	"Routing.PrefixDetails":                                 {3: domain.RoutingPrefixDetailsRequest{Prefix: "10.0.0.0/8", Node: domain.RoutingNode{Device: "r1"}}},
	"Routing.RPPrefixSearch":                                {3: domain.RPPrefixSearch{Prefix: "10.0.0.0/8"}},
	"Routing.RPPrefixTrace":                                 {3: domain.RPPrefixTrace{Prefix: "10.0.0.0/8", Router: domain.DeviceVRFProtocol{Device: "r1", Protocol: "BGP"}, OriginatedFrom: domain.DeviceVRFProtocol{Device: "r2", Protocol: "BGP"}, Outcome: domain.RouteOutcomeInstalled}},
	"Routing.RPPrefixDetails":                               {3: domain.RPPrefixDetailsRequest{Prefix: "10.0.0.0/8", Node: domain.RPNodeRef{NodeKey: json.RawMessage(`{"protocol":"BGP","device":"r1"}`)}}},
	"Routing.InstalledPrefixes":                             {3: domain.RoutingInstalledOptions{Device: "r1"}},
	"ThreatExposure.CreateDefinition":                       {2: domain.NewThreatExposureDefinition{Name: "d", ExposurePoints: []domain.ExposurePointAndLevel{{ExposurePoint: "edge", Level: domain.ExposureHigh}}, MaxDurationSeconds: 60}},
	"ThreatExposure.ResolveDefinition":                      {3: domain.NewThreatExposureDefinition{Name: "d", ExposurePoints: []domain.ExposurePointAndLevel{{ExposurePoint: "edge", Level: domain.ExposureHigh}}, MaxDurationSeconds: 60}},
	"ThreatExposure.UpdateDefinition":                       {3: domain.ThreatExposureDefinitionPatch{Name: domain.Ptr("d")}},
	"EdgeAssets.SetCriticality":                             {3: domain.CriticalityCrownJewel},
	"Networks.PathsBulk":                                    {2: []domain.PathQuery{{SrcIP: "10.0.0.1", DstIP: "10.0.0.2"}}, 3: domain.PathBulkOptions{}},
	"Predict.AddChangeSetCheck":                             {3: domain.NewChangeSetCheck{Definition: domain.ChangeSetCheckDefinition{CheckType: domain.ChangeSetCheckNQE, QueryID: "Q_1"}}},
	"Predict.UpdateChangeSetCheck":                          {4: domain.ChangeSetCheckPatch{ClearName: true}},
	"Networks.InterestingPaths":                             {3: domain.InterestingPathsFirewall},
	"Predict.WithdrawBGPAdvertisement":                      {4: domain.BGPAdvertisementKey{VRF: "default", ExternalPeer: "192.0.2.1", Prefix: "10.0.0.0/8"}},
	"Predict.WithdrawBGPAdvertisements":                     {4: []domain.BGPAdvertisementKey{{VRF: "default", ExternalPeer: "192.0.2.1", Prefix: "10.0.0.0/8"}}},
	"Predict.RestoreBGPAdvertisement":                       {4: domain.BGPAdvertisementKey{VRF: "default", ExternalPeer: "192.0.2.1", Prefix: "10.0.0.0/8"}},
	"Predict.RestoreBGPAdvertisements":                      {4: []domain.BGPAdvertisementKey{{VRF: "default", ExternalPeer: "192.0.2.1", Prefix: "10.0.0.0/8"}}},
	"Predict.EditSecurityRule":                              {7: domain.SecurityRuleDefinition{Name: "r"}},
	"ControllerManagedSetups.Patch":                         {3: domain.ControllerManagedSetupPatch{CollectorID: "C4"}},
	"CustomCommandGroups.Create":                            {2: domain.NewCustomCommandGroup{Name: "g", DeviceConnTypes: []string{"cisco_ios_xe_ssh"}, Commands: []string{"show version"}}},
	"CustomCommandGroups.Validate":                          {2: []string{"cisco_ios_xe_ssh"}, 3: []string{"show version"}},
	"ClassicDevices.ResolveGlobs":                           {2: []string{"edge*"}, 3: []string{"cisco_ios_xe_ssh"}},
	"ConnectivityTests.BulkStop":                            {2: []string{"sw1"}},
	"DeviceDiscovery.Start":                                 {3: domain.SubnetScanParams{Subnets: []string{"10.0.0.0/24"}}},
	"DeviceDiscovery.CreateScheduled":                       {2: domain.ScheduledDiscoveryDefinition{Name: "d", Params: domain.SeededDiscoveryParams{SeedDevices: []string{"10.0.0.1"}}}},
	"ControllerManagedSetups.Test":                          {3: domain.ControllerAndManagedDeviceSelection{SetupNames: []string{"sdwan"}}},
	"NQE.RunAll":                                            {3: domain.NQEQueryRequest{Query: "v1"}, 4: domain.NQEPagedOptions{KeyColumn: "name"}},
	"ClassicDevices.Patch":                                  {3: domain.ClassicDevicePatch{Note: domain.Ptr("v1")}},
	"ClassicDevices.CreateWithAPIKey":                       {2: domain.HTTPCredentialRequest{Name: "k", Password: "t"}, 3: domain.ClassicDeviceRequest{Name: "k8s1", Host: "h"}},
	"Snapshots.MergeIntoLatest":                             {2: wireArgFresh(func() any { return domain.SnapshotUploadFile{Name: "s.zip", Reader: strings.NewReader("PK")} }), 3: domain.SnapshotMergeOptions{}},
	"ControllerManagedSetups.PatchController":               {4: domain.ClassicDevicePatch{Note: domain.Ptr("v1")}},
	"ControllerManagedSetups.PatchManagedDevice":            {4: domain.ClassicDevicePatch{Note: domain.Ptr("v1")}},
	"Backups.DeleteBackup":                                  {1: int64(7), 2: domain.StorageTypeAll},
	"Scorecards.Trends":                                     {2: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), 3: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 4: 30},
	"Checks.ChecksReport":                                   {2: domain.ChecksReportOptions{}, 3: io.Discard},
	"Checks.CheckCategoryReport":                            {2: "NQE", 3: "", 4: domain.ChecksReportOptions{}, 5: io.Discard},
	"Aliases.Put":                                           {2: domain.AliasBuilder{Name: "v1", Type: domain.AliasTypeDevices, Values: []string{"d1"}}},
	"AuditLogs.List":                                        {1: domain.AuditLogListOptions{HTTPMethod: "POST"}},
	"DataConnectors.Add":                                    {2: domain.NewDataConnector{Name: "v1", BaseURL: "https://v1", Endpoints: []domain.HTTPEndpoint{{Name: "v1", Path: "/v1"}}}},
	"DataConnectors.Update":                                 {3: domain.DataConnectorPatch{Collect: domain.Ptr(true)}},
	"DataFiles.InferSchema":                                 {3: domain.DataFileCSV},
	"DataFiles.Add":                                         {1: domain.DataFileCreateRequest{Name: "v1", FileType: domain.DataFileCSV}},
	// The kind-based synthetic node calls address a different route per
	// kind; L3 VPN reaches every one of them.
	"SyntheticNodes.Get":                {2: domain.SyntheticL3VPN},
	"SyntheticNodes.Put":                {2: domain.SyntheticL3VPN},
	"SyntheticNodes.Delete":             {2: domain.SyntheticL3VPN},
	"SyntheticNodes.List":               {2: domain.SyntheticL3VPN},
	"SyntheticNodes.SetQuery":           {2: domain.SyntheticL3VPN},
	"SyntheticNodes.ComputeQuery":       {2: domain.SyntheticL3VPN},
	"SyntheticNodes.CompatibleQueries":  {2: domain.SyntheticL3VPN},
	"SyntheticNodes.Backdate":           {2: domain.SyntheticL3VPN},
	"Endpoints.CreateProfileDefinition": {1: domain.EndpointProfile{Name: "v1", Type: "SNMP"}},
	// AddBatch and Patch refuse a field the endpoint type does not have, and
	// the synthesiser fills every field, so they get a valid type and a
	// minimal endpoint.
	"Endpoints.AddBatch": {2: "CLI", 3: []domain.Endpoint{{Name: "v1", Host: "v1"}}},
	"Endpoints.Patch":    {3: "CLI"},
}

// wireArgFresh wraps an override that must be built anew for every call, such as one holding a reader: a shared reader is read
// once, so every later call (another auth mode, another test) would send an empty body and the result would depend on test order.
type wireArgFresh func() any

func wireArgValue(override any) any {
	if fresh, ok := override.(wireArgFresh); ok {
		return fresh()
	}
	return override
}

// recordRequests invokes symbol and returns every request it sent. Each
// principal mode is tried in turn, because a service-only or browser-only
// method refuses a user client before it reaches the wire.
func recordRequests(t *testing.T, symbol string) []wireRequest {
	t.Helper()
	for _, mode := range []domain.AuthMode{"", domain.AuthModeService, domain.AuthModeBrowser, domain.AuthModeNone} {
		if sent := recordRequestsAs(t, symbol, mode); len(sent) != 0 {
			return sent
		}
	}
	return nil
}

func recordRequestsAs(t *testing.T, symbol string, mode domain.AuthMode) []wireRequest {
	t.Helper()
	var mu sync.Mutex
	var sent []wireRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sent = append(sent, wireRequest{r.Method, r.URL.EscapedPath()})
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	// A transport of its own: closing this server would otherwise close idle connections on the shared
	// http.DefaultTransport and fail the other parallel subtests' in-flight requests.
	cfg := Config{BaseURL: server.URL, Username: "user", Password: "pass", NetworkID: "net1", AuthMode: mode, HTTPClient: privateHTTPClient()}
	switch mode {
	case domain.AuthModeBrowser:
		cfg.Username, cfg.Password = "", ""
		cfg.Cookies = []*http.Cookie{{Name: "SESSION", Value: "v1"}, {Name: "XSRF-TOKEN", Value: "v1"}}
	case domain.AuthModeNone:
		cfg.Username, cfg.Password = "", ""
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("%s: NewClient(%q) error = %v", symbol, mode, err)
	}
	serviceName, methodName, _ := strings.Cut(symbol, ".")
	service := reflect.ValueOf(client).Elem().FieldByName(serviceName)
	if !service.IsValid() {
		t.Fatalf("%s: no Client field %s", symbol, serviceName)
	}
	method := service.MethodByName(methodName)
	if !method.IsValid() {
		t.Fatalf("%s: no method %s on %s", symbol, methodName, service.Type())
	}

	// Pollers and waits would otherwise run until their own deadlines.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	args := make([]reflect.Value, method.Type().NumIn())
	for i := range args {
		in := method.Type().In(i)
		switch override, ok := wireArgOverrides[symbol][i]; {
		case ok:
			args[i] = reflect.ValueOf(wireArgValue(override)).Convert(in)
		case in == reflect.TypeOf((*context.Context)(nil)).Elem():
			args[i] = reflect.ValueOf(ctx)
		default:
			args[i] = synthesize(in, 0)
		}
	}
	func() {
		defer func() { _ = recover() }() // a synthesised argument may trip a nil path; what was sent still counts
		if method.Type().IsVariadic() {
			method.CallSlice(args) // the synthesiser built the variadic argument as a slice
		} else {
			method.Call(args)
		}
	}()

	mu.Lock()
	defer mu.Unlock()
	return append([]wireRequest(nil), sent...)
}

// synthesize builds a non-zero value of type t, so argument validation lets
// as many methods as possible reach the wire.
func synthesize(t reflect.Type, depth int) reflect.Value {
	value := reflect.New(t).Elem()
	if depth > 4 {
		return value
	}
	switch t {
	case reflect.TypeOf(time.Duration(0)):
		// Zero means "use the default" for every Duration option in the SDK (timeouts, poll intervals). A
		// non-zero value is read as a real limit: 10ms once made Upload time out before the recording server
		// answered under -race load, and the 300ms context in recordRequestsAs bounds any wait that remains.
		return reflect.Zero(t)
	case reflect.TypeOf(time.Time{}):
		return reflect.ValueOf(time.Unix(1790000000, 0))
	}
	switch t.Kind() {
	case reflect.String:
		value.SetString("v1")
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(1)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(1)
	case reflect.Pointer:
		value.Set(reflect.New(t.Elem()))
		value.Elem().Set(synthesize(t.Elem(), depth+1))
	case reflect.Slice:
		value.Set(reflect.MakeSlice(t, 1, 1))
		value.Index(0).Set(synthesize(t.Elem(), depth+1))
	case reflect.Map:
		value.Set(reflect.MakeMap(t))
		value.SetMapIndex(synthesize(t.Key(), depth+1), synthesize(t.Elem(), depth+1))
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() {
				value.Field(i).Set(synthesize(t.Field(i).Type, depth+1))
			}
		}
	case reflect.Interface:
		switch {
		case reflect.TypeOf(&bytes.Buffer{}).Implements(t):
			value.Set(reflect.ValueOf(bytes.NewBufferString("v1")))
		case reflect.TypeOf(map[string]any{}).Implements(t):
			value.Set(reflect.ValueOf(map[string]any{"v1": "v1"}))
		}
	}
	return value
}

// routeDeclared reports whether method+path matches one of the declared
// operations, where each {param} in a route matches exactly one non-empty
// path segment.
func routeDeclared(declared []CoverageOperation, method, path string) bool {
	for _, operation := range declared {
		if operation.Method == method && routeMatches(operation.Route, path) {
			return true
		}
	}
	return false
}

func routeMatches(route, path string) bool {
	want := strings.Split(strings.Trim(route, "/"), "/")
	got := strings.Split(strings.Trim(path, "/"), "/")
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if strings.HasPrefix(want[i], "{") && strings.HasSuffix(want[i], "}") {
			if got[i] == "" {
				return false
			}
			continue
		}
		if want[i] != got[i] {
			return false
		}
	}
	return true
}
