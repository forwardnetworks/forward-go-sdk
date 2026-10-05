package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func newChangeSetCmd() *cobra.Command {
	var base string
	cmd := &cobra.Command{
		Use:   "change-set",
		Short: "Create a change set from the latest (or --base) snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, c, net := cmd.Context(), client(cmd), network(cmd)
			if base == "" {
				id, _, err := c.Snapshots.ResolveID(ctx, net, "latest")
				if err != nil {
					return err
				}
				base = string(id)
			}
			cs, _, err := c.Predict.CreateChangeSet(ctx, net, forward.ChangeSetCreateRequest{Name: "fwdctl", SnapshotID: base})
			if err != nil {
				return err
			}
			fmt.Println(cs.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "base snapshot id (default: latest)")
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List change sets",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				list, _, err := client(cmd).Predict.ListChangeSets(cmd.Context(), network(cmd))
				if err != nil {
					return err
				}
				return dump(list)
			},
		},
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a change set",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				_, err := client(cmd).Predict.DeleteChangeSet(cmd.Context(), network(cmd), args[0])
				return err
			},
		},
	)
	return cmd
}

func newCommitCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "commit <change-set>",
		Short: "Commit a change set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := client(cmd).Predict.Commit(cmd.Context(), network(cmd), args[0], note)
			return err
		},
	}
	cmd.Flags().StringVar(&note, "note", "fwdctl", "")
	return cmd
}

func newRunCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "run <change-set>",
		Short: "Predict a change set and wait; prints the predicted snapshot id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, net := client(cmd), network(cmd)
			poller, _, err := c.Predict.RunOperation(cmd.Context(), net, args[0], note)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "predicted snapshot", poller.Current().ID, "processing…")
			s, _, err := poller.Wait(cmd.Context(), forward.PollOptions[forward.Snapshot]{Interval: 10 * time.Second})
			if err != nil {
				return err
			}
			fmt.Println(s.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "fwdctl", "")
	return cmd
}
