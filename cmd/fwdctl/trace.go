package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func newTraceCmd() *cobra.Command {
	var tcpPort, srcPort, from string
	var raw bool
	cmd := &cobra.Command{
		Use:   "trace <snapshot|latest> <src-ip> <dst-ip>",
		Short: "PREVIEW/UNDOCUMENTED per-hop trace of a flow: headers after each hop, network functions with config lines, every path",
		Long: "PREVIEW / UNDOCUMENTED. Calls the GUI's own flow-trace routes (trace-facets, then trace for every path it lists,\n" +
			"alternatives through different devices included, up to " + fmt.Sprint(forward.ExplainLimit) + "). Forward does not document these routes and\n" +
			"may change them in any build; the output shape is only as stable as they are. Read-only.\n\n" +
			"Each hop's headers are the packet's values AFTER that hop; ingressHeaders is the packet entering the first hop. A snapshot\n" +
			"id from a prediction works the same way as a collected one (not yet run against a predicted snapshot). Pass an empty\n" +
			"src-ip (\"\") with --from DEVICE to enumerate paths from a device.\n\n" +
			"--raw prints each path's trace exactly as Forward sent it instead of the SDK's decoded form.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshot := args[0]
			if snapshot == "latest" {
				snapshot = ""
			}
			q := forward.TraceQuery{SrcIP: args[1], DstIP: args[2], From: from, SrcPort: srcPort}
			if tcpPort != "" {
				tcp := 6
				q.IPProto, q.DstPort = &tcp, tcpPort
			} else if srcPort != "" {
				tcp := 6
				q.IPProto = &tcp
			}
			ex, _, err := client(cmd).Trace.Explain(cmd.Context(), network(cmd), snapshot, q)
			if err != nil {
				return err
			}
			if ex.Truncated {
				fmt.Fprintf(os.Stderr, "fwdctl: more than %d paths; the rest were not traced\n", forward.ExplainLimit)
			}
			out := make([]any, 0, len(ex.Paths))
			for _, p := range ex.Paths {
				if raw && p.Trace != nil {
					out = append(out, map[string]any{"id": p.ID, "group": p.Group, "index": p.Index, "traceType": p.TraceType,
						"path": p.Path, "descriptor": p.Descriptor, "trace": json.RawMessage(p.Trace.Raw)})
					continue
				}
				out = append(out, p)
			}
			return dump(map[string]any{
				"preview": "undocumented Forward routes (trace-facets, trace); shape may change without notice",
				"from":    from, "truncated": ex.Truncated, "facetsTimedOut": ex.FacetsTimedOut, "resultsLowerBound": ex.ResultsLowerBound,
				"paths": out,
			})
		},
	}
	cmd.Flags().StringVar(&tcpPort, "tcp-port", "", "trace a TCP flow to this destination port")
	cmd.Flags().StringVar(&srcPort, "src-port", "", "source port (TCP)")
	cmd.Flags().StringVar(&from, "from", "", "device the flow enters at (needed when the source is at several places)")
	cmd.Flags().BoolVar(&raw, "raw", false, "print each trace exactly as Forward sent it")
	return cmd
}
