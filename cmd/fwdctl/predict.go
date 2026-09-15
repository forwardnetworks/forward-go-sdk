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

// route add|update|remove|discard <change-set> <cloud-setup> <object> <destination> [target]
func newRouteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Stage a route-table edit on a cloud object",
	}
	for _, op := range []string{"add", "update", "remove", "discard"} {
		cmd.AddCommand(newRouteOpCmd(op))
	}
	return cmd
}

func newRouteOpCmd(op string) *cobra.Command {
	minArgs, use := 4, "<change-set> <cloud-setup> <object> <destination>"
	if op == "add" || op == "update" {
		use += " <target>"
		minArgs = 5
	}
	return &cobra.Command{
		Use:   op + " " + use,
		Short: "Stage a " + op + " on a route table",
		Args:  cobra.ExactArgs(minArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, c, net := cmd.Context(), client(cmd), network(cmd)
			cs, setup, object, destination := args[0], args[1], args[2], args[3]
			target := ""
			if len(args) > 4 {
				target = args[4]
			}
			r := forward.CloudRoute{Destination: destination, Target: target, Status: "active", Propagated: "no"}
			var err error
			switch op {
			case "add":
				_, err = c.Predict.AddRoute(ctx, net, cs, setup, object, r)
			case "update":
				_, err = c.Predict.UpdateRoute(ctx, net, cs, setup, object, destination, r)
			case "remove":
				_, err = c.Predict.RemoveRoute(ctx, net, cs, setup, object, destination)
			case "discard":
				_, err = c.Predict.DiscardRoute(ctx, net, cs, setup, object, destination)
			}
			return err
		},
	}
}

// rule add|remove|discard <change-set> <cloud-setup> <group> <protocol> <port-range> <source> [description]
func newRuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rule",
		Short: "Stage a security-group inbound-rule edit",
	}
	cmd.AddCommand(newRuleOpCmd("add"), newRuleOpCmd("remove"), newRuleOpCmd("discard"))
	return cmd
}

// Every op is keyed by the same (protocol, port-range, source) triple; only add uses --description.
func newRuleOpCmd(op string) *cobra.Command {
	var description string
	cmd := &cobra.Command{
		Use:   op + " <change-set> <cloud-setup> <group> <protocol> <port-range> <source>",
		Short: "Stage a " + op + " on a security group's inbound rules",
		Args:  cobra.ExactArgs(6),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, c, net := cmd.Context(), client(cmd), network(cmd)
			cs, setup, group := args[0], args[1], args[2]
			r := forward.InboundRule{Protocol: args[3], PortRange: args[4], Source: args[5], Description: description, Action: "allow"}
			var err error
			switch op {
			case "add":
				_, err = c.Predict.AddInboundRule(ctx, net, cs, setup, group, r)
			case "remove":
				_, err = c.Predict.RemoveInboundRule(ctx, net, cs, setup, group, r.Key())
			case "discard":
				_, err = c.Predict.DiscardInboundRule(ctx, net, cs, setup, group, r.Key())
			}
			return err
		},
	}
	if op == "add" {
		cmd.Flags().StringVar(&description, "description", "", "")
	}
	return cmd
}

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "plan", Short: "Terraform plan operations"}
	cmd.AddCommand(&cobra.Command{
		Use:   "import <change-set> <cloud-setup> <plan.json>",
		Short: "Import a `terraform show -json` plan onto a change set",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := os.ReadFile(args[2])
			if err != nil {
				return err
			}
			imported, _, err := client(cmd).Predict.ImportTerraformPlan(cmd.Context(), network(cmd), args[0], args[1], body)
			if err != nil {
				return err
			}
			return dump(imported)
		},
	})
	return cmd
}

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <change-set> <cloud-setup> <object>",
		Short: "Print a cloud object's route-table diff",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, _, err := client(cmd).Predict.RouteTableDiff(cmd.Context(), network(cmd), args[0], args[1], args[2])
			if err != nil {
				return err
			}
			for _, e := range d.Entries {
				row := e.B
				if row == nil {
					row = e.A
				}
				fmt.Printf("%-9s %-20s %s\n", e.DiffType, row.Destination, row.Target)
			}
			return nil
		},
	}
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
