package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func newJumpServersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jump-servers",
		Short: "List the network's jump servers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _, err := client(cmd).JumpServers.List(cmd.Context(), network(cmd))
			if err != nil {
				return err
			}
			return dump(list)
		},
	}
	cmd.AddCommand(newJumpServersAddCmd())
	return cmd
}

func newJumpServersAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <host> <port> <username> <password>",
		Short: "Add a password-authenticated jump server the collector forwards device connections through",
		Long: "Add a password-authenticated jump server the collector forwards device connections through.\n\n" +
			"A jump server with that host already on the network is reused, so re-running adds nothing.",
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := client(cmd)
			net := network(cmd)
			port, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("port %q: %w", args[1], err)
			}
			if id, err := jumpServerByHost(ctx, c, net, args[0]); err == nil {
				fmt.Printf("jump server %s already present: %s\n", args[0], id)
				return nil
			}
			js, _, err := c.JumpServers.CreateWithPassword(ctx, net, forward.JumpServerPasswordRequest{
				Host: args[0], Port: port, Username: args[2], Password: args[3], SupportsPortForwarding: true,
			})
			if err != nil {
				return err
			}
			fmt.Printf("added jump server %s:%d as %s: %s\n", args[0], port, args[2], js.ID)
			return nil
		},
	}
	return cmd
}

// jumpServerByHost is the id of the network's jump server at host.
func jumpServerByHost(ctx context.Context, c *forward.Client, net, host string) (string, error) {
	list, _, err := c.JumpServers.List(ctx, net)
	if err != nil {
		return "", err
	}
	for _, js := range list {
		if js.Host == host {
			return string(js.ID), nil
		}
	}
	return "", fmt.Errorf("no jump server with host %q on network %s", host, net)
}
