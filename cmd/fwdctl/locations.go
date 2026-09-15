package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func newLocationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "locations",
		Short: "List the network's locations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _, err := client(cmd).Locations.List(cmd.Context(), network(cmd))
			if err != nil {
				return err
			}
			for _, l := range list {
				fmt.Printf("%-24s %-20s %9.4f %9.4f %s\n", l.ID, l.Name, l.Lat, l.Lng, l.City)
			}
			return nil
		},
	}
	cmd.AddCommand(newLocationsAddCmd())
	return cmd
}

func newLocationsAddCmd() *cobra.Command {
	var city, country string
	cmd := &cobra.Command{
		Use:   "add <name> <lat> <lng>",
		Short: "Add a location; adding an existing name is a no-op",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, c, net := cmd.Context(), client(cmd), network(cmd)
			if id, err := locationByName(ctx, c, net, args[0]); err == nil {
				fmt.Printf("location %s already exists (%s)\n", args[0], id)
				return nil
			}
			lat, err := strconv.ParseFloat(args[1], 64)
			if err != nil {
				return fmt.Errorf("lat: %w", err)
			}
			lng, err := strconv.ParseFloat(args[2], 64)
			if err != nil {
				return fmt.Errorf("lng: %w", err)
			}
			loc, _, err := c.Locations.Create(ctx, net, forward.LocationCreateRequest{
				Name: args[0], Lat: lat, Lng: lng, City: city, Country: country,
			})
			if err != nil {
				return err
			}
			if loc != nil {
				fmt.Printf("created location %s (%s)\n", loc.Name, loc.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&city, "city", "", "")
	cmd.Flags().StringVar(&country, "country", "", "")
	return cmd
}
