package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

const (
	// CairnConfigVersion is the current schema of the `cairn` block
	// inside CC settings.json. Bumped only for breaking format changes.
	CairnConfigVersion = 1

	// CairnHookCommandPrefix identifies cairn-owned hook entries for
	// idempotent add/remove. Entries whose `command` begins with this
	// string are cairn's (e.g. "cairn hook check-drift"). Cairn owns
	// the `cairn hook check-*` command namespace by contract; operators
	// who paste their own `cairn hook check-foo` hook entry will be
	// swept on disable. That is the documented trade-off vs a custom
	// marker field — CC docs don't guarantee unknown-field preservation.
	CairnHookCommandPrefix = "cairn hook check-"

	// CairnHookDriftCommand is the canonical command string cairn
	// writes into settings.json when enabling the Stop drift hook.
	CairnHookDriftCommand = "cairn hook check-drift"
)

// CairnConfig is the `cairn` block inside settings.json.
//
// Present means the operator has run `cairn hook enable` at least once
// at this scope; Enabled=false means they have since run disable.
// Version lets future cairn migrate the format explicitly — unknown
// versions are rejected rather than silently guessed.
type CairnConfig struct {
	Version int  `json:"version"`
	Enabled bool `json:"enabled"`
}

// Settings is an in-memory edit view of a CC settings.json file. It
// preserves the original top-level key order, and for every top-level
// key other than "hooks" and "cairn", the original raw JSON value
// passes through byte-identical on Save. Inside "hooks" we decode
// structurally; the decode→re-encode loses nested key ordering (Go's
// map encoder emits keys alphabetically) but operator entries survive
// semantically.
//
// Absent file → empty Settings. Malformed JSON → cairnerr with kind
// hook_settings_parse_failed.
type Settings struct {
	path string
	raw  []ordKV // top-level entries in source order, raw values

	hooks        map[string][]map[string]any // nil when no hooks key
	cairn        CairnConfig
	cairnPresent bool
}

type ordKV struct {
	key string
	raw json.RawMessage
}

// LoadSettings reads path, decodes the top-level object preserving key
// order, and parses the hooks and cairn subtrees into structured form.
// An absent file returns an empty Settings ready for Save.
func LoadSettings(path string) (*Settings, error) {
	s := &Settings{path: path, hooks: map[string][]map[string]any{}}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, cairnerr.New(cairnerr.CodeSubstrate, "hook_settings_parse_failed",
			fmt.Sprintf("read %s", path)).WithCause(err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return s, nil
	}

	// Decode top level preserving key order via the streaming decoder.
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return nil, cairnerr.New(cairnerr.CodeBadInput, "hook_settings_parse_failed",
			"top-level read").WithCause(err)
	}
	d, ok := tok.(json.Delim)
	if !ok || d != '{' {
		return nil, cairnerr.New(cairnerr.CodeBadInput, "hook_settings_parse_failed",
			"top level is not a JSON object")
	}
	for dec.More() {
		kTok, err := dec.Token()
		if err != nil {
			return nil, cairnerr.New(cairnerr.CodeBadInput, "hook_settings_parse_failed",
				"key token").WithCause(err)
		}
		k, ok := kTok.(string)
		if !ok {
			return nil, cairnerr.New(cairnerr.CodeBadInput, "hook_settings_parse_failed",
				"non-string key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, cairnerr.New(cairnerr.CodeBadInput, "hook_settings_parse_failed",
				fmt.Sprintf("decode value for %q", k)).WithCause(err)
		}
		s.raw = append(s.raw, ordKV{k, raw})

		switch k {
		case "hooks":
			if err := json.Unmarshal(raw, &s.hooks); err != nil {
				return nil, cairnerr.New(cairnerr.CodeBadInput, "hook_settings_parse_failed",
					"hooks subtree").WithCause(err)
			}
			if s.hooks == nil {
				s.hooks = map[string][]map[string]any{}
			}
		case "cairn":
			var c CairnConfig
			if err := json.Unmarshal(raw, &c); err != nil {
				return nil, cairnerr.New(cairnerr.CodeBadInput, "hook_settings_parse_failed",
					"cairn subtree").WithCause(err)
			}
			if c.Version != 0 && c.Version != CairnConfigVersion {
				return nil, cairnerr.New(cairnerr.CodeBadInput, "hook_settings_parse_failed",
					fmt.Sprintf("cairn.version %d not supported (this cairn understands %d)", c.Version, CairnConfigVersion))
			}
			s.cairn = c
			s.cairnPresent = true
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, cairnerr.New(cairnerr.CodeBadInput, "hook_settings_parse_failed",
			"closing brace").WithCause(err)
	}
	return s, nil
}

// Cairn returns the parsed cairn block. Second return indicates whether
// the block is present in settings.json (false means never enabled).
func (s *Settings) Cairn() (CairnConfig, bool) {
	return s.cairn, s.cairnPresent
}

// CairnEntryCount counts hook entries whose command begins with
// CairnHookCommandPrefix across all event kinds. Used by `hook status`
// to report presence + drift vs the cairn.enabled flag.
func (s *Settings) CairnEntryCount() int {
	n := 0
	for _, entries := range s.hooks {
		for _, entry := range entries {
			rawHooks, ok := entry["hooks"].([]any)
			if !ok {
				continue
			}
			for _, h := range rawHooks {
				hm, ok := h.(map[string]any)
				if !ok {
					continue
				}
				cmd, _ := hm["command"].(string)
				if strings.HasPrefix(cmd, CairnHookCommandPrefix) {
					n++
				}
			}
		}
	}
	return n
}

