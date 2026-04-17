package task

import (
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/intent"
)

// PlanInput is the caller-supplied data for planning from a specs root.
type PlanInput struct {
	OpID      string
	SpecsRoot string
}

// PlanResult is the outcome of a successful Plan call.
// It is an alias for intent.MaterializeResult.
type PlanResult = intent.MaterializeResult

// Plan loads + validates + materializes specs. It does not record an op_log
// entry because plan is deterministic and idempotent by design (upserts by
// id, spec_hash-driven events only fire on change).
func (s *Store) Plan(in PlanInput) (PlanResult, error) {
	bundle, err := intent.Load(in.SpecsRoot)
	if err != nil {
		return PlanResult{}, cairnerr.New(cairnerr.CodeBadInput, "load_failed", err.Error()).
			WithCause(err)
	}
	if errs := intent.Validate(bundle); len(errs) > 0 {
		return PlanResult{}, cairnerr.New(cairnerr.CodeValidation, "spec_invalid",
			fmt.Sprintf("%d spec error(s)", len(errs))).
			WithDetails(map[string]any{"errors": errs})
	}
	iStore := intent.NewStore(s.tx, s.events, s.clock)
	return iStore.Materialize(bundle)
}
