package main

import (
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

func newEventsCmd(app *App) *cobra.Command {
	root := &cobra.Command{Use: "events", Short: "Event log"}
	var limit int
	since := &cobra.Command{
		Use:   "since <timestamp_ms>",
		Short: "Events with at > timestamp_ms (integer ms since epoch)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ts, perr := strconv.ParseInt(args[0], 10, 64)
			if perr != nil || ts < 0 {
				err := cairnerr.New(cairnerr.CodeBadInput, "bad_input",
					"<timestamp_ms> must be a non-negative integer (ms since epoch)").
					WithDetails(map[string]any{"flag": "timestamp_ms", "value": args[0]})
				cli.WriteEnvelope(cmd.OutOrStdout(), cli.Envelope{Kind: "events.since", Err: err})
				os.Exit(cli.ExitCodeFor(err))
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "events.since", "", func() (any, error) {
				h, _, err := openStateDBWithBlobs(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				ev, err := events.Since(h.SQL(), ts, limit)
				if err != nil {
					return nil, err
				}
				return map[string]any{"events": ev}, nil
			}))
			return nil
		},
	}
	since.Flags().IntVar(&limit, "limit", 100, "max rows")
	root.AddCommand(since)
	return root
}
