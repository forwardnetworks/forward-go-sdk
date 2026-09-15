package main

import (
	"fmt"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func newCollectorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collectors [first-username]",
		Short: "List every collector, or print the first one's username",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _, err := client(cmd).Collectors.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(args) == 1 && args[0] == "first-username" {
				if len(list) == 0 {
					return fmt.Errorf("no collectors")
				}
				fmt.Println(list[0].Username)
				return nil
			}
			return dump(list)
		},
	}
	return cmd
}

func newCollectorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collector",
		Short: "Show which collector the network uses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			attachment, _, err := client(cmd).Collectors.Attachment(cmd.Context(), network(cmd))
			if err != nil {
				return err
			}
			return dump(attachment)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "attach <username>",
		Short: "Attach a collector to the network by username",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := client(cmd).Collectors.Attach(cmd.Context(), network(cmd),
				forward.CollectorAttachmentRequest{Username: args[0]})
			return err
		},
	})
	return cmd
}
