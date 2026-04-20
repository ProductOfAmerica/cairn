package cli

import (
	"os"
	"path/filepath"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/repoid"
)

// BootstrapResult is the shape returned by Bootstrap. Shared by
// "cairn init" (envelope kind "init") and "cairn setup" (envelope
// kind "setup") so the two commands report identical state.
type BootstrapResult struct {
	RepoID   string `json:"repo_id"`
	StateDir string `json:"state_dir"`
	DBPath   string `json:"db_path"`
}

// Bootstrap resolves repo identity from cwd, creates the state dir +
// blob tree, and ensures the state DB exists (running migrations on
// first open). Idempotent: re-running leaves the filesystem unchanged.
//
// stateRootOverride is the effective value of --state-root (empty
// string means "use CAIRN_HOME / platform default").
func Bootstrap(cwd, stateRootOverride string) (*BootstrapResult, error) {
	id, err := repoid.Resolve(cwd)
	if err != nil {
		return nil, cairnerr.New(cairnerr.CodeBadInput, "not_a_git_repo", err.Error()).WithCause(err)
	}
	stateRoot := ResolveStateRoot(stateRootOverride)
	stateDir := filepath.Join(stateRoot, id)
	if err := os.MkdirAll(filepath.Join(stateDir, "blobs"), 0o700); err != nil {
		return nil, cairnerr.New(cairnerr.CodeSubstrate, "mkdir_failed", err.Error()).WithCause(err)
	}
	dbPath := filepath.Join(stateDir, "state.db")
	h, err := db.Open(dbPath)
	if err != nil {
		return nil, cairnerr.New(cairnerr.CodeSubstrate, "db_open_failed", err.Error()).WithCause(err)
	}
	_ = h.Close()
	return &BootstrapResult{
		RepoID:   id,
		StateDir: filepath.ToSlash(stateDir),
		DBPath:   filepath.ToSlash(dbPath),
	}, nil
}
