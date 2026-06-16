package flagpole

import (
	"context"
	"testing"
	"time"
)

func TestNoopTracker(t *testing.T) {
	var tr Tracker = NoopTracker{}
	// Must not panic and must accept a fully-populated exposure.
	tr.Track(context.Background(), Exposure{
		ExperimentKey: "exp",
		VariationID:   1,
		Attributes:    Attributes{"id": "u1"},
		At:            time.Now(),
	})
}
