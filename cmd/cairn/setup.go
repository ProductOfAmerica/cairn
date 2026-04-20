package main

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)

// newSetupCmd registers "cairn setup" — a superset of "cairn init" that
// also prints harness-integration hints to stderr. State initialization
// is idempotent, so running setup on an already-initialized repo is a
// no-op on disk but still prints the hints.
func newSetupCmd(app *App) *cobra.Command {
	var repoRoot string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize cairn state + print harness-integration instructions",
		Long: `Runs the same state bootstrapping as "cairn init" and additionally
emits Claude Code plugin-install instructions plus pointers for other
agentic harnesses. Safe to re-run — state setup is idempotent, and
the printed hints are fresh each time.

Output: JSON envelope on stdout (same shape as "cairn init"), plus a
human-readable hint block on stderr.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd := repoRoot
			if cwd == "" {
				var err error
				cwd, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			os.Exit(cli.Run(cmd.OutOrStdout(), "setup", "", func() (any, error) {
				result, err := cli.Bootstrap(cwd, app.Flags.StateRoot)
				if err != nil {
					return nil, err
				}
				printHarnessHints(cmd.ErrOrStderr())
				return result, nil
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&repoRoot, "repo-root", "", "override cwd")
	return cmd
}

// printHarnessHints writes Claude Code + other-harness install guidance
// to w. Platform-aware only for config-path display; install commands
// themselves are identical across OSes.
func printHarnessHints(w io.Writer) {
	configHint := "$HOME/.config/claude-code/"
	switch runtime.GOOS {
	case "darwin":
		configHint = "$HOME/Library/Application Support/Claude/"
	case "windows":
		configHint = "%APPDATA%\\Claude\\"
	}

	fmt.Fprintf(w, `
cairn state initialized. To use cairn's skills with an agentic harness:

  Claude Code
    1. Open Claude Code in this repo.
    2. Run:  /plugin
    3. Add the cairn marketplace:
         https://github.com/ProductOfAmerica/cairn
    4. Enable the cairn plugin when prompted.
    Claude Code config lives under %s

  Other harnesses (Cursor, Codex, Gemini, Copilot, OpenCode)
    cairn ships as a Claude Code plugin for now. The CLI works directly
    from these harnesses' shells; skill-layer wraps are tracked in
    docs/PLAN.md (Upstream posture / Ship 5+).

  Verify
    cairn spec validate
    cairn task list

Re-run "cairn setup" any time to see these hints again.
`, configHint)
}
