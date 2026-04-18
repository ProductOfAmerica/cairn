package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

func newReconcileCmd(app *App) *cobra.Command {
	var dryRun, evidenceSampleFull bool
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Run reconcile (rules 1–5) against the state DB",
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "reconcile", "", func() (any, error) {
				h, blobRoot, err := openStateDBWithBlobs(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				orch := reconcile.NewOrchestrator(h, app.Clock, app.IDs, blobRoot)
				opts := reconcile.Opts{
					DryRun:             dryRun,
					EvidenceSampleFull: evidenceSampleFull,
				}
				if dryRun {
					return orch.DryRun(cmd.Context(), opts)
				}
				return orch.Run(cmd.Context(), opts)
			}))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "simulate reconcile without mutations or events")
	cmd.Flags().BoolVar(&evidenceSampleFull, "evidence-sample-full", false, "rule 3 scans every evidence row (default: sampled)")
	return cmd
}
