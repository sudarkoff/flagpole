package flagpole

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

func loadCases(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatalf("read cases.json: %v", err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatalf("parse cases.json: %v", err)
	}
	return all
}

func TestCompatHash(t *testing.T) {
	all := loadCases(t)
	var cases [][]any
	if err := json.Unmarshal(all["hash"], &cases); err != nil {
		t.Fatalf("parse hash cases: %v", err)
	}
	for _, c := range cases {
		seed, _ := c[0].(string)
		value, _ := c[1].(string)
		version, _ := c[2].(float64)
		if version != 2 {
			continue // we implement v2 only
		}
		want, ok := c[3].(float64)
		if !ok {
			continue // null result = N/A
		}
		if got := hashV2(seed, value); math.Abs(got-want) > 1e-9 {
			t.Errorf("hashV2(%q,%q) = %v, want %v", seed, value, got, want)
		}
	}
}

func TestCompatFeatures(t *testing.T) {
	all := loadCases(t)
	var cases [][]json.RawMessage
	if err := json.Unmarshal(all["feature"], &cases); err != nil {
		t.Fatalf("parse feature cases: %v", err)
	}
	for _, c := range cases {
		var name string
		_ = json.Unmarshal(c[0], &name)

		var ctx struct {
			Attributes Attributes         `json:"attributes"`
			Features   map[string]Feature `json:"features"`
		}
		if err := json.Unmarshal(c[1], &ctx); err != nil {
			continue // contexts using fields we don't model: skip
		}
		var key string
		_ = json.Unmarshal(c[2], &key)
		var want struct {
			Value any  `json:"value"`
			On    bool `json:"on"`
		}
		_ = json.Unmarshal(c[3], &want)

		feat, ok := ctx.Features[key]
		if !ok {
			continue
		}
		// Skip cases that exercise unsupported rule kinds (experiments / unknown ops).
		if usesUnsupported(feat) {
			t.Logf("skip unsupported case %q", name)
			continue
		}

		got := Evaluate(feat, key, ctx.Attributes)
		if got.On != want.On {
			t.Errorf("%s: on = %v, want %v", name, got.On, want.On)
		}
		if !reflect.DeepEqual(got.Value, want.Value) {
			t.Errorf("%s: value = %#v, want %#v", name, got.Value, want.Value)
		}
	}
}

func TestCompatEvalCondition(t *testing.T) {
	all := loadCases(t)
	var cases [][]json.RawMessage
	if err := json.Unmarshal(all["evalCondition"], &cases); err != nil {
		t.Fatalf("parse evalCondition cases: %v", err)
	}
	supported := 0
	for _, c := range cases {
		var name string
		_ = json.Unmarshal(c[0], &name)
		var cond map[string]any
		if err := json.Unmarshal(c[1], &cond); err != nil {
			continue
		}
		var attrs Attributes
		if err := json.Unmarshal(c[2], &attrs); err != nil {
			continue
		}
		var want bool
		_ = json.Unmarshal(c[3], &want)

		got, err := matchCondition(cond, attrs)
		if err != nil {
			// Uses a construct outside our supported subset (logical/unknown operators).
			continue
		}
		supported++
		if got != want {
			t.Errorf("%s: matchCondition = %v, want %v", name, got, want)
		}
	}
	if supported == 0 {
		t.Fatal("expected at least some supported evalCondition cases to be exercised")
	}
	t.Logf("evalCondition: %d supported cases asserted", supported)
}

// usesUnsupported reports whether a feature relies on rule kinds outside this
// library's Phase-A subset (experiment variations, or condition operators we
// don't implement).
//
// Widened skips beyond the original experiment-variations check:
//   - range / filters: GrowthBook's advanced bucketing (Phase B+ feature).
//     A rule with a "range" field uses per-bucket ranges instead of a simple
//     coverage float, and "filters" implements namespace-style exclusion.
//     Both are outside the Phase-A subset.
//   - parentConditions: GrowthBook prerequisite flags — a rule that gates on
//     the result of another flag evaluation, outside Phase-A.
func usesUnsupported(f Feature) bool {
	for _, r := range f.Rules {
		if len(r.Range) > 0 {
			return true
		}
		if len(r.Ranges) > 0 {
			return true
		}
		if len(r.Namespace) > 0 {
			return true
		}
		if len(r.Filters) > 0 {
			return true
		}
		if len(r.ParentConditions) > 0 {
			return true
		}
		// Experiment rules only support hashVersion 2; default (nil) is v1 in
		// GrowthBook, which we don't implement.
		if len(r.Variations) >= 2 && (r.HashVersion == nil || *r.HashVersion != 2) {
			return true
		}
		if r.HashVersion != nil && *r.HashVersion != 2 {
			return true
		}
		if r.Condition != nil {
			if _, err := matchCondition(r.Condition, Attributes{}); err != nil {
				return true
			}
		}
	}
	return false
}

