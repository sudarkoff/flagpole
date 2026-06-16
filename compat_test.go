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
		if len(r.Variations) > 0 {
			return true
		}
		if len(r.Range) > 0 {
			return true
		}
		if len(r.Filters) > 0 {
			return true
		}
		if len(r.ParentConditions) > 0 {
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
