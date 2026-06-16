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
