package flagpole

// Feature is a single flag definition. Its JSON shape is a strict subset of
// GrowthBook's feature schema, so definitions port to GrowthBook unchanged.
type Feature struct {
	DefaultValue any    `json:"defaultValue"`
	Rules        []Rule `json:"rules,omitempty"`
}

// Rule is one targeting/rollout/experiment rule, evaluated in order.
type Rule struct {
	// Targeting: a GrowthBook-style condition object (subset supported).
	Condition map[string]any `json:"condition,omitempty"`

	// Forced value when the rule applies.
	Force any `json:"force,omitempty"`

	// Percentage rollout in [0,1].
	Coverage      *float64 `json:"coverage,omitempty"`
	HashAttribute string   `json:"hashAttribute,omitempty"`
	Seed          string   `json:"seed,omitempty"`

	// Experiment fields (Phase B). Present in the schema now; evaluation of
	// experiment rules is not implemented in this plan.
	Key        string    `json:"key,omitempty"`
	Variations []any     `json:"variations,omitempty"`
	Weights    []float64 `json:"weights,omitempty"`
}
