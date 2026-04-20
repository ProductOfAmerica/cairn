// Package skills embeds cairn's Claude Code skill sources so the CLI
// can install them into a user project's .claude/skills/ tree via
// "cairn setup" — the same auto-discovery path GitNexus uses.
//
// Claude Code auto-loads any skill under a project's .claude/skills/
// directory without requiring /plugin install, so writing these files
// on setup removes the manual marketplace step entirely.
package skills

import "embed"

//go:embed all:using-cairn all:subagent-driven-development-with-verdicts all:verdict-backed-verification
var FS embed.FS
