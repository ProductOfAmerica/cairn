package hook

import (
	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// DisableResult is the envelope data payload for `cairn hook disable`.
type DisableResult struct {
	Scope          string `json:"scope"`
	SettingsPath   string `json:"settings_path"`
	Disabled       bool   `json:"disabled"`
	AlreadyDisabled bool  `json:"already_disabled"`
	EntriesRemoved int    `json:"entries_removed"`
}

// Disable strips every cairn-owned hook entry (command starts with
// CairnHookCommandPrefix) from the scope's settings.json and flips
// cairn.enabled=false if the block exists. Idempotent.
//
// Preserves operator-owned entries in the same event bucket. If
// settings.json is absent or never had cairn entries, Disable returns
// AlreadyDisabled=true without creating a file.
func Disable(scope Scope, cwd, claudeHome string) (DisableResult, error) {
	path, err := SettingsPath(scope, cwd, claudeHome)
	if err != nil {
		return DisableResult{}, cairnerr.New(cairnerr.CodeBadInput, "hook_bad_scope",
			err.Error())
	}

	lock, err := AcquireLock(path, 0)
	if err != nil {
		return DisableResult{}, cairnerr.New(cairnerr.CodeConflict, "hook_config_locked",
			"settings.json lock held by another process").WithCause(err)
	}
	defer func() { _ = lock.Release() }()

	s, err := LoadSettings(path)
	if err != nil {
		return DisableResult{}, err
	}
	before := s.CairnEntryCount()
	_, hadCairnBlock := s.Cairn()

	if before == 0 && !hadCairnBlock {
		// Nothing to disable. Skip the save so we don't create an empty
		// settings.json file as a side effect.
		return DisableResult{
			Scope:           string(scope),
			SettingsPath:    path,
			Disabled:        false,
			AlreadyDisabled: true,
		}, nil
	}

	s.RemoveCairnHooks()
	if err := s.Save(); err != nil {
		return DisableResult{}, cairnerr.New(cairnerr.CodeSubstrate, "hook_settings_write_failed",
			"save "+path).WithCause(err)
	}
	return DisableResult{
		Scope:          string(scope),
		SettingsPath:   path,
		Disabled:       true,
		EntriesRemoved: before,
	}, nil
}
