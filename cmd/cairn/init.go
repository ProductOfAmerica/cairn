package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cli"
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
				return cli.Bootstrap(cwd, app.Flags.StateRoot)
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&repoRoot, "repo-root", "", "override cwd")
	return cmd
}
