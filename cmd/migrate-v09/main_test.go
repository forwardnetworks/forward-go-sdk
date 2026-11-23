package main

import (
	"strings"
	"testing"
)

// A consumer that names only moved types loses its root import, gains the layer imports, and keeps building: an unused root
// import would fail its build, which is the thing the tool exists to prevent.
func TestRootImportGoesWhenNothingStaysAndAGroupIsMadeForASingleImport(t *testing.T) {
	src := "package c\n\nimport forward \"github.com/forwardnetworks/forward-go-sdk\"\n\nvar s forward.Snapshot\nvar e *forward.ErrorResponse\n"
	out, problems, err := migrateFile("c.go", []byte(src))
	if err != nil || len(problems) != 0 {
		t.Fatalf("migrateFile: %v %v", err, problems)
	}
	got := string(out)
	for _, want := range []string{"var s domain.Snapshot", "var e *ports.ErrorResponse", `"github.com/forwardnetworks/forward-go-sdk/domain"`,
		`"github.com/forwardnetworks/forward-go-sdk/ports"`, "import (\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"github.com/forwardnetworks/forward-go-sdk"`+"\n") {
		t.Errorf("the root import stayed although nothing uses it:\n%s", got)
	}
}

// Names the root still declares stay as they are, and keep the root import; the import's own name is honoured.
func TestRootNamesStayAndARenamedImportIsFollowed(t *testing.T) {
	src := "package c\n\nimport (\n\tsdk \"github.com/forwardnetworks/forward-go-sdk\"\n)\n\nfunc f() (*sdk.Client, error) { return sdk.NewClient(sdk.Config{}) }\n\nvar n *sdk.NetworksService\n"
	out, problems, err := migrateFile("c.go", []byte(src))
	if err != nil || len(problems) != 0 {
		t.Fatalf("migrateFile: %v %v", err, problems)
	}
	got := string(out)
	for _, want := range []string{"sdk.NewClient(sdk.Config{})", "*sdk.Client", "*usecases.Networks", `sdk "github.com/forwardnetworks/forward-go-sdk"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A local variable that shadows the import's name is the consumer's own, never the root package: rewriting it would change code
// that does not touch the SDK.
func TestALocalShadowingTheImportIsLeftAlone(t *testing.T) {
	src := "package c\n\nimport forward \"github.com/forwardnetworks/forward-go-sdk\"\n\nvar _ forward.Snapshot\n\nfunc g() int {\n\tforward := struct{ Snapshot int }{}\n\treturn forward.Snapshot\n}\n"
	out, _, err := migrateFile("c.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "return forward.Snapshot") {
		t.Errorf("the local variable's field was rewritten:\n%s", out)
	}
}

// A name the map does not know is reported with its position and left unchanged; nothing else in the file is held back.
func TestAnUnknownNameIsReportedWithItsPosition(t *testing.T) {
	src := "package c\n\nimport forward \"github.com/forwardnetworks/forward-go-sdk\"\n\nvar _ forward.Snapshot\nvar _ = forward.NotAName\n"
	out, problems, err := migrateFile("c.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "c.go:6:") || !strings.Contains(problems[0], "NotAName") {
		t.Errorf("problems = %q", problems)
	}
	if !strings.Contains(string(out), "forward.NotAName") || !strings.Contains(string(out), "domain.Snapshot") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// perfpayload moved under domain/ at the split; a consumer's import of it must follow, or the consumer stops building.
func TestAMovedPackagesImportPathFollowsIt(t *testing.T) {
	src := "package c\n\nimport \"github.com/forwardnetworks/forward-go-sdk/perfpayload\"\n\nvar _ perfpayload.Payload\n"
	out, problems, err := migrateFile("c.go", []byte(src))
	if err != nil || len(problems) != 0 {
		t.Fatalf("migrateFile: %v %v", err, problems)
	}
	if !strings.Contains(string(out), `"github.com/forwardnetworks/forward-go-sdk/domain/perfpayload"`) || !strings.Contains(string(out), "perfpayload.Payload") {
		t.Errorf("the import did not follow the package:\n%s", out)
	}
}

// A consumer with a package of its own called domain must keep it: the SDK's domain comes in under another name, and every
// rewritten name says which it means, or the consumer's types and the SDK's are confused (Skyforge's changedemo/domain).
func TestAConsumersOwnDomainPackageIsNotTakenOver(t *testing.T) {
	src := "package c\n\nimport (\n\t\"example.com/app/domain\"\n\n\tforward \"github.com/forwardnetworks/forward-go-sdk\"\n)\n\nvar _ domain.Thing\nvar _ forward.Snapshot\n"
	out, problems, err := migrateFile("c.go", []byte(src))
	if err != nil || len(problems) != 0 {
		t.Fatalf("migrateFile: %v %v", err, problems)
	}
	got := string(out)
	for _, want := range []string{"var _ domain.Thing", "var _ fwddomain.Snapshot", `fwddomain "github.com/forwardnetworks/forward-go-sdk/domain"`, `"example.com/app/domain"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
