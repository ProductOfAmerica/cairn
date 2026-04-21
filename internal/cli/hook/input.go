// Package hook implements the `cairn hook` subcommand tree: a
// Claude Code Stop hook runtime (check-drift) plus operator CLI
// (enable, disable, status) for installing/removing cairn hook
// entries in CC settings.json.
//
// The package is internal so the public cairn module doesn't grow a
// new API surface; cmd/cairn/hook.go is the only consumer.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
)

// Input is the JSON payload Claude Code passes to hook subcommands on
// stdin. Shape per code.claude.com/docs/en/hooks. Fields cairn doesn't
// consume are still accepted by the tolerant decode so future CC
// schema additions don't break us.
type Input struct {
	SessionID      string         `json:"session_id"`
	TranscriptPath string         `json:"transcript_path"`
	CWD            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	StopHookActive bool           `json:"stop_hook_active"`
	ToolName       string         `json:"tool_name,omitempty"`
	ToolInput      map[string]any `json:"tool_input,omitempty"`
}

// ReadInput decodes a hook Input from r. Empty stdin is reported as a
// distinct error so cobra wrappers can treat it as a misinvocation
// (human ran `cairn hook check-drift` directly) and print guidance.
func ReadInput(r io.Reader) (*Input, error) {
	var in Input
	dec := json.NewDecoder(r)
	if err := dec.Decode(&in); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("hook stdin empty — this subcommand is invoked by Claude Code, not humans")
		}
		return nil, fmt.Errorf("decode hook stdin: %w", err)
	}
	return &in, nil
}
