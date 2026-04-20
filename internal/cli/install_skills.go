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
	Root     string   `json:"root"`
	Written  []string `json:"written"`
	Skipped  []string `json:"skipped"`
	Force    bool     `json:"force"`
}

// InstallSkills copies cairn's embedded skill files into
// <cwd>/.claude/skills/<skill>/. Claude Code auto-discovers skills
// under this path without requiring /plugin install.
//
// Behavior:
//   - If a destination file already exists, it's skipped unless force=true.
//   - Parent directories are created as needed.
//   - force=true overwrites every file regardless.
//
// Returns lists of written vs. skipped relative paths (always posix
// slashes for cross-platform stability in JSON consumers).
func InstallSkills(cwd string, force bool) (*InstallSkillsResult, error) {
	root := filepath.Join(cwd, ".claude", "skills")
	result := &InstallSkillsResult{
		Root:    filepath.ToSlash(root),
		Written: []string{},
		Skipped: []string{},
		Force:   force,
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
