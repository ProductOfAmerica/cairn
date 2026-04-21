package hook

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/intent"
)

// CheckDriftResult is the data payload emitted under the
// "hook.check_drift" envelope kind. Skipped=true means the hook
// short-circuited (no specs/ dir) and Warnings is always empty in
// that case.
type CheckDriftResult struct {
	Checked    int      `json:"checked"`
	Clean      bool     `json:"clean"`
	Warnings   []string `json:"warnings"`
	Skipped    bool     `json:"skipped,omitempty"`
	SkipReason string   `json:"skip_reason,omitempty"`
}

// CheckDrift walks <cwd>/specs/**/*.yaml and verifies each file's
// `# cairn-derived:` header against the current bytes of the prose
// source referenced by source-path. Warnings are appended for:
//
//   - YAML with no header
//   - YAML whose source-path file is missing
//   - YAML whose header hash doesn't match the current source bytes
//   - Unreadable YAML or per-file walk errors (reported, not fatal)
//
// Short-circuit: if <cwd>/specs is absent, returns
// {Skipped:true, SkipReason:"no_specs_dir", Clean:true}. Hook subcmds
// map this to exit 0 silent.
//
// Walk-level errors at the specs dir itself (e.g. permission denied)
// are returned as the error; the result is empty in that case.
func CheckDrift(cwd string) (CheckDriftResult, error) {
	res := CheckDriftResult{Warnings: []string{}}
	specsDir := filepath.Join(cwd, "specs")

	info, err := os.Stat(specsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			res.Skipped = true
			res.SkipReason = "no_specs_dir"
			res.Clean = true
			return res, nil
		}
		return res, fmt.Errorf("stat %s: %w", specsDir, err)
	}
	if !info.IsDir() {
		return res, fmt.Errorf("%s is not a directory", specsDir)
	}

	walkErr := filepath.WalkDir(specsDir, func(p string, d fs.DirEntry, we error) error {
		if we != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("walk error at %s: %v", relTo(cwd, p), we))
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return nil
		}
		res.Checked++

		body, rerr := os.ReadFile(p)
		if rerr != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("read %s: %v", relTo(cwd, p), rerr))
			return nil
		}
		h, perr := intent.ParseSourceHashHeader(body)
		if perr != nil {
			if errors.Is(perr, intent.ErrNoSourceHashHeader) {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("no source-hash header: %s", relTo(cwd, p)))
				return nil
			}
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("parse %s: %v", relTo(cwd, p), perr))
			return nil
		}

		// source-path in the header is repo-root-relative (POSIX slashes).
		// Resolve against cwd and convert to OS-native separators.
		sourceAbs := filepath.Join(cwd, filepath.FromSlash(h.SourcePath))

		got, cerr := intent.ComputeSourceHash(sourceAbs)
		if cerr != nil {
			if errors.Is(cerr, os.ErrNotExist) {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("source file missing: %s (referenced by %s)",
						h.SourcePath, relTo(cwd, p)))
				return nil
			}
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("hash source %s: %v", h.SourcePath, cerr))
			return nil
		}
		if got != h.SourceHash {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("drift: %s header=%s actual=%s source=%s",
					relTo(cwd, p), h.SourceHash, got, h.SourcePath))
		}
		return nil
	})
	if walkErr != nil {
		return res, fmt.Errorf("walk %s: %w", specsDir, walkErr)
	}
	res.Clean = len(res.Warnings) == 0
	return res, nil
}

// WriteDriftWarnings emits one human-readable line per warning to w,
// prefixed so operators can grep cairn's output easily in a busy CC
// stderr stream. Called by the cobra wrapper after CheckDrift returns.
func WriteDriftWarnings(w io.Writer, res CheckDriftResult) {
	for _, m := range res.Warnings {
		fmt.Fprintln(w, "cairn hook check-drift: "+m)
	}
}

// relTo returns base-relative POSIX path for log lines. Falls back to
// the raw path (posix-slashed) if filepath.Rel fails.
func relTo(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}
