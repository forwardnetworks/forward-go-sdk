package forward

import (
	"reflect"
	"testing"
)

// A ForNetwork clone's services must reach the clone, never the parent: a service that still points at the parent's client
// (or, after the hexagonal split of the v0.9 hexagonal split, a use case that still holds the parent's *usecases.Y for a family it
// composes) resolves an empty network against the parent. TestForNetworkCloneNeverSendsTheParentsNetwork sees that only for
// symbols that send; this walks every pointer a service holds, so it also sees a composite whose own call never reached the
// wire. It reads the structs by type only: a pointer field whose type is *Client, or the type of a Client field, must be the
// clone's own.
func TestForNetworkCloneServicesPointAtTheClonesOwnSiblings(t *testing.T) {
	parent, err := NewClient(Config{BaseURL: "https://forward.example", Username: "u", Password: "p", NetworkID: "parentnet",
		HTTPClient: privateHTTPClient()})
	if err != nil {
		t.Fatal(err)
	}
	clone := parent.ForNetwork("boundnet")
	if clone == parent {
		t.Fatal("ForNetwork returned the parent; the check proves nothing")
	}

	pv, cv := reflect.ValueOf(parent).Elem(), reflect.ValueOf(clone).Elem()
	clientType := reflect.TypeOf(parent)
	families := map[reflect.Type]string{} // a Client field's type -> its field name
	for i := 0; i < cv.NumField(); i++ {
		f := cv.Type().Field(i)
		if f.IsExported() && f.Type.Kind() == reflect.Pointer {
			families[f.Type] = f.Name
		}
	}

	checked := 0
	for i := 0; i < cv.NumField(); i++ {
		field := cv.Type().Field(i)
		if !field.IsExported() || field.Type.Kind() != reflect.Pointer || field.Type.Elem().Kind() != reflect.Struct {
			continue
		}
		svc := cv.Field(i)
		if svc.IsNil() {
			t.Errorf("clone.%s is nil", field.Name)
			continue
		}
		if svc.Pointer() == pv.Field(i).Pointer() {
			t.Errorf("clone.%s is the parent's own %s", field.Name, field.Name)
		}
		st, pst := svc.Elem(), pv.Field(i).Elem()
		for j := 0; j < st.NumField(); j++ {
			inner, ft := st.Field(j), st.Type().Field(j).Type
			// Whatever a service holds by pointer, directly or behind an interface (a port, the session), is the clone's own:
			// never the very object the parent's service holds in that field.
			if p, ok := heldPointer(inner); ok {
				checked++
				if pp, _ := heldPointer(pst.Field(j)); pp == p {
					t.Errorf("clone.%s.%s holds the parent's own %s", field.Name, st.Type().Field(j).Name, ft)
				}
			}
			switch {
			case ft == clientType:
				if inner.Pointer() != cv.Addr().Pointer() {
					t.Errorf("clone.%s.%s points at a client other than the clone", field.Name, st.Type().Field(j).Name)
				}
			case families[ft] != "":
				sibling := families[ft]
				if inner.Pointer() != cv.FieldByName(sibling).Pointer() {
					t.Errorf("clone.%s.%s is not the clone's own %s", field.Name, st.Type().Field(j).Name, sibling)
				}
			}
		}
	}
	// Before the split every service holds its *Client (68 fields); after it, each use case holds its port and its session, and
	// the composites their families. Raise the floor to what the tree has, never lower it.
	const minChecked = 68
	t.Logf("%d held pointers checked", checked)
	if checked < minChecked {
		t.Errorf("only %d held pointers checked; want at least %d", checked, minChecked)
	}
}

// heldPointer is the address a field holds: a pointer's, or the pointer behind an interface. It reports false for anything else.
func heldPointer(v reflect.Value) (uintptr, bool) {
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return 0, false
	}
	return v.Pointer(), true
}
