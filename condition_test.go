package flagpole

import "testing"

func TestConditionMatch(t *testing.T) {
	attrs := Attributes{"plan": "starter", "role": "user"}
	cases := []struct {
		name string
		cond map[string]any
		want bool
	}{
		{"equality match", map[string]any{"plan": "starter"}, true},
		{"equality miss", map[string]any{"plan": "free"}, false},
		{"in match", map[string]any{"plan": map[string]any{"$in": []any{"free", "starter"}}}, true},
		{"in miss", map[string]any{"plan": map[string]any{"$in": []any{"comp"}}}, false},
		{"ne match", map[string]any{"plan": map[string]any{"$ne": "free"}}, true},
		{"multi-field AND", map[string]any{"plan": "starter", "role": "user"}, true},
		{"multi-field AND miss", map[string]any{"plan": "starter", "role": "admin"}, false},
		{"missing attr", map[string]any{"country": "US"}, false},
	}
	for _, c := range cases {
		got, err := matchCondition(c.cond, attrs)
		if err != nil {
			t.Errorf("%s: unexpected err %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestConditionUnknownOperator(t *testing.T) {
	cond := map[string]any{"age": map[string]any{"$gt": 18}}
	if _, err := matchCondition(cond, Attributes{"age": 20}); err == nil {
		t.Fatal("expected error for unsupported operator $gt")
	}
}

func TestConditionInArrayAttribute(t *testing.T) {
	// actual is an array; $in matches on intersection.
	cond := map[string]any{"tags": map[string]any{"$in": []any{"a", "b"}}}
	if ok, err := matchCondition(cond, Attributes{"tags": []any{"d", "e", "a"}}); err != nil || !ok {
		t.Errorf("intersection should match: ok=%v err=%v", ok, err)
	}
	if ok, err := matchCondition(cond, Attributes{"tags": []any{"x", "y"}}); err != nil || ok {
		t.Errorf("no intersection should not match: ok=%v err=%v", ok, err)
	}
}

func TestConditionInTypedSliceAttribute(t *testing.T) {
	// Callers commonly build attributes as typed slices ([]string, []int) rather
	// than []any. $in must apply the same intersection semantics to those.
	strCond := map[string]any{"cohorts": map[string]any{"$in": []any{"skip-on-sync-beta"}}}
	if ok, err := matchCondition(strCond, Attributes{"cohorts": []string{"skip-on-sync-beta"}}); err != nil || !ok {
		t.Errorf("[]string intersection should match: ok=%v err=%v", ok, err)
	}
	if ok, err := matchCondition(strCond, Attributes{"cohorts": []string{"other"}}); err != nil || ok {
		t.Errorf("[]string no intersection should not match: ok=%v err=%v", ok, err)
	}

	intCond := map[string]any{"groups": map[string]any{"$in": []any{2, 3}}}
	if ok, err := matchCondition(intCond, Attributes{"groups": []int{1, 2}}); err != nil || !ok {
		t.Errorf("[]int intersection should match: ok=%v err=%v", ok, err)
	}
}

func TestConditionTopLevelOperatorErrors(t *testing.T) {
	for _, op := range []string{"$or", "$and", "$not", "$nor"} {
		cond := map[string]any{op: []any{}}
		if _, err := matchCondition(cond, Attributes{}); err == nil {
			t.Errorf("expected error for top-level %s", op)
		}
	}
}
