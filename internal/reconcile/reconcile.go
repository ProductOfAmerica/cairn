package reconcile

import (
	"context"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

// Opts controls a single reconcile invocation.
type Opts struct {
	DryRun             bool
	EvidenceSampleFull bool
	// SamplePct / SampleCap may be set by tests; defaults apply when zero.
	SamplePct float64
	SampleCap int
}

// Orchestrator ties together the probe + five rules. Held via NewOrchestrator.
type Orchestrator struct {
	db       *db.DB
	clock    clock.Clock
	ids      *ids.Generator
	blobRoot string
}

// NewOrchestrator constructs the runner. blobRoot matters only for rule 3's
// probe (file paths stored in evidence.uri are absolute, so blobRoot is
// reserved for future symmetry with evidence.Store).
func NewOrchestrator(h *db.DB, clk clock.Clock, g *ids.Generator, blobRoot string) *Orchestrator {
	return &Orchestrator{db: h, clock: clk, ids: g, blobRoot: blobRoot}
}

// Run executes the reconcile in two phases: probe (outside tx), mutation
// (one BEGIN IMMEDIATE). Dry-run short-circuits to the pure-read simulator.
func (o *Orchestrator) Run(ctx context.Context, opts Opts) (Result, error) {
	if opts.DryRun {
		dr, err := o.dryRun(ctx, opts)
		if err != nil {
			return Result{}, err
		}
		// Adapt DryRunResult to Result shape so the CLI can emit uniformly.
		return Result{DryRun: true, AuthoringErrors: extractAuthoringFromDry(dr)}, nil
	}

	// =================================================================
	// PROBE PHASE — NO TX. Filesystem I/O only; zero writes, zero events.
	// Collects candidate mutations into an in-memory struct.
	// DO NOT move these reads inside the mutation tx — doing so
	// reintroduces the Q8 lock-contention problem (100-blob sha256
	// under BEGIN IMMEDIATE starves concurrent writers).
	// =================================================================
	probeOpts := ProbeOpts{Full: opts.EvidenceSampleFull, SamplePct: opts.SamplePct, SampleCap: opts.SampleCap}
	candidates, err := RunEvidenceProbe(ctx, o.db, probeOpts)
	if err != nil {
		return Result{}, fmt.Errorf("probe: %w", err)
	}
	sampled, err := SampleSize(o.db, probeOpts)
	if err != nil {
		return Result{}, err
	}
	var total int
	if err := o.db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM evidence`).Scan(&total); err != nil {
		return Result{}, err
	}

	// =================================================================
	// MUTATION PHASE — ONE BEGIN IMMEDIATE. All rule writes + events.
	// Rule ordering: 1 → 2 → 3 → 4 → 5.
	//   - Rule 4 depends on rule 1 running first (fresh released_at is
	//     within 10min grace; orphan sweep correctly skips).
	//   - Rule 5 is read-only; emits no events; findings in stats.
	// =================================================================
	reconcileID := o.ids.ULID()
	appender := events.NewAppender(o.clock)

	var result Result
	result.ReconcileID = reconcileID
	result.DryRun = false

	err = o.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_started",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"reconcile_id": reconcileID,
			},
		}); err != nil {
			return err
		}

		r1, err := Rule1ReleaseExpiredLeases(tx, appender, o.clock, reconcileID)
		if err != nil {
			return err
		}
		r2, err := Rule2FlipStaleTasks(tx, appender, o.clock, reconcileID)
		if err != nil {
			return err
		}
		r3, err := Rule3ApplyEvidenceInvalidations(tx, appender, o.clock, reconcileID, candidates)
		if err != nil {
			return err
		}
		r4, err := Rule4OrphanExpiredRuns(tx, appender, o.clock, reconcileID)
		if err != nil {
			return err
		}
		authErrs, err := Rule5CollectAuthoringErrors(tx)
		if err != nil {
			return err
		}

		// Merge stats.
		result.Stats = mergeStats(r1, r2, r3, r4)
		result.Stats.Rule3Sampled = sampled
		result.Stats.Rule3OfTotal = total
		if opts.EvidenceSampleFull {
			result.Stats.Rule3Mode = "full"
		} else {
			result.Stats.Rule3Mode = "sample"
		}
		result.Stats.Rule5AuthoringErrors = len(authErrs)
		result.AuthoringErrors = authErrs

		return appender.Append(tx, events.Record{
			Kind:       "reconcile_ended",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"reconcile_id":     reconcileID,
				"stats":            result.Stats,
				"authoring_errors": authErrs,
			},
		})
	})
	if err != nil {
		return Result{}, err
	}
	if result.AuthoringErrors == nil {
		result.AuthoringErrors = []AuthoringError{}
	}
	return result, nil
}

func mergeStats(rs ...RuleResult) Stats {
	var out Stats
	for _, r := range rs {
		s := r.Stats
		out.Rule1ClaimsReleased += s.Rule1ClaimsReleased
		out.Rule1TasksReverted += s.Rule1TasksReverted
		out.Rule2TasksFlippedStale += s.Rule2TasksFlippedStale
		if s.Rule2LatencyMs > out.Rule2LatencyMs {
			out.Rule2LatencyMs = s.Rule2LatencyMs
		}
		out.Rule3EvidenceInvalid += s.Rule3EvidenceInvalid
		out.Rule4RunsOrphaned += s.Rule4RunsOrphaned
	}
	return out
}

func extractAuthoringFromDry(dr DryRunResult) []AuthoringError {
	for _, r := range dr.Rules {
		if r.Rule == 5 {
			return r.AuthoringErrors
		}
	}
	return nil
}
