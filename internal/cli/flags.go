package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

// GlobalFlags tracks --op-id, --state-root, --format, --verbose.
type GlobalFlags struct {
	OpID      string
	StateRoot string
	Format    string
	Verbose   bool
}

// Register attaches global flags to the root command.
func (g *GlobalFlags) Register(root *cobra.Command) {
	root.PersistentFlags().StringVar(&g.OpID, "op-id", "",
		"caller-supplied idempotency key (ULID); auto-generated if omitted")
	root.PersistentFlags().StringVar(&g.StateRoot, "state-root", "",
		"override state-root (CAIRN_HOME / XDG / %USERPROFILE%)")
	root.PersistentFlags().StringVar(&g.Format, "format", "json",
		"output format (only 'json' supported in Ship 1)")
	root.PersistentFlags().BoolVar(&g.Verbose, "verbose", false,
		"bump stderr log level to DEBUG")
}

// ResolveOpID returns g.OpID or generates a new ULID. It also validates the
// caller-supplied format when one was provided.
func (g *GlobalFlags) ResolveOpID(gen *ids.Generator) (string, error) {
	if g.OpID == "" {
		return gen.ULID(), nil
	}
	if err := ids.ValidateOpID(g.OpID); err != nil {
		return "", cairnerr.New(cairnerr.CodeBadInput, "bad_input",
			fmt.Sprintf("--op-id: %v", err))
	}
	return g.OpID, nil
}

// RequireJSONFormat rejects non-json formats (Ship 1 constraint).
func (g *GlobalFlags) RequireJSONFormat() error {
	if g.Format != "" && g.Format != "json" {
		return cairnerr.New(cairnerr.CodeBadInput, "bad_input",
			fmt.Sprintf("--format=%q not implemented in Ship 1 (use json)", g.Format))
	}
	return nil
}
