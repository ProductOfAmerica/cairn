package hook

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ProductOfAmerica/cairn/internal/repoid"
)

// IsCairnTracked reports whether cwd looks like a cairn-tracked repo.
// A repo qualifies if either:
//   - <cwd>/specs exists as a directory (spec pipeline is set up), OR
//   - <stateRoot>/<repo-id>/state.db exists (cairn state was created).
//
// When neither condition holds, hook runtime subcmds should exit 0
// silently — cairn shouldn't spam operators running Claude Code in
// repos that don't use cairn. Callers that want to distinguish the two
// positive cases can stat them directly.
//
// A repoid.Resolve failure (e.g. cwd is not inside a git repo) is
// treated as "not cairn-tracked": the second branch short-circuits to
// false.
func IsCairnTracked(cwd, stateRoot string) bool {
	if info, err := os.Stat(filepath.Join(cwd, "specs")); err == nil && info.IsDir() {
		return true
	}
	id, err := repoid.Resolve(cwd)
	if err != nil {
		return false
	}
	db := filepath.Join(stateRoot, id, "state.db")
	if info, err := os.Stat(db); err == nil && !info.IsDir() {
		return true
	}
	return false
}

// GuardPanic runs fn and converts any panic into a stderr diagnostic
// line plus a return value signaling the caller to os.Exit(1). Hook
// subcommands call this to satisfy the "never exit >1 on panic"
// guarantee — Go panics would otherwise abort with exit code 2 (or
// higher on some signals), which CC might misinterpret as a blocking
// hook response.
//
// Usage pattern (cobra RunE):
//
//	RunE: func(cmd *cobra.Command, args []string) error {
//	    if hook.GuardPanic(func() { runImpl(cmd, args) }) {
//	        os.Exit(1)
//	    }
//	    return nil
//	}
func GuardPanic(fn func()) (recovered bool) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "cairn hook: panic recovered: %v\n", r)
			recovered = true
		}
	}()
	fn()
	return false
}
