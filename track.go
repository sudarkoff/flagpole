package flagpole

import (
	"context"
	"time"
)

// Exposure records that a unit was exposed to a variation of an experiment.
// Field shape mirrors GrowthBook exposure logging so downstream analysis can be
// done by GrowthBook or by hand-written SQL.
type Exposure struct {
	ExperimentKey string
	VariationID   int
	Attributes    Attributes
	At            time.Time
}

// Tracker records experiment exposures. Phase A ships only the no-op; consumers
// supply a persistent implementation for Phase B.
type Tracker interface {
	Track(ctx context.Context, e Exposure)
}

// NoopTracker discards exposures.
type NoopTracker struct{}

func (NoopTracker) Track(context.Context, Exposure) {}
