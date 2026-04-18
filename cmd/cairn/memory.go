package main

import (
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/memory"
)

func newMemoryCmd(app *App) *cobra.Command {
	root := &cobra.Command{Use: "memory", Short: "Append-only cross-session memory"}
	root.AddCommand(newMemoryAppendCmd(app))
	root.AddCommand(newMemorySearchCmd(app))
	root.AddCommand(newMemoryListCmd(app))
	return root
}

// parseTagsCSV splits a comma-separated tag flag. Empty input yields nil so
// the library's "no tags" path is exercised.
func parseTagsCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseSinceFlag translates a raw --since string into a *int64 pointer (nil
// when flag was not set). Invalid integers return cairnerr{invalid_since}.
func parseSinceFlag(raw string, set bool) (*int64, error) {
	if !set {
		return nil, nil
	}
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return nil, cairnerr.New(cairnerr.CodeBadInput, "invalid_since",
			"--since must be integer ms since epoch").
			WithDetails(map[string]any{"got": raw})
	}
	return &v, nil
}

func newMemoryAppendCmd(app *App) *cobra.Command {
	var kind, body, entityKind, entityID, tagsCSV string
	cmd := &cobra.Command{
		Use:   "append",
		Short: "Append a memory entry (decision|rationale|outcome|failure)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opID, err := app.Flags.ResolveOpID(app.IDs)
			if err != nil {
				cli.WriteEnvelope(cmd.OutOrStdout(), cli.Envelope{Kind: "memory.append", Err: err})
				os.Exit(cli.ExitCodeFor(err))
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "memory.append", opID, func() (any, error) {
				h, err := openStateDB(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res memory.AppendResult
				err = h.WithTx(cmd.Context(), func(tx *db.Tx) error {
					store := memory.NewStore(tx, events.NewAppender(app.Clock), app.IDs, app.Clock)
					r, err := store.Append(memory.AppendInput{
						OpID:       opID,
						Kind:       kind,
						Body:       body,
						EntityKind: entityKind,
						EntityID:   entityID,
						Tags:       parseTagsCSV(tagsCSV),
					})
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "decision|rationale|outcome|failure (required)")
	cmd.Flags().StringVar(&body, "body", "", "memory body text (required)")
	cmd.Flags().StringVar(&entityKind, "entity-kind", "", "optional entity kind (paired with --entity-id)")
	cmd.Flags().StringVar(&entityID, "entity-id", "", "optional entity id (paired with --entity-kind)")
	cmd.Flags().StringVar(&tagsCSV, "tags", "", "comma-separated tags (e.g. a,b,c)")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}

func newMemorySearchCmd(app *App) *cobra.Command {
	var limit int
	var kind, entityKind, entityID, sinceRaw string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "FTS5 search over memory bodies and tags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "memory.search", "", func() (any, error) {
				since, err := parseSinceFlag(sinceRaw, cmd.Flags().Changed("since"))
				if err != nil {
					return nil, err
				}
				effectiveLimit := limit
				if effectiveLimit == 0 {
					effectiveLimit = math.MaxInt32
				}
				h, err := openStateDB(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res memory.SearchResult
				err = h.WithReadTx(cmd.Context(), func(tx *db.Tx) error {
					store := memory.NewStore(tx, events.NewAppender(app.Clock), app.IDs, app.Clock)
					r, err := store.Search(memory.SearchInput{
						Query:      args[0],
						Kind:       kind,
						EntityKind: entityKind,
						EntityID:   entityID,
						Since:      since,
						Limit:      effectiveLimit,
					})
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "max results; 0 = unlimited")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind (decision|rationale|outcome|failure)")
	cmd.Flags().StringVar(&entityKind, "entity-kind", "", "filter by entity kind (paired with --entity-id)")
	cmd.Flags().StringVar(&entityID, "entity-id", "", "filter by entity id (paired with --entity-kind)")
	cmd.Flags().StringVar(&sinceRaw, "since", "", "filter entries at >= since (ms since epoch)")
	return cmd
}

func newMemoryListCmd(app *App) *cobra.Command {
	var limit int
	var kind, entityKind, entityID, sinceRaw string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List memory entries newest-first (optional filters)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "memory.list", "", func() (any, error) {
				since, err := parseSinceFlag(sinceRaw, cmd.Flags().Changed("since"))
				if err != nil {
					return nil, err
				}
				effectiveLimit := limit
				if effectiveLimit == 0 {
					effectiveLimit = math.MaxInt32
				}
				h, err := openStateDB(app)
				if err != nil {
					return nil, err
				}
				defer h.Close()
				var res memory.ListResult
				err = h.WithReadTx(cmd.Context(), func(tx *db.Tx) error {
					store := memory.NewStore(tx, events.NewAppender(app.Clock), app.IDs, app.Clock)
					r, err := store.List(memory.ListInput{
						Kind:       kind,
						EntityKind: entityKind,
						EntityID:   entityID,
						Since:      since,
						Limit:      effectiveLimit,
					})
					res = r
					return err
				})
				return res, err
			}))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "max results; 0 = unlimited")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind (decision|rationale|outcome|failure)")
	cmd.Flags().StringVar(&entityKind, "entity-kind", "", "filter by entity kind (paired with --entity-id)")
	cmd.Flags().StringVar(&entityID, "entity-id", "", "filter by entity id (paired with --entity-kind)")
	cmd.Flags().StringVar(&sinceRaw, "since", "", "filter entries at >= since (ms since epoch)")
	return cmd
}
