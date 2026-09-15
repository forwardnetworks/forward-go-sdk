package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func newCloudAccountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud-accounts [name]",
		Short: "List cloud accounts, or show one by name",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _, err := client(cmd).CloudAccounts.List(cmd.Context(), network(cmd))
			if err != nil {
				return err
			}
			if len(args) == 0 {
				for _, a := range list {
					fmt.Printf("%-10s %s\n", a.Type, a.Name)
				}
				return nil
			}
			for _, a := range list {
				if a.Name == args[0] {
					return dump(a)
				}
			}
			return fmt.Errorf("no cloud account %q", args[0])
		},
	}
	cmd.AddCommand(newCloudAccountsCredentialCmd(), newCloudAccountsCollectCmd())
	return cmd
}

// credential <name> <accessKeyId> <secret>              -- AWS
// credential <name> --azure <clientId> <tenant> <secret> -- Azure
func newCloudAccountsCredentialCmd() *cobra.Command {
	var azure bool
	cmd := &cobra.Command{
		Use:   "credential <name> <args...>",
		Short: "Set a cloud account's credential",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, c, net := cmd.Context(), client(cmd), network(cmd)
			acct, err := cloudAccountByName(ctx, c, net, args[0])
			if err != nil {
				return err
			}
			req := forward.CloudAccountCredentialRequest{Type: acct.Type}
			rest := args[1:]
			if azure || strings.EqualFold(acct.Type, "AZURE") {
				if len(rest) < 3 {
					return fmt.Errorf("azure credential needs <clientId> <tenant> <secret>")
				}
				req.ClientID, req.Tenant, req.Password = rest[0], rest[1], rest[2]
			} else {
				if len(rest) < 2 {
					return fmt.Errorf("credential needs <accessKeyId> <secret>")
				}
				req.Username, req.Password = rest[0], rest[1]
			}
			_, err = c.CloudAccounts.UpdateCredential(ctx, net, args[0], req)
			return err
		},
	}
	cmd.Flags().BoolVar(&azure, "azure", false, "treat as an Azure credential regardless of the account's stored type")
	return cmd
}

func newCloudAccountsCollectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "collect <name> <on|off>",
		Short: "Turn collection on or off for a cloud account",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, c, net := cmd.Context(), client(cmd), network(cmd)
			acct, err := cloudAccountByName(ctx, c, net, args[0])
			if err != nil {
				return err
			}
			// A PATCH with only these fields leaves the account's credentials and environment as they are.
			_, _, err = c.CloudAccounts.Update(ctx, net, args[0], forward.CloudAccountRequest{
				Type: acct.Type, Name: acct.Name, Collect: args[1] == "on",
			})
			return err
		},
	}
}

func cloudAccountByName(ctx context.Context, c *forward.Client, net, name string) (forward.CloudAccount, error) {
	list, _, err := c.CloudAccounts.List(ctx, net)
	if err != nil {
		return forward.CloudAccount{}, err
	}
	for _, a := range list {
		if a.Name == name {
			return a, nil
		}
	}
	return forward.CloudAccount{}, fmt.Errorf("no cloud account %q", name)
}
