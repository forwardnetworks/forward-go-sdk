package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newChecksCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "checks", Short: "Saved-check operations"}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list <snapshot>",
			Short: "List a snapshot's saved checks and their status",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				list, _, err := client(cmd).Checks.List(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				for _, ch := range list {
					fmt.Printf("%-5s %s\n", ch.Status, ch.Name)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "diff <base> <predicted>",
			Short: "Print checks that pass on base and fail on predicted; exit 1 if any do",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, ctx := client(cmd), cmd.Context()
				before, _, err := c.Checks.List(ctx, args[0])
				if err != nil {
					return err
				}
				after, _, err := c.Checks.List(ctx, args[1])
				if err != nil {
					return err
				}
				passing := map[string]bool{}
				for _, ch := range before {
					if ch.Status == "PASS" {
						passing[ch.Name] = true
					}
				}
				var broke []string
				for _, ch := range after {
					if ch.Status == "FAIL" && passing[ch.Name] {
						broke = append(broke, ch.Name)
					}
				}
				sort.Strings(broke)
				if len(broke) == 0 {
					fmt.Println("no regression")
					return nil
				}
				fmt.Println("blocked:", strings.Join(broke, ", "))
				os.Exit(1)
				return nil
			},
		},
	)
	return cmd
}
