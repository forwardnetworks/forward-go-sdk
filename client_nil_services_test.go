package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// updateNilServices records how methods added since the hexagonal split (the v0.9 hexagonal split) answer a nil service, in
// testdata/nil_services_added.txt. testdata/nil_services.txt was recorded on the unmoved tree and is never rewritten.
var updateNilServices = flag.Bool("update-nil-services", false, "record methods added since the split in testdata/nil_services_added.txt")

// updateNilApprovals rewrites testdata/nil_services_approved.txt from the differences the tree shows. It approves only the one
// kind of difference Craig ruled acceptable (2026-11-23): a method that answered a nil service without a nil-service error of
// its own (its argument check ran first) now panics. Any other difference fails the run and writes nothing.
var updateNilApprovals = flag.Bool("update-nil-approvals", false, "rewrite testdata/nil_services_approved.txt")

const nilServicesPath = "testdata/nil_services.txt"

// A zero Client's families are nil, and some of their methods have always answered a nil service with an error ("forward: admin
// service is nil") or a zero value instead of panicking. The split puts a use-case struct in front of every port, which on its
// own would panic dereferencing the nil struct; the step-zero amendment (item 4) keeps every nil-safe method nil-safe. This is
// the differential that holds it: every method of every Client field, called on a nil receiver, must end exactly as it did on
// the reference tree — the same panic or not, the same nil-service error text, the same zero or non-zero results.
func TestNilServicesAnswerAsTheyAlwaysHave(t *testing.T) {
	rendered := renderNilServices()
	if n := strings.Count(rendered, "\n"); n < 795 {
		t.Fatalf("only %d methods rendered; the check proves little", n)
	}
	safe := strings.Count(rendered, " returns ")
	t.Logf("%d methods answer a nil service without panicking", safe)
	if *updateNilServices {
		// Only methods added since the split are recorded, in the companion file. A recorded method's answer is judged by the
		// normal run against the approved differences, never rewritten here.
		writeAddedLines(t, nilServicesPath, rendered, func(string, string) bool { return true })
		return
	}
	want := readLineReference(t, nilServicesPath)
	if *updateNilApprovals {
		writeNilApprovals(t, want, rendered)
		return
	}
	want = applyApprovedNilPanics(t, want, rendered)
	if string(want) != rendered {
		wantLines, gotLines := lineSet(string(want)), lineSet(rendered)
		var diffs []string
		for line := range wantLines {
			if !gotLines[line] {
				diffs = append(diffs, "- "+line)
			}
		}
		for line := range gotLines {
			if !wantLines[line] {
				diffs = append(diffs, "+ "+line)
			}
		}
		sort.Strings(diffs)
		t.Errorf("%d lines differ from %s:\n%s", len(diffs), nilServicesPath, strings.Join(diffs, "\n"))
	}
}

