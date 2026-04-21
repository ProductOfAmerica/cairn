package hook

import (
	"errors"
	"os"
)

// ScopeStatus reports the per-scope state read from a settings.json.
type ScopeStatus struct {
	Scope             string `json:"scope"`
	SettingsPath      string `json:"settings_path"`
	Exists            bool   `json:"exists"`
	CairnBlockPresent bool   `json:"cairn_block_present"`
	Version           int    `json:"version,omitempty"`
	Enabled           bool   `json:"enabled"`
	EntryCount        int    `json:"entry_count"`
	// Drift is non-empty when the config block says one thing but the
	// hook entries contradict it (e.g. enabled=false but entries still
	// present, or entries present without a cairn config block at all).
	Drift string `json:"drift,omitempty"`
}

// StatusResult is the envelope data payload for `cairn hook status`.
type StatusResult struct {
	Scopes []ScopeStatus `json:"scopes"`
}

// Status reads each requested scope's settings.json (or all three if
// scopes is empty) and returns a per-scope summary. Read-only — never
// takes the advisory lock, never writes. A non-existent settings.json
// reports Exists=false without error.
//
// A malformed settings.json at any scope is reported as a drift entry
// ("parse error: ...") rather than aborting the whole status query —
// operators usually want to see the OTHER scopes even if one is broken.
func Status(scopes []Scope, cwd, claudeHome string) (StatusResult, error) {
	if len(scopes) == 0 {
		scopes = AllScopes()
	}
	out := StatusResult{Scopes: make([]ScopeStatus, 0, len(scopes))}
	for _, sc := range scopes {
		st := ScopeStatus{Scope: string(sc)}
		path, err := SettingsPath(sc, cwd, claudeHome)
		if err != nil {
			st.Drift = "path resolve: " + err.Error()
			out.Scopes = append(out.Scopes, st)
			continue
		}
		st.SettingsPath = path

		if _, statErr := os.Stat(path); statErr != nil {
			if !errors.Is(statErr, os.ErrNotExist) {
				st.Drift = "stat: " + statErr.Error()
			}
			out.Scopes = append(out.Scopes, st)
			continue
		}
		st.Exists = true

		s, lerr := LoadSettings(path)
		if lerr != nil {
			st.Drift = "parse: " + lerr.Error()
			out.Scopes = append(out.Scopes, st)
			continue
		}

		cairn, present := s.Cairn()
		st.CairnBlockPresent = present
		st.Version = cairn.Version
		st.Enabled = cairn.Enabled
		st.EntryCount = s.CairnEntryCount()

		switch {
		case st.EntryCount > 0 && !present:
			st.Drift = "entries present but no cairn config block (partial install)"
		case st.EntryCount > 0 && !cairn.Enabled:
			st.Drift = "entries present but cairn.enabled=false (operator-caused drift; run `cairn hook disable` to reconcile)"
		case st.EntryCount == 0 && cairn.Enabled:
			st.Drift = "cairn.enabled=true but no entries present (run `cairn hook enable` to reconcile)"
		}
		out.Scopes = append(out.Scopes, st)
	}
	return out, nil
}
