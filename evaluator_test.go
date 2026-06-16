package flagpole

import "testing"

func TestEvaluateDefault(t *testing.T) {
	f := Feature{DefaultValue: false}
	r := Evaluate(f, "flag", Attributes{"id": "u1"})
	if r.Value != false {
		t.Errorf("value = %v, want false", r.Value)
	}
	if r.On {
		t.Errorf("on = true, want false")
	}
}

func TestEvaluateForceWithCondition(t *testing.T) {
	f := Feature{
		DefaultValue: false,
		Rules: []Rule{
			{Condition: map[string]any{"plan": "starter"}, Force: true},
		},
	}
	if r := Evaluate(f, "flag", Attributes{"id": "u1", "plan": "starter"}); !r.On {
		t.Errorf("starter should be on")
	}
	if r := Evaluate(f, "flag", Attributes{"id": "u1", "plan": "free"}); r.On {
		t.Errorf("free should be off")
	}
}

func TestEvaluateSkipsUnsupportedConditionRule(t *testing.T) {
	// First rule uses an unsupported operator ($gt) -> matchCondition errors ->
	// the rule must be skipped, not applied. The second rule should win.
	f := Feature{
		DefaultValue: false,
		Rules: []Rule{
			{Condition: map[string]any{"age": map[string]any{"$gt": 18}}, Force: true},
			{Condition: map[string]any{"plan": "starter"}, Force: true},
		},
	}
	// Unsupported-operator rule skipped; "starter" rule applies -> on.
	if r := Evaluate(f, "flag", Attributes{"id": "u1", "age": 30, "plan": "starter"}); !r.On {
		t.Error("expected the unsupported-operator rule to be skipped and the starter rule to apply")
	}
	// Unsupported-operator rule skipped; second rule misses -> default false.
	if r := Evaluate(f, "flag", Attributes{"id": "u1", "age": 30, "plan": "free"}); r.On {
		t.Error("expected default (off) when no valid rule matches")
	}
}

func TestTruthyIntZeroIsFalse(t *testing.T) {
	f := Feature{DefaultValue: 0} // Go int zero
	if Evaluate(f, "flag", Attributes{"id": "u1"}).On {
		t.Error("int zero default should be off")
	}
}

func TestEvaluateSkipsUnknownHashVersionRule(t *testing.T) {
	v := 99
	cov := 1.0
	f := Feature{
		DefaultValue: "default",
		Rules: []Rule{
			{Coverage: &cov, HashVersion: &v, Force: "forced"},
		},
	}
	// Unknown hashVersion => rollout rule skipped => default value.
	r := Evaluate(f, "flag", Attributes{"id": "u1"})
	if r.Value != "default" {
		t.Errorf("value = %v, want \"default\" (rule should be skipped)", r.Value)
	}
}

func TestEvaluateAppliesHashVersion2Rule(t *testing.T) {
	v := 2
	cov := 1.0 // coverage 1.0 => everyone included
	f := Feature{
		DefaultValue: "default",
		Rules: []Rule{
			{Coverage: &cov, HashVersion: &v, Force: "forced"},
		},
	}
	if r := Evaluate(f, "flag", Attributes{"id": "u1"}); r.Value != "forced" {
		t.Errorf("value = %v, want \"forced\" (hashVersion 2, coverage 1.0)", r.Value)
	}
}

func TestEvaluateCoverageDeterministic(t *testing.T) {
	cov := 0.5
	f := Feature{
		DefaultValue: false,
		Rules:        []Rule{{Coverage: &cov, HashAttribute: "id", Force: true}},
	}
	// Same id must always yield the same answer.
	first := Evaluate(f, "flag", Attributes{"id": "user-123"}).On
	for i := 0; i < 5; i++ {
		if Evaluate(f, "flag", Attributes{"id": "user-123"}).On != first {
			t.Fatal("coverage bucketing is not deterministic")
		}
	}
}
