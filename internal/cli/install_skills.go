package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/skills"
)

// InstallSkillsResult reports what "cairn setup" did to the project's
// .claude/skills/ tree.
type InstallSkillsResult struct {
	Root          string   `json:"root"`
	Written       []string `json:"written"`
	Skipped       []string `json:"skipped"`
	Force         bool     `json:"force"`
	SkippedReason string   `json:"skipped_reason,omitempty"`
	LocalShadow   []string `json:"local_shadow,omitempty"`
}

// InstallSkills copies cairn's embedded skill files into
// <cwd>/.claude/skills/<skill>/. Claude Code auto-discovers skills
// under this path without requiring /plugin install.
//
// Behavior:
//   - If the cairn plugin is already cached in Claude Code's global plugin
//     cache (see globalCairnPluginCached), the install is skipped entirely
//     as redundant. force=true bypasses this check. SkippedReason and
//     LocalShadow on the result describe the skip and any pre-existing
//     local shadow copies operators should remove.
//   - Otherwise, files are copied from the embedded FS to <cwd>/.claude/skills/.
//   - If a destination file already exists, it's skipped unless force=true.
//   - Parent directories are created as needed.
//   - force=true overwrites every file regardless.
//
// claudeHome overrides the Claude Code config directory used for global
// plugin detection. Resolution order: explicit arg > CLAUDE_HOME env >
// ~/.claude/ via os.UserHomeDir. Matches the precedence pattern used by
// ResolveStateRoot for cairn's own state dir.
//
// Returns lists of written vs. skipped relative paths (always posix
// slashes for cross-platform stability in JSON consumers).
func InstallSkills(cwd string, force bool, claudeHome string) (*InstallSkillsResult, error) {
	root := filepath.Join(cwd, ".claude", "skills")
	result := &InstallSkillsResult{
		Root:    filepath.ToSlash(root),
		Written: []string{},
		Skipped: []string{},
		Force:   force,
	}

	if claudeHome == "" {
		if env := os.Getenv("CLAUDE_HOME"); env != "" {
			claudeHome = env
		} else if home, err := os.UserHomeDir(); err == nil {
			claudeHome = filepath.Join(home, ".claude")
		}
	}

	if !force && globalCairnPluginCached(claudeHome) {
		// Enumerate embedded files as "skipped"; list any pre-existing
		// local copies as shadow so printSetupHints can warn the operator.
		_ = fs.WalkDir(skills.FS, ".", func(relPath string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || relPath == "." || d.IsDir() || relPath == "embed.go" {
				return nil
			}
			posix := filepath.ToSlash(relPath)
			result.Skipped = append(result.Skipped, posix)
			if _, statErr := os.Stat(filepath.Join(root, relPath)); statErr == nil {
				result.LocalShadow = append(result.LocalShadow, posix)
			}
			return nil
		})
		result.SkippedReason = "global_plugin_detected"
		return result, nil
	}

	err := fs.WalkDir(skills.FS, ".", func(relPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if relPath == "." || d.IsDir() {
			return nil
		}
		// Skip the embed.go declaration — it's source, not a skill file.
		if relPath == "embed.go" {
			return nil
		}

		dst := filepath.Join(root, relPath)
		posix := filepath.ToSlash(relPath)

		if _, statErr := os.Stat(dst); statErr == nil && !force {
			result.Skipped = append(result.Skipped, posix)
			return nil
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", filepath.Dir(dst), err)
		}
		data, err := skills.FS.ReadFile(relPath)
		if err != nil {
			return fmt.Errorf("read embedded %q: %w", relPath, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("write %q: %w", dst, err)
		}
		result.Written = append(result.Written, posix)
		return nil
	})
	if err != nil {
		return nil, cairnerr.New(cairnerr.CodeSubstrate, "install_skills_failed", err.Error()).WithCause(err)
	}
	return result, nil
}

// globalCairnPluginCached reports whether Claude Code has the cairn
// plugin cached in its global plugin cache. The cache layout is
// documented at https://code.claude.com/docs/en/plugins-reference
// under "Plugin caching and file resolution":
//
//	<claude-home>/plugins/cache/<marketplace>/<plugin>/<version>/.claude-plugin/plugin.json
//
// Cairn's marketplace name and plugin name are both "cairn" (see
// .claude-plugin/marketplace.json and .claude-plugin/plugin.json at
// the repo root).
//
// Returns false when claudeHome is empty or on any glob error — a
// negative detection safely falls through to normal install.
func globalCairnPluginCached(claudeHome string) bool {
	if claudeHome == "" {
		return false
	}
	pattern := filepath.Join(claudeHome, "plugins", "cache", "cairn", "cairn", "*", ".claude-plugin", "plugin.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return false
	}
	return len(matches) > 0
}
