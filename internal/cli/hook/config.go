package hook

import (
	"fmt"
	"os"
	"path/filepath"
)

// Scope identifies which Claude Code settings.json file a hook
// management command operates on. CC's fourth scope (managed, used by
// enterprise policy) is deliberately not reachable from cairn — see
// code.claude.com/docs/en/settings for the full scope hierarchy.
type Scope string

const (
	// ScopeUser is the per-user default (global) — ~/.claude/settings.json.
	ScopeUser Scope = "user"
	// ScopeProject is the per-repo committed config —
	// <repo>/.claude/settings.json.
	ScopeProject Scope = "project"
	// ScopeLocal is the per-repo non-committed config —
	// <repo>/.claude/settings.local.json.
	ScopeLocal Scope = "local"
)

// ParseScope validates a user-supplied --scope flag value.
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeUser, ScopeProject, ScopeLocal:
		return Scope(s), nil
	default:
		return "", fmt.Errorf("unknown scope %q (valid: user, project, local)", s)
	}
}

// AllScopes returns all three scopes in the canonical report order.
// `cairn hook status` with no --scope flag walks this list.
func AllScopes() []Scope {
	return []Scope{ScopeUser, ScopeProject, ScopeLocal}
}

// SettingsPath resolves the absolute settings.json path for scope.
//
// For user scope, claudeHome wins over the CLAUDE_HOME env, which wins
// over ~/.claude via os.UserHomeDir. Precedence mirrors the
// install_skills.go pattern so operators set CLAUDE_HOME once and all
// cairn commands agree on the install root.
//
// Project and local scopes require cwd; they resolve relative to it.
// Returned path's parent directory is not guaranteed to exist —
// LoadSettings and Save handle absent directories gracefully.
func SettingsPath(scope Scope, cwd, claudeHome string) (string, error) {
	switch scope {
	case ScopeUser:
		home := claudeHome
		if home == "" {
			home = os.Getenv("CLAUDE_HOME")
		}
		if home == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve user home: %w", err)
			}
			home = filepath.Join(h, ".claude")
		}
		return filepath.Join(home, "settings.json"), nil
	case ScopeProject:
		if cwd == "" {
			return "", fmt.Errorf("project scope requires cwd")
		}
		return filepath.Join(cwd, ".claude", "settings.json"), nil
	case ScopeLocal:
		if cwd == "" {
			return "", fmt.Errorf("local scope requires cwd")
		}
		return filepath.Join(cwd, ".claude", "settings.local.json"), nil
	default:
		return "", fmt.Errorf("unknown scope %q", scope)
	}
}
