package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func newDevicesCmd() *cobra.Command {
	var snapshotID string
	cmd := &cobra.Command{
		Use:   "devices [name-substring]",
		Short: "List devices, name/type/collection source, optionally filtered by name",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _, err := client(cmd).Devices.List(cmd.Context(), network(cmd), forward.DeviceListOptions{SnapshotID: snapshotID})
			if err != nil {
				return err
			}
			filter := ""
			if len(args) == 1 {
				filter = args[0]
			}
			for _, d := range list {
				if filter == "" || strings.Contains(d.Name, filter) {
					fmt.Printf("%-40s %-22s %s\n", d.Name, d.Type, d.SourceName)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&snapshotID, "snapshot", "", "list as of this snapshot rather than the latest")

	cmd.AddCommand(newDevicesAddCmd(), newDevicesRemoveCmd())
	return cmd
}

func newDevicesAddCmd() *cobra.Command {
	var location, jump string
	cmd := &cobra.Command{
		Use:   "add <name> <host> <type> <username> <password>",
		Short: "Add a classic device, reusing a matching CLI credential if one exists",
		Long: "Add a classic device, reusing a matching CLI credential if one exists.\n\n" +
			"A CLI credential named user@type is reused when it already exists, so re-running\n" +
			"adds the device, not a duplicate. --location places it on the Forward map by name;\n" +
			"--jump reaches the device through the network's jump server with that host.",
		Args: cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := client(cmd)
			net := network(cmd)
			name, host, kind, user, pass := args[0], args[1], args[2], args[3], args[4]

			locationID := ""
			if location != "" {
				id, err := locationByName(ctx, c, net, location)
				if err != nil {
					return err
				}
				locationID = id
			}

			jumpID := ""
			if jump != "" {
				id, err := jumpServerByHost(ctx, c, net, jump)
				if err != nil {
					return err
				}
				jumpID = id
			}

			credName := user + "@" + kind
			creds, _, err := c.Credentials.ListCLI(ctx, net)
			if err != nil {
				return err
			}
			credID := ""
			for _, cred := range creds {
				if strings.EqualFold(cred.Name, credName) {
					credID = cred.ID
				}
			}
			if credID == "" {
				created, _, err := c.Credentials.CreateCLI(ctx, net,
					forward.CLICredentialRequest{Name: credName, Username: user, Password: pass})
				if err != nil {
					return err
				}
				credID = created.ID
			}

			collect := true
			dev, _, err := c.ClassicDevices.Create(ctx, net, forward.ClassicDeviceRequest{
				Name: name, Host: host, Type: kind, CLICredentialID: credID, Collect: &collect, JumpServerID: jumpID,
			})
			if err != nil {
				return err
			}
			fmt.Printf("added %s (%s) at %s with credential %s\n", dev.Name, dev.Type, dev.Host, credID)
			if locationID != "" {
				if _, err := c.Locations.Assign(ctx, net, forward.AtlasPatch{name: locationID}); err != nil {
					return err
				}
				fmt.Printf("placed %s at %s\n", name, location)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&location, "location", "", "place the device at this location (by name)")
	cmd.Flags().StringVar(&jump, "jump", "", "reach the device through the jump server with this host")
	return cmd
}

func newDevicesRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a classic device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := client(cmd).ClassicDevices.Delete(cmd.Context(), network(cmd), args[0])
			return err
		},
	}
}

func locationByName(ctx context.Context, c *forward.Client, net, name string) (string, error) {
	list, _, err := c.Locations.List(ctx, net)
	if err != nil {
		return "", err
	}
	for _, l := range list {
		if l.Name == name {
			return string(l.ID), nil
		}
	}
	return "", fmt.Errorf("no location named %q (create it with: fwdctl locations add %s <lat> <lng>)", name, name)
}
