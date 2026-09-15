// Command fwdctl is the change loop as single commands, for scripts and for
// checking a Forward instance by hand. It exists so nothing that uses it --
// a demo rig, a runbook -- ever reaches for curl against Forward: every
// subcommand is one forward-go-sdk call, printed.
//
// Credentials come from FWD_USER / FWD_PASS; the URL from --url or FWD_HOST.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

// globalFlags are the root persistent flags, resolved once in PersistentPreRunE and handed
// to every subcommand through the command's context -- so a subcommand never reads an
// environment variable or a flag pointer directly, and there is exactly one place that
// decides what "the Forward instance" and "the network" mean for this invocation.
type globalFlags struct {
	baseURL  string
	network  string
	insecure bool
}

type ctxKey int

const clientKey ctxKey = iota

func client(cmd *cobra.Command) *forward.Client {
	return cmd.Context().Value(clientKey).(*forward.Client)
}

func network(cmd *cobra.Command) string {
	return cmd.Root().PersistentFlags().Lookup("network").Value.String()
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "fwdctl:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	flags := &globalFlags{}
	root := &cobra.Command{
		Use:           "fwdctl",
		Short:         "Drive a Forward instance one SDK call at a time",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			c, err := forward.NewClient(forward.Config{
				BaseURL: flags.baseURL, Username: os.Getenv("FWD_USER"), Password: os.Getenv("FWD_PASS"),
				NetworkID: flags.network, InsecureSkipVerify: flags.insecure, UserAgent: "fwdctl",
			})
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Minute)
			cmd.SetContext(context.WithValue(ctx, clientKey, c))
			// cancel is intentionally leaked to the process exit: cobra's Execute has no hook to
			// run it after RunE returns, and the timeout itself bounds the leak.
			_ = cancel
			return nil
		},
	}
	root.PersistentFlags().StringVar(&flags.baseURL, "url", envOr("FWD_HOST", "https://localhost:8443"),
		"Forward instance (env FWD_HOST)")
	root.PersistentFlags().StringVar(&flags.network, "network", os.Getenv("FWD_NETWORK"), "network id (env FWD_NETWORK)")
	root.PersistentFlags().BoolVar(&flags.insecure, "insecure", true, "skip TLS verification for a self-signed dev Forward")

	root.AddCommand(
		newCollectorsCmd(), newCollectorCmd(), newDevicesCmd(), newLocationsCmd(), newCloudAccountsCmd(),
		newSnapshotCmd(), newChangeSetCmd(), newRouteCmd(), newRuleCmd(), newPlanCmd(), newDiffCmd(),
		newCommitCmd(), newRunCmd(), newChecksCmd(), newPathsCmd(),
	)
	return root
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func dump(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
