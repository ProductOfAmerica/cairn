package repoid_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/repoid"
)

func mustGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func setupRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	mustGit(t, "-C", d, "init", "-q")
	return d
}

func TestResolve_SameRepoSameID(t *testing.T) {
	d := setupRepo(t)
	a, err := repoid.Resolve(d)
	if err != nil {
		t.Fatal(err)
	}
	b, err := repoid.Resolve(d)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("unstable repo id: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("expected 64-char hex sha256, got %d (%s)", len(a), a)
	}
}

func TestResolve_DifferentReposDifferentIDs(t *testing.T) {
	a, err := repoid.Resolve(setupRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := repoid.Resolve(setupRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("distinct repos produced same id: %s", a)
	}
}

func TestResolve_SubdirYieldsSameID(t *testing.T) {
	d := setupRepo(t)
	sub := filepath.Join(d, "pkg", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := repoid.Resolve(d)
	if err != nil {
		t.Fatal(err)
	}
	b, err := repoid.Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("subdir yields different id: %s vs %s", a, b)
	}
}

func TestResolve_NotAGitRepo(t *testing.T) {
	d := t.TempDir()
	_, err := repoid.Resolve(d)
	if err == nil {
		t.Fatal("expected error for non-repo")
	}
	if !strings.Contains(err.Error(), "git") {
		t.Fatalf("error should mention git, got: %v", err)
	}
}

func TestResolve_WindowsDriveCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	d := setupRepo(t)
	upper, err := repoid.Resolve(d)
	if err != nil {
		t.Fatal(err)
	}
	alt := swapDriveCase(d)
	if alt == d {
		t.Skip("could not flip drive case for path")
	}
	lower, err := repoid.Resolve(alt)
	if err != nil {
		t.Fatal(err)
	}
	if upper != lower {
		t.Fatalf("drive case affects id: %s vs %s", upper, lower)
	}
}

func swapDriveCase(p string) string {
	if len(p) < 2 || p[1] != ':' {
		return p
	}
	c := p[0]
	if c >= 'A' && c <= 'Z' {
		return string(c+('a'-'A')) + p[1:]
	}
	if c >= 'a' && c <= 'z' {
		return string(c-('a'-'A')) + p[1:]
	}
	return p
}

func TestResolve_WorktreeSharesID(t *testing.T) {
	d := setupRepo(t)
	// Need at least one commit before `git worktree add`.
	if err := os.WriteFile(filepath.Join(d, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, "-C", d, "add", "f")
	mustGit(t, "-C", d, "-c", "user.email=t@t", "-c", "user.name=t",
		"commit", "-q", "-m", "c1")
	wt := filepath.Join(t.TempDir(), "wt")
	mustGit(t, "-C", d, "worktree", "add", "-q", wt)

	mainID, err := repoid.Resolve(d)
	if err != nil {
		t.Fatal(err)
	}
	wtID, err := repoid.Resolve(wt)
	if err != nil {
		t.Fatal(err)
	}
	if mainID != wtID {
		t.Fatalf("worktree id drifted: main=%s wt=%s", mainID, wtID)
	}
}

func TestResolve_BareRepo(t *testing.T) {
	d := t.TempDir()
	mustGit(t, "init", "--bare", "-q", d)
	_, err := repoid.Resolve(d)
	if err != nil {
		t.Fatalf("bare repo should resolve, got: %v", err)
	}
}

func TestResolve_GitDirEnvRelative(t *testing.T) {
	// A shell invocation of `git rev-parse --git-common-dir` with GIT_DIR set
	// to a relative path can produce relative output. Verify filepath.Abs
	// promotes that to absolute before hashing.
	d := setupRepo(t)
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(d); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", ".git")
	id, err := repoid.Resolve(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 64 {
		t.Fatalf("bad id: %s", id)
	}
}

func TestResolve_Symlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require admin on windows")
	}
	d := setupRepo(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(d, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	viaReal, err := repoid.Resolve(d)
	if err != nil {
		t.Fatal(err)
	}
	viaLink, err := repoid.Resolve(link)
	if err != nil {
		t.Fatal(err)
	}
	if viaReal != viaLink {
		t.Fatalf("symlink changes id: real=%s link=%s", viaReal, viaLink)
	}
}