func renderNilServices() string {
	ct := reflect.TypeOf(Client{})
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	var lines []string
	for i := 0; i < ct.NumField(); i++ {
		field := ct.Field(i)
		if !field.IsExported() || field.Type.Kind() != reflect.Pointer {
			continue
		}
		nilService := reflect.Zero(field.Type)
		for j := 0; j < field.Type.NumMethod(); j++ {
			m := field.Type.Method(j)
			mt := m.Type
			args := []reflect.Value{nilService}
			for k := 1; k < mt.NumIn(); k++ {
				switch in := mt.In(k); {
				case in == ctxType:
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					args = append(args, reflect.ValueOf(ctx))
				default:
					args = append(args, synthesize(in, 0))
				}
			}
			lines = append(lines, field.Name+"."+m.Name+" "+callNil(m.Func, args, mt.IsVariadic()))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// callNil calls fn and renders how it ended: "panics", or "returns" with each result as its error text, "zero" or "set".
func callNil(fn reflect.Value, args []reflect.Value, variadic bool) (out string) {
	defer func() {
		if recover() != nil {
			out = "panics"
		}
	}()
	var results []reflect.Value
	if variadic {
		results = fn.CallSlice(args)
	} else {
		results = fn.Call(args)
	}
	errType := reflect.TypeOf((*error)(nil)).Elem()
	parts := make([]string, 0, len(results))
	for _, r := range results {
		switch {
		case r.Type() == errType && !r.IsNil():
			err := r.Interface().(error)
			var unavailable *domain.UnavailableError
			if errors.As(err, &unavailable) {
				parts = append(parts, "unavailable")
				continue
			}
			// A nil service's own error is recorded word for word; any other (an argument check that ran before the receiver
			// was reached) only as an error, since some of those name the first of several problems in map order.
			if strings.Contains(err.Error(), "service is nil") {
				parts = append(parts, fmt.Sprintf("error %q", err.Error()))
			} else {
				parts = append(parts, "error")
			}
		case r.IsZero():
			parts = append(parts, "zero")
		default:
			parts = append(parts, "set")
		}
	}
	return "returns " + strings.Join(parts, ", ")
}

const nilServicesApprovedPath = "testdata/nil_services_approved.txt"

// applyApprovedNilPanics applies the approved differences: each line of testdata/nil_services_approved.txt names a method,
// then why it may now panic on a nil service where the reference recorded an answer. An approved method must panic now, and
// must have answered in the reference; either way round is an error, so the list can neither hide a change nor go stale.
func applyApprovedNilPanics(t *testing.T, reference []byte, rendered string) []byte {
	t.Helper()
	data, err := os.ReadFile(nilServicesApprovedPath)
	if os.IsNotExist(err) {
		return reference
	}
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(reference), "\n"), "\n")
	index := map[string]int{}
	for i, line := range lines {
		name, _, _ := strings.Cut(line, " ")
		index[name] = i
	}
	now := lineSet(rendered)
	for _, entry := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		name, why, _ := strings.Cut(entry, " ")
		if strings.TrimSpace(why) == "" {
			t.Errorf("%s: %s gives no reason", nilServicesApprovedPath, name)
		}
		i, ok := index[name]
		switch {
		case !ok:
			t.Errorf("%s names %s, which is not a method", nilServicesApprovedPath, name)
		case strings.HasSuffix(lines[i], " panics"):
			t.Errorf("%s: %s already panicked in the reference; take it off the list", nilServicesApprovedPath, name)
		case strings.Contains(lines[i], "service is nil") || !strings.Contains(lines[i], " error"):
			t.Errorf("%s: %s answered a nil service with a deliberate check or no error at all, which may never be approved away", nilServicesApprovedPath, name)
		case !now[name+" panics"]:
			t.Errorf("%s: %s no longer panics; take it off the list", nilServicesApprovedPath, name)
		default:
			lines[i] = name + " panics"
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func writeNilApprovals(t *testing.T, reference []byte, rendered string) {
	t.Helper()
	now := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
		name, outcome, _ := strings.Cut(line, " ")
		now[name] = outcome
	}
	var approved []string
	for _, line := range strings.Split(strings.TrimSuffix(string(reference), "\n"), "\n") {
		name, was, _ := strings.Cut(line, " ")
		is, ok := now[name]
		switch {
		case !ok:
			t.Errorf("%s is gone", name)
		case is == was:
		case is == "panics" && strings.Contains(was, " error") && !strings.Contains(was, "service is nil"):
			approved = append(approved, name+" answered a nil service only through an argument check run before the receiver; it now panics first")
		default:
			t.Errorf("%s: %q became %q, which is not an approvable difference", name, was, is)
		}
	}
	if t.Failed() {
		return
	}
	head := "# Methods that may now panic on a nil service (the v0.9 hexagonal split), written by\n# go test -run TestNilServicesAnswerAsTheyAlwaysHave -update-nil-approvals from the differences the tree shows.\n"
	if err := os.WriteFile(nilServicesApprovedPath, []byte(head+strings.Join(approved, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("approved %d methods", len(approved))
}
