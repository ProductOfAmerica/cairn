package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/evidence"
)

func newEvidenceCmd(app *App) *cobra.Command {
	root := &cobra.Command{Use: "evidence", Short: "Content-addressed evidence store"}
	root.AddCommand(newEvidencePutCmd(app))
	root.AddCommand(newEvidenceVerifyCmd(app))
	root.AddCommand(newEvidenceGetCmd(app))
	return root
}

func newEvidencePutCmd(app *App) *cobra.Command {
	var contentType string
	cmd := &cobra.Command{
		Use:   "put <path>",
		Short: "Store a file as content-addressed evidence",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opID, err := app.Flags.ResolveOpID(app.IDs)
			if err != nil {
				cli.WriteEnvelope(cmd.OutOrStdout(), cli.Envelope{Kind: "evidence.put", Err: err})
				os.Exit(cli.ExitCodeFor(err))
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "evidence.put", opID, func() (any, error) {
				h, blobRoot, err := openStateDBWithBlobs(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res evidence.PutResult
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					store := evidence.NewStore(tx, events.NewAppender(app.Clock), app.IDs, blobRoot)
					r, err := store.Put(opID, args[0], contentType)
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&contentType, "content-type", "", "override default application/octet-stream")
	return cmd
}

func newEvidenceVerifyCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "verify <sha256>",
		Short: "Rehash a stored blob",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "evidence.verify", "", func() (any, error) {
				h, blobRoot, err := openStateDBWithBlobs(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					store := evidence.NewStore(tx, events.NewAppender(app.Clock), app.IDs, blobRoot)
					return store.Verify(args[0])
				})
				return map[string]any{"sha256": args[0], "verified_at": app.Clock.NowMilli()}, err
			}))
			return nil
		},
	}
}

func newEvidenceGetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "get <sha256>",
		Short: "Return the stored metadata for a sha",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "evidence.get", "", func() (any, error) {
				h, blobRoot, err := openStateDBWithBlobs(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res evidence.GetResult
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					store := evidence.NewStore(tx, events.NewAppender(app.Clock), app.IDs, blobRoot)
					r, err := store.Get(args[0])
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
}
