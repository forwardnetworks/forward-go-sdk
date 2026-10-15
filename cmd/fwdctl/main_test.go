package main

import (
	"bytes"
	"strings"
	"testing"
)

// Certificate checks may be skipped only for a Forward on this machine; any other host must be verified unless the
// caller says --insecure.
func TestIsLoopbackURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://localhost:8443": true, "https://127.0.0.1": true, "https://[::1]:8443": true,
		"https://fwd.app": false, "https://localhost.example.com": false, "::bad": false,
	} {
		if got := isLoopbackURL(raw); got != want {
			t.Errorf("isLoopbackURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

// The help topics carry what a method name does not say, so they must exist,
// be listed where `fwdctl help` finds them, and keep the facts that were
// expensive to learn (a PutBatch replaces, a patch replaces the whole
// disabled-commands map, an empty CAP filter fails collection, GC time is
// cumulative). A rewrite that drops one fails here.
func TestHelpTopicsCarryTheCaveats(t *testing.T) {
	// Help needs no Forward: with no credentials set it must still print.
	t.Setenv("FWD_USER", "")
	t.Setenv("FWD_PASS", "")
	root := newRootCmd()
	for name, mustSay := range map[string][]string{
		"tls":         {"--ca-bundle", "REPLACES the system roots"},
		"writes":      {"PutBatch REPLACES", "PatchMany", "Clear", "REPLACES the whole stored map", "NOT retried"},
		"reads":       {"NQE.RunAll", "EMPTY awsC2SAccountInfos does NOT", "CUMULATIVE", "no documented row order"},
		"permissions": {"DEBUG_NQE", "DEBUG_COLLECTORS", "MissingPermission"},
	} {
		topic, _, err := root.Find([]string{name})
		if err != nil || topic == nil || topic.Name() != name {
			t.Fatalf("no help topic %q: %v", name, err)
		}
		if topic.Runnable() {
			t.Errorf("topic %q must be help-only, not a command", name)
		}
		for _, phrase := range mustSay {
			if !strings.Contains(topic.Long, phrase) {
				t.Errorf("topic %q lost %q", name, phrase)
			}
		}
	}
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Additional help topics") || !strings.Contains(out.String(), "permissions") {
		t.Errorf("fwdctl help must list the topics:\n%s", out.String())
	}
}

func TestHelpPrintsATopicAndCompletionWorkWithoutCredentials(t *testing.T) {
	t.Setenv("FWD_USER", "")
	t.Setenv("FWD_PASS", "")
	for _, args := range [][]string{{"help", "tls"}, {"__complete", ""}, {"completion", "bash"}} {
		root := newRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v needs no credentials: %v", args, err)
		}
		if args[0] == "help" && !strings.Contains(out.String(), "REPLACES the system roots") {
			t.Errorf("help tls must print the topic:\n%s", out.String())
		}
	}
}
