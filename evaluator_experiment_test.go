package flagpole

import "testing"

func expFeature(weights []float64, coverage *float64) Feature {
	return Feature{
		DefaultValue: "control",
		Rules: []Rule{{
			Key:        "exp1",
			Variations: []any{"a", "b"},
			Weights:    weights,
			Coverage:   coverage,
		}},
	}
}

func TestEvaluateExperimentAssignsDeterministically(t *testing.T) {
	f := expFeature(nil, nil)
	r1 := Evaluate(f, "feat", Attributes{"id": "user-123"})
	r2 := Evaluate(f, "feat", Attributes{"id": "user-123"})
	if !r1.InExperiment || r1.VariationID < 0 {
		t.Fatalf("expected in-experiment assignment, got %+v", r1)
	}
	if r1.VariationID != r2.VariationID || r1.Value != r2.Value {
		t.Errorf("non-deterministic: %+v vs %+v", r1, r2)
	}
	if r1.HashAttribute != "id" || r1.HashValue != "user-123" {
		t.Errorf("hash unit = %q/%q, want id/user-123", r1.HashAttribute, r1.HashValue)
	}
	if r1.Value != f.Rules[0].Variations[r1.VariationID] {
		t.Errorf("value %v != variation[%d]", r1.Value, r1.VariationID)
	}
}

func TestEvaluateExperimentNoIdentifierFallsThrough(t *testing.T) {
	r := Evaluate(expFeature(nil, nil), "feat", Attributes{"plan": "pro"})
	if r.InExperiment {
		t.Errorf("no id => must not be in experiment: %+v", r)
	}
	if r.Value != "control" {
		t.Errorf("value = %v, want control (default)", r.Value)
	}
}

func TestEvaluateExperimentZeroCoverageExcludes(t *testing.T) {
	zero := 0.0
	r := Evaluate(expFeature(nil, &zero), "feat", Attributes{"id": "user-123"})
	if r.InExperiment {
		t.Errorf("coverage 0 => excluded, got %+v", r)
	}
	if r.Value != "control" {
		t.Errorf("value = %v, want control", r.Value)
	}
}

func TestEvaluateExperimentSkewedWeights(t *testing.T) {
	// Weight all traffic to variation index 1; every assigned unit must get "b".
	f := expFeature([]float64{0.0, 1.0}, nil)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		r := Evaluate(f, "feat", Attributes{"id": id})
		if r.InExperiment && r.VariationID != 1 {
			t.Errorf("id %s: variation = %d, want 1", id, r.VariationID)
		}
	}
}

func TestEvaluateExperimentConditionTargeting(t *testing.T) {
	f := Feature{
		DefaultValue: "control",
		Rules: []Rule{{
			Key:        "exp1",
			Condition:  map[string]any{"plan": "pro"},
			Variations: []any{"a", "b"},
		}},
	}
	off := Evaluate(f, "feat", Attributes{"id": "u1", "plan": "free"})
	if off.InExperiment {
		t.Errorf("free user must not enter experiment: %+v", off)
	}
	on := Evaluate(f, "feat", Attributes{"id": "u1", "plan": "pro"})
	if !on.InExperiment {
		t.Errorf("pro user must enter experiment: %+v", on)
	}
}
