package integration_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

// copyFixture copies every file under testdata/e2e/<fixtureName>/ into dst,
// preserving relative paths. The test's tempdir is the typical dst.
func copyFixture(t *testing.T, dst, fixtureName string) {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "e2e", fixtureName)
	// The test runs from internal/integration; the testdata root lives two
	// levels up in the repo root.
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fixture %q not found: %v", fixtureName, err)
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copyFixture %q: %v", fixtureName, err)
	}
}

// runCairnExit is runCairn plus an exit-code assertion. Useful when the
// test cares both about the exit code and the envelope payload.
func runCairnExit(t *testing.T, dir, cairnHome string, expectedExit int, args ...string) map[string]any {
	t.Helper()
	env, code := runCairn(t, dir, cairnHome, args...)
	if code != expectedExit {
		t.Fatalf("cairn %v: exit=%d want %d\nenv=%+v", args, code, expectedExit, env)
	}
	return env
}

// expectEnvelopeKind fails the test if env.kind != want.
func expectEnvelopeKind(t *testing.T, env map[string]any, want string) {
	t.Helper()
	got, _ := env["kind"].(string)
	if got != want {
		t.Fatalf("envelope kind=%q want %q", got, want)
	}
}

// expectErrorKind fails the test if env.error.code != want.
func expectErrorKind(t *testing.T, env map[string]any, want string) {
	t.Helper()
	e, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("envelope has no error, got %+v", env)
	}
	got, _ := e["code"].(string)
	if got != want {
		t.Fatalf("error.code=%q want %q", got, want)
	}
}

// stringsContainsAll returns true iff every substring is present in s.
// Not currently used but kept for future scenario diagnostics.
func stringsContainsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// mutateOneCell directly UPDATEs a single cell of the state DB. Used by
// e2e tests to inject substrate-level corruption that no CLI surface can
// reach (cairn never writes invalid JSON itself, but a hand-edit, partial
// restore, or future migration regression could produce one). Asserts the
// UPDATE changed exactly one row so the test fails loudly if the fixture
// drifts and the WHERE clause stops matching.
//
// The table, column, idCol parameters are interpolated via fmt.Sprintf and
// are SAFE to concatenate ONLY because every callsite passes literal Go
// string constants — no test input ever flows here. Do NOT call this with
// values derived from CLI output, files on disk, or other dynamic sources.
func mutateOneCell(t *testing.T, repoDir, cairnHome, table, column string, value any, idCol, idVal string) {
	t.Helper()
	dbPath := resolveStateDBPath(t, repoDir, cairnHome)
	h, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open %q: %v", dbPath, err)
	}
	defer h.Close()
	stmt := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?", table, column, idCol)
	res, err := h.SQL().Exec(stmt, value, idVal)
	if err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("rows affected: %v", err)
	}
	if n != 1 {
		t.Fatalf("mutateOneCell: expected 1 row updated, got %d (table=%s %s=%q)",
			n, table, idCol, idVal)
	}
}

// mustEmptyRepo creates a throwaway git repo with no spec files. Callers
// supply the spec themselves (typically via copyFixture).
func mustEmptyRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	run := func(args ...string) {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = d
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-q")
	run("git", "commit", "--allow-empty", "-q", "-m", "bootstrap")
	return d
}