// AddCairnHook installs the `cairn hook check-drift` Stop entry plus a
// versioned cairn config block with enabled=true. Idempotent: if the
// entry is already present, the method leaves it alone.
func (s *Settings) AddCairnHook() {
	s.cairn = CairnConfig{Version: CairnConfigVersion, Enabled: true}
	s.cairnPresent = true

	// Ensure Stop bucket exists.
	stopEntries := s.hooks["Stop"]

	// If any existing Stop entry already contains our cairn drift
	// handler, leave settings untouched (idempotent).
	for _, entry := range stopEntries {
		rawHooks, ok := entry["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range rawHooks {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := hm["command"].(string); cmd == CairnHookDriftCommand {
				return
			}
		}
	}

	// Append a cairn-owned Stop entry. Kept structurally identical to
	// CC's documented shape: { matcher, hooks: [ { type, command } ] }.
	cairnEntry := map[string]any{
		"matcher": ".*",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": CairnHookDriftCommand,
			},
		},
	}
	s.hooks["Stop"] = append(stopEntries, cairnEntry)
}

// RemoveCairnHooks strips every hook handler whose command starts with
// CairnHookCommandPrefix, across all event kinds. If an entry's hooks
// list empties as a result, the entry itself is dropped. If an event's
// entries list empties, the event key is removed.
//
// Flips cairn.enabled=false if the cairn block is present so a
// subsequent `hook status` can distinguish "never enabled" from
// "disabled". Idempotent: re-running is a no-op.
func (s *Settings) RemoveCairnHooks() {
	if s.cairnPresent {
		s.cairn.Enabled = false
	}
	for event, entries := range s.hooks {
		newEntries := make([]map[string]any, 0, len(entries))
		for _, entry := range entries {
			rawHooks, ok := entry["hooks"].([]any)
			if !ok {
				newEntries = append(newEntries, entry)
				continue
			}
			kept := make([]any, 0, len(rawHooks))
			for _, h := range rawHooks {
				hm, ok := h.(map[string]any)
				if !ok {
					kept = append(kept, h)
					continue
				}
				cmd, _ := hm["command"].(string)
				if strings.HasPrefix(cmd, CairnHookCommandPrefix) {
					continue // drop cairn-owned handler
				}
				kept = append(kept, h)
			}
			if len(kept) == 0 {
				continue // entire entry emptied — drop it
			}
			entry["hooks"] = kept
			newEntries = append(newEntries, entry)
		}
		if len(newEntries) == 0 {
			delete(s.hooks, event)
			continue
		}
		s.hooks[event] = newEntries
	}
}

// Save writes the current Settings to disk via atomic rename. Preserves
// top-level key ordering and byte-identical passthrough for keys other
// than "hooks" and "cairn". Creates the file and parent directory if
// absent. Caller is responsible for holding an advisory FileLock when
// concurrent cairn writers are possible.
func (s *Settings) Save() error {
	// Re-encode only the two keys cairn owns. Other keys keep their
	// original raw JSON bytes.
	hooksRaw, err := marshalCanonical(s.hooks)
	if err != nil {
		return fmt.Errorf("marshal hooks: %w", err)
	}
	cairnRaw, err := marshalCanonical(s.cairn)
	if err != nil {
		return fmt.Errorf("marshal cairn: %w", err)
	}

	// Build the output key list, preserving source order where possible
	// and appending new ones at the end.
	out := make([]ordKV, 0, len(s.raw)+2)
	haveHooks, haveCairn := false, false
	for _, kv := range s.raw {
		switch kv.key {
		case "hooks":
			if len(s.hooks) == 0 {
				// Drop empty hooks subtree rather than emit "hooks":{}.
				haveHooks = true
				continue
			}
			out = append(out, ordKV{"hooks", hooksRaw})
			haveHooks = true
		case "cairn":
			if !s.cairnPresent {
				haveCairn = true
				continue
			}
			out = append(out, ordKV{"cairn", cairnRaw})
			haveCairn = true
		default:
			out = append(out, kv)
		}
	}
	if !haveHooks && len(s.hooks) > 0 {
		out = append(out, ordKV{"hooks", hooksRaw})
	}
	if !haveCairn && s.cairnPresent {
		out = append(out, ordKV{"cairn", cairnRaw})
	}

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, kv := range out {
		buf.WriteString("  ")
		kBytes, _ := json.Marshal(kv.key)
		buf.Write(kBytes)
		buf.WriteString(": ")
		buf.Write(indentRaw(kv.raw, "  "))
		if i != len(out)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")

	// Ensure parent dir exists.
	if err := os.MkdirAll(dirOf(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir parent of %s: %w", s.path, err)
	}

	// Atomic write via rename.
	tmp := s.path + ".cairn-tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, s.path, err)
	}
	return nil
}

// marshalCanonical returns v as indented JSON (2-space). Used for the
// two cairn-owned keys inside settings.json so the output is readable.
func marshalCanonical(v any) ([]byte, error) {
	return json.MarshalIndent(v, "  ", "  ")
}

// indentRaw returns the bytes of raw, with newlines indented by prefix
// for consistency with the handcrafted two-space layout above. If raw
// is already indented, this is idempotent enough — the goal is
// readability, not byte-exact round-trip (which is impossible after a
// subtree edit in any case).
func indentRaw(raw json.RawMessage, prefix string) []byte {
	// Compact then re-indent to normalize any authorial whitespace.
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return raw
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact.Bytes(), prefix, "  "); err != nil {
		return raw
	}
	return out.Bytes()
}

// dirOf returns filepath.Dir but tolerates empty input.
func dirOf(p string) string {
	// strings and filepath both live in stdlib; keep this inline to
	// avoid an import cycle during internal testing.
	last := strings.LastIndexAny(p, `/\`)
	if last < 0 {
		return "."
	}
	return p[:last]
}
