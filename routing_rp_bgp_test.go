package forward

import (
	"encoding/json"
	"testing"
)

// The attributes decode as Forward's BgpRouteAttributes names them (source-derived): the AS path is a list of sets and flattens to the
// collected shape, and a non-BGP node is refused by name instead of decoding as zeros.
func TestRPPrefixDetailsBGPAttributes(t *testing.T) {
	var details RPPrefixDetails
	body := `{"prefix":"10.0.0.0/24","attributes":{"protocol":"BGP","asPath":[[65001],[65002,65003]],"localPref":100,"med":0,"bgpNextHopAddr":"192.0.2.1","origin":"IGP","standardCommunities":["65000:1"]}}`
	if err := json.Unmarshal([]byte(body), &details); err != nil {
		t.Fatal(err)
	}
	attrs, err := details.BGPAttributes()
	if err != nil || attrs.LocalPref != 100 || attrs.NextHop != "192.0.2.1" || attrs.Origin != "IGP" || len(attrs.StandardCommunities) != 1 {
		t.Fatalf("attrs=%+v err=%v", attrs, err)
	}
	if flat := attrs.FlatASPath(); len(flat) != 3 || flat[0] != 65001 || flat[2] != 65003 {
		t.Fatalf("flat = %v", flat)
	}
	if string(attrs.Raw) == "" {
		t.Fatal("Raw must keep the whole object")
	}
	var ospf RPPrefixDetails
	_ = json.Unmarshal([]byte(`{"attributes":{"protocol":"OSPF","metric":10}}`), &ospf)
	if _, err := ospf.BGPAttributes(); err == nil {
		t.Fatal("an OSPF node must not decode as BGP")
	}
	if _, err := (&RPPrefixDetails{}).BGPAttributes(); err == nil {
		t.Fatal("no attributes must be an error")
	}
}
