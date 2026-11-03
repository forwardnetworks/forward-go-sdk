package forward

import (
	"encoding/json"
	"testing"
)

// The same query written two ways (key order, "value" against "values", value order, upper-case header names) must come out as the same
// bytes: Postgres jsonb reorders keys, and a comparison of stored against built definitions must not see a difference that is not there.
func TestIntentQueryCanonicalForm(t *testing.T) {
	t.Parallel()
	a := `{"to":{"location":{"values":["10.0.1.0/24","10.0.0.0/24"],"type":"HostFilter"},"headers":[{"values":{"IP_PROTO":["6"],"TP_DST":["443","80"]},"type":"PacketFilter"}]},
	       "from":{"location":{"value":"web-1","type":"HostFilter"}},"flowTypes":["VALID"],"mode":"PERMIT_ALL"}`
	b := `{"mode":"PERMIT_ALL","flowTypes":["VALID"],"from":{"location":{"type":"HostFilter","value":"web-1"}},
	       "to":{"headers":[{"type":"PacketFilter","values":{"tp_dst":["80","443"],"ip_proto":["6"]}}],"location":{"type":"HostFilter","values":["10.0.0.0/24","10.0.1.0/24"]}}}`
	ca, err := CanonicalIntentQuery(json.RawMessage(a))
	if err != nil {
		t.Fatal(err)
	}
	cb, err := CanonicalIntentQuery(json.RawMessage(b))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"from":{"location":{"type":"HostFilter","value":"web-1"}},"to":{"location":{"type":"HostFilter","values":["10.0.0.0/24","10.0.1.0/24"]},"headers":[{"type":"PacketFilter","values":{"ip_proto":["6"],"tp_dst":["443","80"]}}]},"flowTypes":["VALID"],"mode":"PERMIT_ALL"}`
	if string(ca) != want || string(cb) != string(ca) {
		t.Fatalf("canonical forms differ:\n a=%s\n b=%s\nwant %s", ca, cb, want)
	}
	// Writing the canonical form again changes nothing.
	again, err := CanonicalIntentQuery(ca)
	if err != nil || string(again) != string(ca) {
		t.Fatalf("not idempotent: %s, %v", again, err)
	}
}

// Builders give the same bytes as the stored form, so an EOS static route's Existential check can be built and later compared.
func TestExistentialCheckBuilder(t *testing.T) {
	t.Parallel()
	query := IntentQuery{
		From: &IntentEndpoint{Location: SubnetLocation("10.1.0.0/24")},
		To: &IntentEndpoint{
			Location: HostLocation("192.0.2.10"),
			Headers:  []PathFilter{PacketHeaders(map[string][]string{HeaderIPProto: {"6"}, HeaderTPDst: {"443"}})},
		},
		FlowTypes: []string{"VALID"},
	}
	check, err := NewExistentialCheck("static route to web", query, ReturnPathAny)
	if err != nil {
		t.Fatal(err)
	}
	if check.Name != "static route to web" || check.Definition.CheckType != "Existential" || check.Definition.ReturnPath != ReturnPathAny {
		t.Fatalf("check = %+v", check)
	}
	const want = `{"from":{"location":{"type":"SubnetLocationFilter","value":"10.1.0.0/24"}},"to":{"location":{"type":"HostFilter","value":"192.0.2.10"},"headers":[{"type":"PacketFilter","values":{"ip_proto":["6"],"tp_dst":["443"]}}]},"flowTypes":["VALID"]}`
	if string(check.Definition.Filters) != want {
		t.Fatalf("filters = %s", check.Definition.Filters)
	}
	if body, err := json.Marshal(check); err != nil || !json.Valid(body) {
		t.Fatalf("check does not marshal: %v", err)
	}
	definition, err := IntentCheckDefinition(ChangeSetCheckExists, query, ReturnPathAny)
	if err != nil || definition["checkType"] != "Existential" || definition["returnPath"] != ReturnPathAny || string(definition["filters"].(json.RawMessage)) != want {
		t.Fatalf("persistent definition = %v, %v", definition, err)
	}
	// Reachability takes no 'to' headers, bypass or flowTypes (Forward refuses them when it runs the check).
	if _, err := NewReachabilityCheck("r", query); err == nil {
		t.Error("a Reachability check with 'to' headers and flowTypes was accepted")
	}
	plain := IntentQuery{From: query.From, To: &IntentEndpoint{Location: query.To.Location}}
	if _, err := NewReachabilityCheck("r", plain); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]IntentQuery{
		"bypass":    {From: plain.From, To: plain.To, Bypass: []PathFilter{DeviceLocation("r9")}},
		"flowTypes": {From: plain.From, To: plain.To, FlowTypes: []string{"VALID"}},
	} {
		if _, err := NewReachabilityCheck("r", q); err == nil {
			t.Errorf("a Reachability check with %s was accepted", name)
		}
	}
	if c, err := NewIsolationCheck("i", query); err != nil || c.Definition.CheckType != "Isolation" {
		t.Fatalf("isolation = %+v, %v", c, err)
	}
}

// Mistakes Forward would refuse, or would read as something else, are refused before a request.
func TestIntentQueryRefusals(t *testing.T) {
	t.Parallel()
	ok := &IntentEndpoint{Location: HostLocation("10.0.0.1")}
	for name, q := range map[string]IntentQuery{
		"empty host":       {From: ok, To: &IntentEndpoint{Location: HostLocation()}},
		"unknown type":     {From: ok, To: &IntentEndpoint{Location: PathFilter{Type: "Banana"}}},
		"not of not":       {From: ok, To: &IntentEndpoint{Location: Not(Not(HostLocation("a")))}},
		"empty packet":     {From: ok, To: &IntentEndpoint{Location: HostLocation("a"), Headers: []PathFilter{PacketHeaders(nil)}}},
		"bad transit":      {From: ok, To: ok, Chain: []IntentHop{{Transit: "around", Location: DeviceLocation("r1")}}},
		"host in chain":    {From: ok, To: ok, Chain: []IntentHop{{Transit: "through", Location: HostLocation("a")}}},
		"subnet in chain":  {From: ok, To: ok, Chain: []IntentHop{{Transit: "through", Location: Not(SubnetLocation("10.0.0.0/8"))}}},
		"interface bypass": {From: ok, To: ok, Bypass: []PathFilter{InterfaceLocation("r1 eth0")}},
		"packet location":  {From: &IntentEndpoint{Location: PacketHeaders(map[string][]string{HeaderIPProto: {"6"}})}, To: ok},
		"location header":  {From: ok, To: &IntentEndpoint{Location: HostLocation("a"), Headers: []PathFilter{HostLocation("b")}}},
		"neither side":     {},
		"zone no device":   {From: ok, To: &IntentEndpoint{Location: SecurityZoneLocation("", "trust")}},
	} {
		if _, err := NewExistentialCheck("x", q, ""); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := NewExistentialCheck("x", IntentQuery{From: ok, To: ok}, "SOMETIMES"); err == nil {
		t.Error("an unknown return path was accepted")
	}
	if _, err := NewReachabilityCheck("x", IntentQuery{From: ok, To: ok}); err != nil {
		t.Errorf("a plain query was refused: %v", err)
	}
	// A chain, a bypass and a negation make it through in canonical order.
	q := IntentQuery{From: ok, To: ok, Chain: []IntentHop{{Transit: "through", Location: DeviceLocation("fw1", "fw2")}}, Bypass: []PathFilter{DeviceLocation("r9")}}
	raw, err := q.CanonicalJSON()
	if err != nil || !json.Valid(raw) {
		t.Fatalf("chain query = %s, %v", raw, err)
	}
	if _, err := ParseIntentQuery(raw); err != nil {
		t.Fatalf("canonical form does not parse back: %v", err)
	}
}

// Pin the bytes Forward would see for each filter kind, so a change to the canonical form is a visible diff, not a silent one.
func TestIntentQueryPinnedForms(t *testing.T) {
	t.Parallel()
	hop := func(f PathFilter) IntentQuery {
		return IntentQuery{Chain: []IntentHop{{Transit: "THROUGH", Location: f}}}
	}
	for name, c := range map[string]struct {
		query IntentQuery
		want  string
	}{
		"subnet":      {IntentQuery{From: &IntentEndpoint{Location: SubnetLocation("10.0.0.1/32")}}, `{"from":{"location":{"type":"SubnetLocationFilter","value":"10.0.0.1"}}}`},
		"host values": {IntentQuery{From: &IntentEndpoint{Location: HostLocation("b", "a", "a")}}, `{"from":{"location":{"type":"HostFilter","values":["a","b"]}}}`},
		"logical":     {IntentQuery{To: &IntentEndpoint{LogicalNetwork: "ln1", Location: DeviceLocation("r1")}}, `{"to":{"logicalNetwork":"ln1","location":{"type":"DeviceFilter","value":"r1"}}}`},
		"not":         {IntentQuery{To: &IntentEndpoint{Location: Not(HostLocation("h1"))}}, `{"to":{"location":{"type":"NotFilter","clause":{"type":"HostFilter","value":"h1"}}}}`},
		"zone":        {hop(SecurityZoneLocation("fw1", "trust")), `{"chain":[{"transitType":"through","location":{"type":"SecurityZoneFilter","device":"fw1","value":"trust"}}]}`},
		"device vrf":  {hop(DeviceVRFLocation("r1", "blue")), `{"chain":[{"transitType":"through","location":{"type":"DeviceVrfFilter","device":"r1","vrf":"blue"}}]}`},
		"packet alias": {IntentQuery{To: &IntentEndpoint{Location: HostLocation("h"), Headers: []PathFilter{{Type: HeaderPacketAlias, Value: "web", Direction: "DST"}}}},
			`{"to":{"location":{"type":"HostFilter","value":"h"},"headers":[{"type":"PacketAliasFilter","value":"web","direction":"dst"}]}}`},
		"bypass": {IntentQuery{Bypass: []PathFilter{DeviceLocation("r9", "r8")}, ForwardingTypes: []string{"L3", "L2"}}, `{"bypass":[{"type":"DeviceFilter","values":["r8","r9"]}],"forwardingTypes":["L2","L3"]}`},
	} {
		got, err := c.query.CanonicalJSON()
		if err != nil || string(got) != c.want {
			t.Errorf("%s = %s, %v; want %s", name, got, err, c.want)
		}
	}
	// A key this SDK does not model is an error: the hidden deviceHops, or a typo, must not canonicalize to a different, equal-looking query.
	for name, raw := range map[string]string{
		"deviceHops": `{"from":{"location":{"type":"HostFilter","value":"a"}},"deviceHops":{"min":1}}`,
		"typo":       `{"from":{"location":{"type":"HostFilter","value":"a"},"logicalNetwrk":"x"}}`,
		"filter key": `{"from":{"location":{"type":"HostFilter","value":"a","extra":1}}}`,
	} {
		if _, err := CanonicalIntentQuery(json.RawMessage(raw)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// What Forward echoes ("value", one address) and what the builder writes are the same bytes.
	stored := `{"to":{"location":{"value":"10.0.0.1/32","type":"SubnetLocationFilter"},"logicalNetwork":"ln1"}}`
	canon, err := CanonicalIntentQuery(json.RawMessage(stored))
	if err != nil || string(canon) != `{"to":{"logicalNetwork":"ln1","location":{"type":"SubnetLocationFilter","value":"10.0.0.1"}}}` {
		t.Fatalf("stored form = %s, %v", canon, err)
	}
}
