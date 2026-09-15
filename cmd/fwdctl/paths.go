package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func newPathsCmd() *cobra.Command {
	var tcpPort string
	cmd := &cobra.Command{
		Use:   "paths <snapshot> <src-ip> <dst-ip>",
		Short: "Path search; prints outcome and the device hops for each path",
		Long: "Path search; prints outcome and the device hops for each path.\n\n" +
			"FWD_FROM, if set, names the device the search starts from.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := forward.PathSearchRequest{SnapshotID: args[0], SrcIP: args[1], DstIP: args[2], From: os.Getenv("FWD_FROM")}
			if tcpPort != "" {
				tcp := 6
				req.IPProto, req.DstPort = &tcp, tcpPort
			}
			out, _, err := client(cmd).Networks.Paths(cmd.Context(), network(cmd), req)
			if err != nil {
				return err
			}
			for _, p := range out.Info.Paths {
				hops := make([]string, 0, len(p.Hops))
				for _, h := range p.Hops {
					hops = append(hops, h.DeviceName)
				}
				fmt.Printf("%s/%s  %s\n", p.ForwardingOutcome, p.SecurityOutcome, strings.Join(hops, " > "))
			}
			if len(out.Info.Paths) == 0 {
				fmt.Println("no paths")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tcpPort, "tcp-port", "", "search a TCP flow to this destination port")
	return cmd
}
