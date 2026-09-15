package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func newSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Snapshot operations: latest id by default",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _, err := client(cmd).Snapshots.ResolveID(cmd.Context(), network(cmd), "latest")
			if err != nil {
				return err
			}
			fmt.Println(id)
			return nil
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get <id>",
			Short: "Print a snapshot's metadata",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, _, err := client(cmd).Snapshots.Get(cmd.Context(), network(cmd), args[0])
				if err != nil {
					return err
				}
				return dump(s)
			},
		},
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a snapshot",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				_, err := client(cmd).Snapshots.Delete(cmd.Context(), args[0])
				return err
			},
		},
		&cobra.Command{
			Use:   "reprocess <id>",
			Short: "Invalidate and reprocess a snapshot, then wait for it to settle",
			Long: "Invalidate and reprocess a snapshot, then wait: the way to realign a base\n" +
				"snapshot's job versions after a build change.",
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx, c, net := cmd.Context(), client(cmd), network(cmd)
				if _, _, err := c.Snapshots.Reprocess(ctx, args[0]); err != nil {
					return err
				}
				for {
					s, _, err := c.Snapshots.Get(ctx, net, args[0])
					if err != nil {
						return err
					}
					fmt.Fprintln(os.Stderr, "snapshot", args[0], s.State)
					switch strings.ToUpper(s.State) {
					case "PROCESSED":
						fmt.Println(s.ID)
						return nil
					case "FAILED", "CANCELED", "TIMED_OUT":
						return fmt.Errorf("snapshot %s ended %s", args[0], s.State)
					}
					time.Sleep(15 * time.Second)
				}
			},
		},
		newSnapshotDownloadCmd(),
		&cobra.Command{
			Use:   "collect",
			Short: "Start a collection and wait for it; prints the resulting snapshot id",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx, c, net := cmd.Context(), client(cmd), network(cmd)
				op, _, err := c.CollectorTasks.StartCollectionOperation(ctx, net, forward.CollectionStartOptions{})
				if err != nil {
					return err
				}
				s, err := op.Wait(ctx, forward.CollectionWaitOptions{PollInterval: 10 * time.Second})
				if err != nil {
					return err
				}
				fmt.Println(s.ID)
				return nil
			},
		},
	)
	return cmd
}

func newSnapshotDownloadCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download a snapshot as a zip",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Create(out)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = client(cmd).Snapshots.Download(cmd.Context(), args[0], f)
			return err
		},
	}
	cmd.Flags().StringVar(&out, "out", "snapshot.zip", "output file")
	return cmd
}
