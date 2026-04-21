package hook

import (
	"fmt"
	"os/exec"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// EnableResult is the envelope data payload for `cairn hook enable`.
// AlreadyEnabled is true when the scope was already fully set up; the
// save still runs but the settings.json content is byte-identical.
//
// BinaryOnPath is informational — cairn doesn't refuse to enable just
// because its own binary can't be found on PATH (operators may install
// cairn after enabling hooks). BinaryWarning carries the reason when
// BinaryOnPath is false so `hook status` / CI can flag it.
type EnableResult struct {
	Scope          string `json:"scope"`
	SettingsPath   string `json:"settings_path"`
	Enabled        bool   `json:"enabled"`
	AlreadyEnabled bool   `json:"already_enabled"`
	BinaryOnPath   bool   `json:"binary_on_path"`
	BinaryPathHint string `json:"binary_path_hint,omitempty"`
	BinaryWarning  string `json:"binary_warning,omitempty"`
}

// Enable installs the cairn Stop hook + cairn config block into the
// scope's settings.json. Holds the advisory file lock across
// load/modify/save so parallel invocations don't clobber each other.
//
// Idempotent: re-running against an already-enabled scope produces
// AlreadyEnabled=true and leaves the settings.json byte-identical.
//
// Returns cairnerr-kinded errors:
//   - hook_config_locked  (lock timeout)
//   - hook_settings_parse_failed (from LoadSettings on malformed input)
func Enable(scope Scope, cwd, claudeHome string) (EnableResult, error) {
	path, err := SettingsPath(scope, cwd, claudeHome)
	if err != nil {
		return EnableResult{}, cairnerr.New(cairnerr.CodeBadInput, "hook_bad_scope",
			err.Error())
	}

	lock, err := AcquireLock(path, 0)
	if err != nil {
		return EnableResult{}, cairnerr.New(cairnerr.CodeConflict, "hook_config_locked",
			"settings.json lock held by another process").WithCause(err)
	}
	defer func() { _ = lock.Release() }()

	s, err := LoadSettings(path)
	if err != nil {
		return EnableResult{}, err
	}

	before := s.CairnEntryCount()
	s.AddCairnHook()
	after := s.CairnEntryCount()

	if err := s.Save(); err != nil {
		return EnableResult{}, cairnerr.New(cairnerr.CodeSubstrate, "hook_settings_write_failed",
			"save "+path).WithCause(err)
	}

	res := EnableResult{
		Scope:          string(scope),
		SettingsPath:   path,
		Enabled:        true,
		AlreadyEnabled: before == after && before > 0,
	}
	if p, lerr := exec.LookPath("cairn"); lerr == nil {
		res.BinaryOnPath = true
		res.BinaryPathHint = p
	} else {
		res.BinaryWarning = fmt.Sprintf("cairn not on PATH (%v) — CC will fail the hook until cairn is installed", lerr)
	}
	return res, nil
}