// TestCompatRun validates experiment assignment against GrowthBook's run()
// fixtures, mapped onto flagpole's experiment-as-rule model: the experiment
// object becomes a single experiment Rule, with the feature default set to the
// control variation so a not-in-experiment fall-through matches run()'s
// not-in-experiment return value.
func TestCompatRun(t *testing.T) {
	all := loadCases(t)
	var cases [][]json.RawMessage
	if err := json.Unmarshal(all["run"], &cases); err != nil {
		t.Fatalf("parse run cases: %v", err)
	}
	asserted := 0
	for _, c := range cases {
		var name string
		_ = json.Unmarshal(c[0], &name)
		var ctx struct {
			Attributes Attributes `json:"attributes"`
		}
		if err := json.Unmarshal(c[1], &ctx); err != nil {
			continue
		}
		var exp struct {
			Key           string         `json:"key"`
			Variations    []any          `json:"variations"`
			Weights       []float64      `json:"weights"`
			Coverage      *float64       `json:"coverage"`
			HashAttribute string         `json:"hashAttribute"`
			Seed          string         `json:"seed"`
			HashVersion   *int           `json:"hashVersion"`
			Condition     map[string]any `json:"condition"`
			// Unsupported markers — presence => skip.
			Namespace        json.RawMessage `json:"namespace"`
			Filters          json.RawMessage `json:"filters"`
			Ranges           json.RawMessage `json:"ranges"`
			ParentConditions json.RawMessage `json:"parentConditions"`
			Force            json.RawMessage `json:"force"`
			Active           json.RawMessage `json:"active"`
		}
		if err := json.Unmarshal(c[2], &exp); err != nil {
			continue
		}
		if exp.Namespace != nil || exp.Filters != nil || exp.Ranges != nil ||
			exp.ParentConditions != nil || exp.Force != nil || exp.Active != nil {
			continue // outside our supported subset
		}
		// We implement hashVersion 2 only; GrowthBook's default is v1.
		if exp.HashVersion == nil || *exp.HashVersion != 2 {
			continue
		}
		if len(exp.Variations) < 2 {
			continue
		}
		if exp.Condition != nil {
			if _, err := matchCondition(exp.Condition, Attributes{}); err != nil {
				continue // condition uses an unsupported operator
			}
		}
		var wantValue any
		_ = json.Unmarshal(c[3], &wantValue)
		var wantInExp bool
		_ = json.Unmarshal(c[4], &wantInExp)

		feat := Feature{
			DefaultValue: exp.Variations[0], // control == not-in-experiment value
			Rules: []Rule{{
				Key:           exp.Key,
				Variations:    exp.Variations,
				Weights:       exp.Weights,
				Coverage:      exp.Coverage,
				HashAttribute: exp.HashAttribute,
				Seed:          exp.Seed,
				HashVersion:   exp.HashVersion,
				Condition:     exp.Condition,
			}},
		}
		got := Evaluate(feat, exp.Key, ctx.Attributes)
		asserted++
		if got.InExperiment != wantInExp {
			t.Errorf("%s: inExperiment = %v, want %v", name, got.InExperiment, wantInExp)
		}
		if !reflect.DeepEqual(got.Value, wantValue) {
			t.Errorf("%s: value = %#v, want %#v", name, got.Value, wantValue)
		}
	}
	if asserted == 0 {
		t.Fatal("expected at least some supported run cases to be asserted")
	}
	t.Logf("run: %d supported experiment cases asserted", asserted)
}
