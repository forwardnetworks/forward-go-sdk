package forward

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func decodeAlias(t *testing.T, text string) Alias {
	t.Helper()
	var alias Alias
	if err := json.Unmarshal([]byte(text), &alias); err != nil {
		t.Fatalf("decode %s: %v", text, err)
	}
	return alias
}

// Each type is read as Forward serializes it (lower-case header keys, an unwrapped vlans object, isExposurePoint
// only when true, resolvedValue only on Get), converted, and written back as the exact body a Put would send.
func TestAliasBuilderFromDefinitions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, read, want string
	}{
		{"hosts", `{"name":"web","type":"HOSTS","createdAt":"2026-10-01T09:00:00Z","creatorId":"12","values":["10.30.1.0/24","10.1.0.0/24"],"locations":["tor_switches"]}`,
			`{"locations":["tor_switches"],"name":"web","type":"HOSTS","values":["10.1.0.0/24","10.30.1.0/24"]}`},
		{"devices from Get", `{"name":"border","type":"DEVICES","createdAt":"x","creatorId":"1","values":["bbr?_rtr","a"],"resolvedValue":{"devices":["bbra_rtr"]}}`,
			`{"name":"border","type":"DEVICES","values":["a","bbr?_rtr"]}`},
		{"interfaces", `{"name":"v","type":"INTERFACES","creatorId":"1","vlanIds":["20-29"],"vlanIntfTypes":["TRUNK","ACCESS"],"isExposurePoint":true}`,
			`{"isExposurePoint":true,"name":"v","type":"INTERFACES","vlanIds":["20-29"],"vlanIntfTypes":["ACCESS","TRUNK"]}`},
		{"interfaces exposure false is omitted by Forward", `{"name":"i","type":"INTERFACES","values":["d p"]}`,
			`{"name":"i","type":"INTERFACES","values":["d p"]}`},
		{"headers", `{"name":"VOIP","type":"HEADERS","values":{"tp_port":["20000","10000"],"ip_proto":["UDP"]}}`,
			`{"name":"VOIP","type":"HEADERS","values":{"ip_proto":["UDP"],"tp_port":["10000","20000"]}}`},
		{"logical network", `{"name":"enclave","type":"LOGICAL_NETWORK","devices":["d2*","d1"],"edgeNodes":["en1"]}`,
			`{"devices":["d1","d2*"],"edgeNodes":["en1"],"name":"enclave","type":"LOGICAL_NETWORK"}`},
	}
	for _, tc := range cases {
		builder, err := decodeAlias(t, tc.read).Builder(AliasConvertOptions{})
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		got, err := json.Marshal(builder)
		if err != nil || string(got) != tc.want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", tc.name, got, err, tc.want)
		}
	}
}

// The converter must never drop a field it cannot map: it names it, and a caller can opt in to ignoring it.
func TestAliasBuilderRefusesWhatItCannotMap(t *testing.T) {
	t.Parallel()

	alias := decodeAlias(t, `{"name":"web","type":"HOSTS","values":["h"],"futureField":1,"anotherOne":"x"}`)
	_, err := alias.Builder(AliasConvertOptions{})
	if err == nil || !strings.Contains(err.Error(), "anotherOne, futureField") || !strings.Contains(err.Error(), `"web"`) {
		t.Fatalf("unknown fields must be named, sorted: %v", err)
	}
	if _, err := alias.Builder(AliasConvertOptions{IgnoreKeys: []string{"futureField", "anotherOne"}}); err != nil {
		t.Fatalf("explicitly ignored keys must convert: %v", err)
	}
	if _, err := alias.Builder(AliasConvertOptions{IgnoreKeys: []string{"futureField"}}); err == nil {
		t.Fatal("ignoring one key must not hide another")
	}
	for name, text := range map[string]string{
		"unknown type":         `{"name":"a","type":"GROUPS","values":["x"]}`,
		"values not a list":    `{"name":"a","type":"DEVICES","values":"d1"}`,
		"header not an object": `{"name":"a","type":"HEADERS","values":["x"]}`,
		"header unknown key":   `{"name":"a","type":"HEADERS","values":{"ipv4_src":["1.1.1.1"]}}`,
		"hosts with nothing":   `{"name":"a","type":"HOSTS"}`,
		"exposure not bool":    `{"name":"a","type":"INTERFACES","values":["d p"],"isExposurePoint":"yes"}`,
	} {
		if _, err := decodeAlias(t, text).Builder(AliasConvertOptions{}); err == nil {
			t.Errorf("%s must be an error, not a partial conversion", name)
		}
	}
	if _, err := (Alias{Name: "built by hand", Type: AliasTypeDevices}).Builder(AliasConvertOptions{}); err == nil {
		t.Error("an alias without a Definition must be an error")
	}
}

// Shuffled input yields identical canonical output, so a hash over it is reproducible, and the inputs are
// not modified.
func TestCanonicalFormsAreOrderIndependent(t *testing.T) {
	t.Parallel()

	aliases := []Alias{
		decodeAlias(t, `{"name":"zeta","type":"DEVICES","values":["b","a"]}`),
		decodeAlias(t, `{"name":"alpha","type":"HOSTS","values":["h2","h1"],"locations":["l"]}`),
	}
	reversed := []Alias{aliases[1], aliases[0]}
	one, err := CanonicalAliases(aliases, AliasConvertOptions{})
	two, err2 := CanonicalAliases(reversed, AliasConvertOptions{})
	if err != nil || err2 != nil || !reflect.DeepEqual(one, two) || one[0].Name != "alpha" || !reflect.DeepEqual(one[1].Values, []string{"a", "b"}) {
		t.Fatalf("aliases: %+v / %+v (%v, %v)", one, two, err, err2)
	}
	if _, err := CanonicalAliases([]Alias{aliases[0], decodeAlias(t, `{"name":"bad","type":"HOSTS","x":1}`)}, AliasConvertOptions{}); err == nil || !strings.Contains(err.Error(), `"bad"`) {
		t.Fatalf("a list with an unmappable alias must fail naming it: %v", err)
	}

	locations := []Location{{ID: "b", Name: "B", DeviceGlobs: []string{"z-*", "a-*"}}, {ID: "a", Name: "A"}}
	got := CanonicalLocations(locations)
	if got[0].ID != "a" || !reflect.DeepEqual(got[1].DeviceGlobs, []string{"a-*", "z-*"}) || locations[0].ID != "b" || locations[0].DeviceGlobs[0] != "z-*" {
		t.Fatalf("locations = %+v (input must be untouched: %+v)", got, locations)
	}

	circuits := []WanCircuit{{Name: "b", Source: "API"}, {Name: "a", Source: "UI", Connection1: WanCircuitConnection{Device: "x", Port: "1"}}}
	cc := CanonicalWanCircuits(circuits)
	if cc[0].Name != "a" || cc[0].Source != "" || cc[1].Source != "" || circuits[0].Source != "API" {
		t.Fatalf("circuits = %+v (input must be untouched: %+v)", cc, circuits)
	}

	tags := []DeviceTag{{Name: "Edge", Color: "#00FF00", Devices: []string{"r2", "r1"}}, {Name: "Core", Devices: []string{"csw01"}}}
	ct := CanonicalDeviceTags(tags)
	if ct[0].Name != "Core" || ct[1].Color != "#00ff00" || !reflect.DeepEqual(ct[1].Devices, []string{"r1", "r2"}) || tags[0].Color != "#00FF00" || tags[0].Devices[0] != "r2" {
		t.Fatalf("tags = %+v (input must be untouched: %+v)", ct, tags)
	}
}
