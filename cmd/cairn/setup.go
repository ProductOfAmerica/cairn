package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)

// newSetupCmd registers "cairn setup" — state bootstrap + installs the
// cairn Claude Code skills into <cwd>/.claude/skills/ so Claude Code
// auto-discovers them (no /plugin install required). Idempotent; writes
// nothing if files already exist, unless --force is passed.
func newSetupCmd(app *App) *cobra.Command {
	var (
		repoRoot string
		force    bool
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize cairn state + install Claude Code skills into the project",
		Long: `Runs the same state bootstrapping as "cairn init" and additionally
writes cairn's Claude Code skills into <cwd>/.claude/skills/ so Claude
Code auto-discovers them. No /plugin install step is required.

State bootstrap is idempotent. Skill files are written on first setup
and skipped on re-run; pass --force to overwrite (e.g. after upgrading
cairn to pick up updated skills).

Output: JSON envelope on stdout (kind "setup", data includes the
Bootstrap result plus the skill install summary), human-readable
post-install guidance on stderr.`,
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
				bootstrap, err := cli.Bootstrap(cwd, app.Flags.StateRoot)
				if err != nil {
					return nil, err
				}
				install, err := cli.InstallSkills(cwd, force)
				if err != nil {
					return nil, err
				}
				printSetupHints(cmd.ErrOrStderr(), install)
				return map[string]any{
					"repo_id":   bootstrap.RepoID,
					"state_dir": bootstrap.StateDir,
					"db_path":   bootstrap.DBPath,
					"skills":    install,
				}, nil
			}))
			return nil
		},
	}
	cmd.Flags().StringVar(&repoRoot, "repo-root", "", "override cwd")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing .claude/skills files (use after upgrading cairn)")
	return cmd
}

// printSetupHints writes human-readable post-install guidance to w.
// Content reflects whether skills were written or already present, so
// a re-run shows the correct message.
func printSetupHints(w io.Writer, install *cli.InstallSkillsResult) {
	fmt.Fprintf(w, "\ncairn state initialized.\n")

	if len(install.Written) > 0 {
		fmt.Fprintf(w, "Installed %d cairn skill file(s) at %s.\n", len(install.Written), install.Root)
		fmt.Fprintf(w, "Restart Claude Code (or start a new session) to pick them up.\n")
	}
	if len(install.Skipped) > 0 && len(install.Written) == 0 {
		fmt.Fprintf(w, "All %d skill file(s) already present at %s.\n", len(install.Skipped), install.Root)
		fmt.Fprintf(w, "Pass --force after upgrading cairn to refresh them.\n")
	}
	if len(install.Skipped) > 0 && len(install.Written) > 0 {
		fmt.Fprintf(w, "(%d file(s) skipped; pass --force to overwrite.)\n", len(install.Skipped))
	}

	fmt.Fprint(w, `
Skills now available in Claude Code for this repo:
  - using-cairn
  - subagent-driven-development-with-verdicts
  - verdict-backed-verification

Other agentic harnesses (Cursor, Codex, Gemini, Copilot, OpenCode)
can drive the CLI directly; skill-layer wraps for them are tracked
in docs/PLAN.md (Ship 5+).

Verify:
  cairn spec validate
  cairn task list
`)
}
