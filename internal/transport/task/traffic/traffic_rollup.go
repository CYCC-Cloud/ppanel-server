package traffic

import (
	"context"

	"github.com/pars-aria-labs/asynq"
)

// RollupRunner is the network module's traffic rollup: it rebuilds the hourly
// and daily aggregates of the raw traffic log and applies their retention.
type RollupRunner interface {
	RunTrafficRollup(ctx context.Context) error
}

// TrafficRollupHandler is the queue shell of the traffic rollup. Overwriting
// each bucket makes a retry harmless, so asynq owns the retries.
type TrafficRollupHandler struct {
	rollup RollupRunner
}

// NewTrafficRollupHandler builds the shell over the network facade.
func NewTrafficRollupHandler(rollup RollupRunner) *TrafficRollupHandler {
	return &TrafficRollupHandler{rollup: rollup}
}

func (h *TrafficRollupHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	if h.rollup == nil {
		return nil
	}
	return h.rollup.RunTrafficRollup(ctx)
}
