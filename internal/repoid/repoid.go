// Package repoid resolves a stable identifier for a git repository.
//
// The repo-id is:
//   sha256(canonical absolute path of `git rev-parse --git-common-dir`)
//
// Pipeline:
//   1. Run `git rev-parse --git-common-dir` from the repo working dir.
//   2. filepath.Abs on the result (handles relative output under GIT_DIR env).
//   3. filepath.EvalSymlinks (resolves symlinks; on Windows resolves 8.3 short
//      names and directory junctions into long canonical paths).
//   4. On Windows: lowercase the drive letter.
//   5. Normalize separators to forward slash.
//   6. sha256 of the UTF-8 bytes, hex-encoded lowercase.
//
// Worktrees of the same repo resolve to the same id because `--git-common-dir`
// always points to the primary repo's `.git` directory, not the per-worktree
// `.git/worktrees/<name>/`.
package repoid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Resolve computes the repo-id for the repository containing cwd.
// Returns an error if cwd is not inside a git repository or if git is missing.
func Resolve(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = cwd
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "(no stderr)"
		}
		return "", fmt.Errorf("git rev-parse --git-common-dir in %q: %w: %s", cwd, err, msg)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return "", fmt.Errorf("git rev-parse --git-common-dir returned empty")
	}

	// Git outputs paths relative to cmd.Dir (i.e., cwd). filepath.Abs uses the
	// process's working directory as its anchor, which may differ from cwd.
	// Explicitly joining with cwd first ensures the library is independent of
	// the caller's os.Getwd().
	var absCandidate string
	if filepath.IsAbs(raw) {
		absCandidate = raw
	} else {
		absCandidate = filepath.Join(cwd, raw)
	}
	abs, err := filepath.Abs(absCandidate)
	if err != nil {
		return "", fmt.Errorf("abs(%q): %w", absCandidate, err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("evalsymlinks(%q): %w", abs, err)
	}

	canon := resolved
	if runtime.GOOS == "windows" && len(canon) >= 2 && canon[1] == ':' {
		canon = strings.ToLower(canon[:1]) + canon[1:]
	}

	canon = filepath.ToSlash(canon)

	sum := sha256.Sum256([]byte(canon))
	return hex.EncodeToString(sum[:]), nil
}
