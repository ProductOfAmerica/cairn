package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/repoid"
)

func newInitCmd(app *App) *cobra.Command {
	var repoRoot string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize cairn state for the current repo",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd := repoRoot
			if cwd == "" {
				var err error
				cwd, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "init", "", func() (any, error) {
				id, err := repoid.Resolve(cwd)
				if err != nil {
					return nil, cairnerr.New(cairnerr.CodeBadInput, "not_a_git_repo",
						err.Error()).WithCause(err)
				}
				stateRoot := cli.ResolveStateRoot(app.Flags.StateRoot)
				stateDir := filepath.Join(stateRoot, id)
				if err := os.MkdirAll(filepath.Join(stateDir, "blobs"), 0o700); err != nil {
					return nil, cairnerr.New(cairnerr.CodeSubstrate, "mkdir_failed",
						err.Error()).WithCause(err)
				}
				dbPath := filepath.Join(stateDir, "state.db")
				h, err := db.Open(dbPath)
				if err != nil {
					return nil, cairnerr.New(cairnerr.CodeSubstrate, "db_open_failed",
						err.Error()).WithCause(err)
				}
				_ = h.Close()
				return map[string]any{
					"repo_id":   id,
					"state_dir": filepath.ToSlash(stateDir),
					"db_path":   filepath.ToSlash(dbPath),
				}, nil
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&repoRoot, "repo-root", "", "override cwd")
	_ = fmt.Sprintf // keep import if fmt is unused elsewhere
	return cmd
}
