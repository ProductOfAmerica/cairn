package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

// App bundles long-lived singletons needed by every command.
type App struct {
	Clock clock.Clock
	IDs   *ids.Generator
	Flags *cli.GlobalFlags
}

func newApp() *App {
	c := clock.Wall{}
	return &App{
		Clock: c,
		IDs:   ids.NewGenerator(c),
		Flags: &cli.GlobalFlags{},
	}
}

func main() {
	app := newApp()
	root := &cobra.Command{
		Use:   "cairn",
		Short: "Verification substrate for AI-coordinated software development",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return app.Flags.RequireJSONFormat()
		},
	}
	app.Flags.Register(root)

	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd(app))
	root.AddCommand(newSpecCmd(app))
	root.AddCommand(newTaskCmd(app))
	root.AddCommand(newVerdictCmd(app))
	root.AddCommand(newEvidenceCmd(app))
	root.AddCommand(newEventsCmd(app))

	if err := root.Execute(); err != nil {
		// cobra already printed a usage message; emit a JSON envelope + exit.
		cli.WriteEnvelope(os.Stdout, cli.Envelope{
			Kind: "cli.error",
			Err:  err,
		})
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
