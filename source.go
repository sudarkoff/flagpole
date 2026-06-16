package flagpole

import (
	"context"
	"encoding/json"
)

// Source supplies the full set of feature definitions. Implementations are
// expected to be cheap to call repeatedly; the Client caches results.
type Source interface {
	Load(ctx context.Context) (map[string]Feature, error)
}

// StaticSource serves a fixed set of features (tests, or a GrowthBook-format
// payload fetched elsewhere).
//
// Do not mutate Features after passing the StaticSource to a Client: Load
// returns the map directly, so a later mutation would be observed by the Client
// outside its lock.
type StaticSource struct {
	Features map[string]Feature
}

func (s StaticSource) Load(context.Context) (map[string]Feature, error) {
	return s.Features, nil
}

// StaticSourceFromJSON parses a GrowthBook-style `{"features": {...}}` payload.
func StaticSourceFromJSON(b []byte) (StaticSource, error) {
	var wrapper struct {
		Features map[string]Feature `json:"features"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return StaticSource{}, err
	}
	return StaticSource{Features: wrapper.Features}, nil
}
