package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/cli/hook"
)

// newHookCmd registers `cairn hook` and its four children:
//
//	cairn hook check-drift    (CC-invoked runtime; Stop hook)
//	cairn hook enable         (operator; install hook entries)
//	cairn hook disable        (operator; strip hook entries)
//	cairn hook status         (operator; report install state)
//
// The check-drift child is intentionally in the same command tree as
// the management verbs so operators have a single place to look.
// check-drift reads stdin JSON from Claude Code; the others take
// --scope flags on the command line.
func newHookCmd(app *App) *cobra.Command {
	root := &cobra.Command{
		Use:   "hook",
		Short: "Opt-in Claude Code Stop hook for spec/YAML drift detection",
		Long: `Opt-in harness-enforced drift detection.

  cairn hook enable             # install the Stop hook (scope=user by default)
  cairn hook disable            # remove it
  cairn hook status             # report install state across all scopes
  cairn hook check-drift        # CC invokes this at Stop; reads stdin JSON

The drift check verifies that every specs/**/*.yaml file's
# cairn-derived: header still matches the SHA-256 of its source prose
file. Warns on mismatch. Warn-only; never blocks.`,
	}
	root.AddCommand(newHookCheckDriftCmd(app))
	root.AddCommand(newHookEnableCmd(app))
	root.AddCommand(newHookDisableCmd(app))
	root.AddCommand(newHookStatusCmd(app))
	return root
}

// newHookCheckDriftCmd is the CC-invoked hook runtime. Reads stdin
// JSON for cwd (falls back to os.Getwd), walks specs/**/*.yaml, and
// emits warnings.
//
// Exit codes:
//
//	0   clean OR short-circuited (non-cairn repo, no specs/)
//	1   one or more warnings, OR recovered panic, OR handled error
func newHookCheckDriftCmd(_ *App) *cobra.Command {
	return &cobra.Command{
		Use:    "check-drift",
		Short:  "Run source-hash drift check (invoked by CC Stop hook)",
		Hidden: true, // operators don't call this directly
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Strict warn-only guarantee: any unexpected panic on this
			// path converts to exit 1 instead of the default Go exit 2.
			panicked := hook.GuardPanic(func() {
				runCheckDrift(cmd)
			})
			if panicked {
				os.Exit(1)
			}
			return nil
		},
	}
}

func runCheckDrift(cmd *cobra.Command) {
	stdin := cmd.InOrStdin()
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	in, err := hook.ReadInput(stdin)
	if err != nil {
		// Called directly by a human (empty stdin) or CC sent garbage.
		// Print a guidance line to stderr and exit 1. Envelope on
		// stdout still flies so automation can distinguish.
		cli.WriteEnvelope(stdout, cli.Envelope{
			Kind: "hook.check_drift",
			Err:  cairnerr.New(cairnerr.CodeBadInput, "hook_input_invalid", err.Error()),
		})
		fmt.Fprintln(stderr, "cairn hook check-drift:", err)
		os.Exit(1)
	}

	cwd := in.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Second short-circuit (beyond CheckDrift's own no-specs-dir
	// handling): if the repo isn't cairn-tracked at all, stay silent.
	// This matters when hooks are installed at user scope and the
	// operator opens CC in a non-cairn repo — we don't want noise.
	if !hook.IsCairnTracked(cwd, cli.ResolveStateRoot("")) {
		cli.WriteEnvelope(stdout, cli.Envelope{
			Kind: "hook.check_drift",
			Data: hook.CheckDriftResult{
				Warnings:   []string{},
				Clean:      true,
				Skipped:    true,
				SkipReason: "non_cairn_repo",
			},
		})
		os.Exit(0)
	}

	res, err := hook.CheckDrift(cwd)
	if err != nil {
		cli.WriteEnvelope(stdout, cli.Envelope{
			Kind: "hook.check_drift",
			Err:  err,
		})
		fmt.Fprintln(stderr, "cairn hook check-drift:", err)
		os.Exit(1)
	}

	hook.WriteDriftWarnings(stderr, res)
	cli.WriteEnvelope(stdout, cli.Envelope{
		Kind: "hook.check_drift",
		Data: res,
	})
	if res.Clean {
		os.Exit(0)
	}
	os.Exit(1)
}

func newHookEnableCmd(_ *App) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Install cairn Stop hook in Claude Code settings.json",
		Long: `Writes the Stop hook entry plus a "cairn" config block into
the resolved settings.json. Idempotent: re-running on an already-
enabled scope is a no-op.

Scope precedence for user scope: --scope flag > CLAUDE_HOME env >
~/.claude`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := hook.ParseScope(scope)
			if err != nil {
				os.Exit(cli.ExitCodeFor(cairnerr.New(cairnerr.CodeBadInput, "hook_bad_scope", err.Error())))
			}
			cwd, _ := os.Getwd()
			os.Exit(cli.Run(cmd.OutOrStdout(), "hook.enable", "", func() (any, error) {
				return hook.Enable(sc, cwd, "")
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "user", "settings.json scope (user|project|local)")
	return cmd
}

func newHookDisableCmd(_ *App) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Remove cairn Stop hook from Claude Code settings.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := hook.ParseScope(scope)
			if err != nil {
				os.Exit(cli.ExitCodeFor(cairnerr.New(cairnerr.CodeBadInput, "hook_bad_scope", err.Error())))
			}
			cwd, _ := os.Getwd()
			os.Exit(cli.Run(cmd.OutOrStdout(), "hook.disable", "", func() (any, error) {
				return hook.Disable(sc, cwd, "")
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "user", "settings.json scope (user|project|local)")
	return cmd
}

func newHookStatusCmd(_ *App) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report cairn Stop hook install state across settings.json scopes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var scopes []hook.Scope
			if scope != "" {
				sc, err := hook.ParseScope(scope)
				if err != nil {
					os.Exit(cli.ExitCodeFor(cairnerr.New(cairnerr.CodeBadInput, "hook_bad_scope", err.Error())))
				}
				scopes = []hook.Scope{sc}
			}
			cwd, _ := os.Getwd()
			os.Exit(cli.Run(cmd.OutOrStdout(), "hook.status", "", func() (any, error) {
				return hook.Status(scopes, cwd, "")
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "settings.json scope to check (user|project|local); default: all")
	return cmd
}
