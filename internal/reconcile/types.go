package reconcile

// Mutation is one concrete state change a rule would apply. Used by both
// the real mutation path (to emit reconcile_rule_applied payloads) and the
// dry-run simulator (for parity testing).
type Mutation struct {
	Rule     int    `json:"rule"`
	EntityID string `json:"entity_id"`
	Action   string `json:"action"` // e.g. "release", "flip_stale", "invalidate", "orphan"
	Reason   string `json:"reason"` // e.g. "expired", "spec_drift", "missing", "grace_expired"
}

// RuleResult captures what one mutating rule did during a real run.
type RuleResult struct {
	Rule      int        `json:"rule"`
	Mutations []Mutation `json:"mutations"`
	Stats     Stats      `json:"-"` // flattened into reconcile stats
}

// Stats is the per-run summary surfaced in the reconcile_ended event payload
// and the JSON response. All counters default to 0.
type Stats struct {
	Rule1ClaimsReleased    int    `json:"rule_1_claims_released"`
	Rule1TasksReverted     int    `json:"rule_1_tasks_reverted"`
	Rule2TasksFlippedStale int    `json:"rule_2_tasks_flipped_stale"`
	Rule2LatencyMs         int64  `json:"rule_2_latency_ms"`
	Rule3EvidenceInvalid   int    `json:"rule_3_evidence_invalidated"`
	Rule3Sampled           int    `json:"rule_3_sampled"`
	Rule3OfTotal           int    `json:"rule_3_of_total"`
	Rule3Mode              string `json:"rule_3_mode"` // "sample" | "full"
	Rule4RunsOrphaned      int    `json:"rule_4_runs_orphaned"`
	Rule5AuthoringErrors   int    `json:"rule_5_authoring_errors"`
}

// AuthoringError is a finding from rule 5 (read-only).
type AuthoringError struct {
	TaskID        string `json:"task_id"`
	MissingGateID string `json:"missing_gate_id"`
}

// Result is the top-level response for a real reconcile run.
type Result struct {
	ReconcileID     string           `json:"reconcile_id"`
	DryRun          bool             `json:"dry_run"`
	Stats           Stats            `json:"stats"`
	AuthoringErrors []AuthoringError `json:"authoring_errors"`
}

// DryRunResult is the response shape for `cairn reconcile --dry-run`. Does
// NOT include reconcile_id (Q9: dry-run didn't happen; no event references an id).
type DryRunResult struct {
	DryRun bool         `json:"dry_run"`
	Rules  []DryRunRule `json:"rules"`
}

// DryRunRule is a per-rule preview. For mutating rules 1..4, Mutations holds
// the would-be mutations. For rule 5 (read-only), AuthoringErrors holds the
// findings; Mutations stays empty.
type DryRunRule struct {
	Rule            int              `json:"rule"`
	Mutations       []Mutation       `json:"would_mutate,omitempty"`
	AuthoringErrors []AuthoringError `json:"authoring_errors,omitempty"`
}
