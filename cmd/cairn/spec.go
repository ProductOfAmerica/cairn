package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/intent"
)

func newSpecCmd(app *App) *cobra.Command {
	spec := &cobra.Command{Use: "spec", Short: "Spec tools"}
	var path string
	validate := &cobra.Command{
		Use:   "validate",
		Short: "Schema + referential + uniqueness validation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "spec.validate", "", func() (any, error) {
				bundle, err := intent.Load(path)
				if err != nil {
					return nil, cairnerr.New(cairnerr.CodeBadInput, "load_failed", err.Error()).WithCause(err)
				}
				errs := intent.Validate(bundle)
				if len(errs) == 0 {
					return map[string]any{"errors": []any{}}, nil
				}
				return nil, cairnerr.New(cairnerr.CodeValidation, "spec_invalid",
					"see errors").WithDetails(map[string]any{"errors": errs})
			}))
			return nil
		},
	}
	validate.Flags().StringVar(&path, "path", "specs", "root directory containing requirements/ and tasks/")
	spec.AddCommand(validate)
	_ = app
	return spec
}
