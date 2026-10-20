// Command rbacdump reads Forward's operation table and network role ladder from a local Forward source checkout and writes them as JSON,
// for forward.LoadAccessTableJSON (or FORWARD_RBAC_TABLE) on a machine that has no checkout.
//
// The output is Forward's per-operation permission matrix. Keep it out of public repositories: this module commits only the parser.
//
//	go run ./cmd/rbacdump -fwd ~/src/fwd -out ~/forward-rbac-table.json
package main

import (
	"flag"
	"fmt"
	"os"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func main() {
	fwd := flag.String("fwd", os.Getenv("FORWARD_SOURCE"), "Forward source checkout (default $FORWARD_SOURCE)")
	out := flag.String("out", "", "file to write (default stdout)")
	flag.Parse()
	if *fwd == "" {
		fmt.Fprintln(os.Stderr, "rbacdump: -fwd or FORWARD_SOURCE is required")
		os.Exit(2)
	}
	table, err := forward.LoadAccessTable(*fwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rbacdump:", err)
		os.Exit(1)
	}
	w := os.Stdout
	if *out != "" {
		file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "rbacdump:", err)
			os.Exit(1)
		}
		// An existing file keeps its old permissions on open; this is a permission matrix, so tighten them.
		if err := file.Chmod(0o600); err != nil {
			fmt.Fprintln(os.Stderr, "rbacdump:", err)
			os.Exit(1)
		}
		w = file
	}
	if err := table.WriteJSON(w); err != nil {
		fmt.Fprintln(os.Stderr, "rbacdump:", err)
		os.Exit(1)
	}
	if w != os.Stdout {
		if err := w.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "rbacdump:", err)
			os.Exit(1)
		}
	}
}
