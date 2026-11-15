package forward

import (
	"encoding/json"
	"testing"
)

func nqeRows(t *testing.T, rows ...string) []NQERecord {
	t.Helper()
	out := make([]NQERecord, 0, len(rows))
	for _, row := range rows {
		var rec NQERecord
		if err := json.Unmarshal([]byte(row), &rec); err != nil {
			t.Fatal(err)
		}
		out = append(out, rec)
	}
	return out
}

// Rows pair on the key, not on position; column order and number formatting inside a row must not read as a change; a repeated
// key must fail rather than pair the wrong rows.
func TestDiffNQERows(t *testing.T) {
	before := nqeRows(t, `{"Device":"a","State":"UP","N":1}`, `{"Device":"b","State":"UP","N":2}`, `{"Device":"c","State":"UP","N":3}`)
	after := nqeRows(t, `{"N":3.0,"State":"UP","Device":"c"}`, `{"Device":"b","State":"DOWN","N":2}`, `{"Device":"d","State":"UP","N":4}`)
	diff, err := DiffNQERows(before, after, "Device")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range diff {
		var key string
		switch {
		case e.After != nil:
			key = string((*e.After)["Device"])
		default:
			key = string((*e.Before)["Device"])
		}
		got[key] = e.Type
	}
	want := map[string]string{`"a"`: "DELETED", `"b"`: "MODIFIED", `"d"`: "ADDED"}
	if len(got) != 3 || got[`"a"`] != want[`"a"`] || got[`"b"`] != want[`"b"`] || got[`"d"`] != want[`"d"`] {
		t.Fatalf("diff = %v (c is the same row and must not appear)", got)
	}
	if _, err := DiffNQERows(nqeRows(t, `{"Device":"a"}`, `{"Device":"a"}`), nil, "Device"); err == nil {
		t.Fatal("a repeated key must be an error")
	}
	if _, err := DiffNQERows(nqeRows(t, `{"x":1}`), nil, "Device"); err == nil {
		t.Fatal("a row without the key column must be an error")
	}
	if _, err := DiffNQERows(nil, nil); err == nil {
		t.Fatal("no key column must be an error")
	}
}
