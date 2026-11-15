package forward

import (
	"encoding/json"
	"testing"
)

// Rows decode by JSON tag with integers kept exact above 2^53; enum text "AdminStatus.UP" is rewritten only when asked, including in
// lists, and a row that does not fit the type fails naming its index.
func TestDecodeNQE(t *testing.T) {
	type row struct {
		Device string   `json:"Device"`
		Status string   `json:"Status"`
		Modes  []string `json:"Modes"`
		Bytes  int64    `json:"Bytes"`
	}
	var rec NQERecord
	if err := json.Unmarshal([]byte(`{"Device":"core.SW1","Status":"AdminStatus.UP","Modes":["Mode.ACCESS","Mode.TRUNK"],"Bytes":9007199254740993,"Extra":1}`), &rec); err != nil {
		t.Fatal(err)
	}
	plain, err := DecodeNQE[row]([]NQERecord{rec})
	if err != nil || plain[0].Status != "AdminStatus.UP" || plain[0].Bytes != 9007199254740993 {
		t.Fatalf("plain = %+v err = %v", plain, err)
	}
	norm, err := DecodeNQE[row]([]NQERecord{rec}, NQENormalizeEnums())
	if err != nil || norm[0].Status != "UP" || norm[0].Modes[1] != "TRUNK" || norm[0].Device != "core.SW1" || norm[0].Bytes != 9007199254740993 {
		t.Fatalf("normalised = %+v err = %v", norm, err)
	}
	var bad NQERecord
	_ = json.Unmarshal([]byte(`{"Device":5}`), &bad)
	if _, err := DecodeNQE[row]([]NQERecord{rec, bad}); err == nil {
		t.Fatal("a row that does not fit must be an error")
	}
}
