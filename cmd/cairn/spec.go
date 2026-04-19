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
		Long: `Schema + referential + uniqueness validation.

Flags:
  --path <dir>   Directory to scan (default: specs/).

Response includes:
  errors          List of validation errors (empty if all specs valid).
  specs_scanned   Object with counts of requirement/task files loaded.

specs_scanned counts files loaded, not files passed. Cross-reference
with errors for per-file status: len(errors) == 0 → all passed;
len(errors) > 0 → some failed, others may have passed.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			bundle, loadErr := intent.Load(path)
			if loadErr != nil {
				// Hard load failure (YAML parse, dir read). Preserve Ship 1 shape.
				cli.WriteEnvelope(out, cli.Envelope{
					Kind: "spec.validate",
					Err:  cairnerr.New(cairnerr.CodeBadInput, "load_failed", loadErr.Error()).WithCause(loadErr),
				})
				os.Exit(cli.ExitCodeFor(cairnerr.New(cairnerr.CodeBadInput, "load_failed", "")))
				return nil
			}

			errs := intent.Validate(bundle)
			if errs == nil {
				errs = []intent.SpecError{}
			}
			data := map[string]any{
				"errors": errs,
				"specs_scanned": map[string]any{
					"requirements": len(bundle.Requirements),
					"tasks":        len(bundle.Tasks),
				},
			}

			cli.WriteEnvelope(out, cli.Envelope{Kind: "spec.validate", Data: data})

			if len(errs) > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	validate.Flags().StringVar(&path, "path", "specs", "root directory containing requirements/ and tasks/")
	spec.AddCommand(validate)

	var (
		initPath  string
		initForce bool
	)
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold cairn spec directories with annotated templates",
		Long: `Scaffold cairn spec directories with annotated templates.

Creates:
  <path>/requirements/REQ-001.yaml.example
  <path>/tasks/TASK-001.yaml.example

Real YAML is derived from prose specs by the using-cairn skill; these
templates are reference only. Do not rename .example files to .yaml.

Flags:
  --path <dir>   Target directory (default: specs/).
  --force        Overwrite existing .example files.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "spec.init", "", func() (any, error) {
				res, err := cli.SpecInit(initPath, initForce)
				if err != nil {
					return nil, cairnerr.New(cairnerr.CodeSubstrate, "init_failed", err.Error()).WithCause(err)
				}
				return res, nil
			}))
			return nil
		},
	}
	initCmd.Flags().StringVar(&initPath, "path", "specs", "Target directory")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite existing .example files")
	spec.AddCommand(initCmd)

	_ = app
	return spec
}
