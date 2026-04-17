package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/evidence"
	"github.com/ProductOfAmerica/cairn/internal/repoid"
	"github.com/ProductOfAmerica/cairn/internal/verdict"
)

// openStateDBWithBlobs returns the DB handle and the blob root path.
func openStateDBWithBlobs(app *App) (*db.DB, string, error) {
	cwd, _ := os.Getwd()
	id, err := repoid.Resolve(cwd)
	if err != nil {
		return nil, "", cairnerr.New(cairnerr.CodeBadInput, "not_a_git_repo", err.Error())
	}
	stateRoot := cli.ResolveStateRoot(app.Flags.StateRoot)
	stateDir := filepath.Join(stateRoot, id)
	h, err := db.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		return nil, "", err
	}
	return h, filepath.Join(stateDir, "blobs"), nil
}

func newVerdictCmd(app *App) *cobra.Command {
	root := &cobra.Command{Use: "verdict", Short: "Verdict lifecycle"}
	root.AddCommand(newVerdictReportCmd(app))
	root.AddCommand(newVerdictLatestCmd(app))
	root.AddCommand(newVerdictHistoryCmd(app))
	return root
}

func newVerdictReportCmd(app *App) *cobra.Command {
	var gate, run, status, evPath, producerHash, inputsHash, scoreJSON string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Bind a verdict to a gate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opID, err := app.Flags.ResolveOpID(app.IDs)
			if err != nil {
				cli.WriteEnvelope(cmd.OutOrStdout(), cli.Envelope{Kind: "verdict.report", Err: err})
				os.Exit(cli.ExitCodeFor(err))
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "verdict.report", opID, func() (any, error) {
				// Compute sha256 of the evidence file — required so the Store
				// can look it up by content hash.
				data, rerr := os.ReadFile(evPath)
				if rerr != nil {
					return nil, cairnerr.New(cairnerr.CodeBadInput, "path_unreadable", rerr.Error()).WithCause(rerr)
				}
				_ = data
				h, blobRoot, err := openStateDBWithBlobs(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res verdict.ReportResult
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					evStore := evidence.NewStore(tx, events.NewAppender(app.Clock), app.IDs, blobRoot)
					put, perr := evStore.Put(opID+":ev", evPath, "")
					if perr != nil {
						return perr
					}
					vStore := verdict.NewStore(tx, events.NewAppender(app.Clock), app.IDs, evStore)
					r, verr := vStore.Report(verdict.ReportInput{
						OpID: opID, GateID: gate, RunID: run, Status: status,
						Sha256: put.SHA256, ProducerHash: producerHash,
						InputsHash: inputsHash, ScoreJSON: scoreJSON,
					})
					res = r
					return verr
				})
				return res, err
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&gate, "gate", "", "gate id (required)")
	cmd.Flags().StringVar(&run, "run", "", "run id (required)")
	cmd.Flags().StringVar(&status, "status", "", "pass|fail|inconclusive (required)")
	cmd.Flags().StringVar(&evPath, "evidence", "", "path to evidence file (required)")
	cmd.Flags().StringVar(&producerHash, "producer-hash", "", "64-char hex sha256 (required)")
	cmd.Flags().StringVar(&inputsHash, "inputs-hash", "", "64-char hex sha256 (required)")
	cmd.Flags().StringVar(&scoreJSON, "score-json", "", "optional score body")
	for _, f := range []string{"gate", "run", "status", "evidence", "producer-hash", "inputs-hash"} {
		_ = cmd.MarkFlagRequired(f)
	}
	return cmd
}

func newVerdictLatestCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "latest <gate_id>",
		Short: "Latest verdict for a gate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "verdict.latest", "", func() (any, error) {
				h, blobRoot, err := openStateDBWithBlobs(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res verdict.LatestResult
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					evStore := evidence.NewStore(tx, events.NewAppender(app.Clock), app.IDs, blobRoot)
					vStore := verdict.NewStore(tx, events.NewAppender(app.Clock), app.IDs, evStore)
					r, err := vStore.Latest(args[0])
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
}

func newVerdictHistoryCmd(app *App) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "history <gate_id>",
		Short: "Verdict history for a gate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "verdict.history", "", func() (any, error) {
				h, blobRoot, err := openStateDBWithBlobs(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res []verdict.VerdictWithFresh
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					evStore := evidence.NewStore(tx, events.NewAppender(app.Clock), app.IDs, blobRoot)
					vStore := verdict.NewStore(tx, events.NewAppender(app.Clock), app.IDs, evStore)
					r, err := vStore.History(args[0], limit)
					res = r
					return err
				})
				return map[string]any{"verdicts": res}, err
			}))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "max rows")
	return cmd
}
