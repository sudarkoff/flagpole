# flagpole Phase B — Experiments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make flagpole assign experiment variations (GrowthBook-compatible) and emit exposure records a BI layer can read, including a batteries-included `trackpg` Postgres tracker.

**Architecture:** An experiment is a `Rule` carrying `Variations` (experiment-as-rule). The pure `Evaluate` function gains variation assignment and returns richer metadata; the stateful `Evaluation`/`Client` layer fires `tracker.Track` on assignment. A new `trackpg` subpackage persists exposures asynchronously, with `trackpg/schema.sql` as the canonical contract for downstream readers (gnomon).

**Tech Stack:** Go 1.25, `github.com/jackc/pgx/v5` (confined to subpackages), GrowthBook `cases.json` fixtures (`testdata/cases.json`).

**Source spec:** `docs/superpowers/specs/2026-06-19-flagpole-phase-b-experiments-design.md`

## Global Constraints

- Module path: `github.com/sudarkoff/flagpole`. Core package stays **zero third-party deps**; pgx only in `sourcepg`/`trackpg`.
- **Hash version 2 only.** A rule requesting any other `hashVersion` is skipped, never mis-evaluated.
- **GrowthBook drop-in:** assignment math must pass the vendored `cases.json` fixtures (`getEqualWeights`, `getBucketRange`, `chooseVariation`, `run`, `feature`). Unsupported fixtures are explicitly skipped, never silently passed.
- **Experiment identity:** a `Rule` is an experiment rule iff `len(Variations) >= 2`.
- **Fire every genuine assignment, no cross-unit dedup.** Fire-once only within a single `Evaluation` binding.
- Out of scope: namespaces, sticky/fallback bucketing, `range`/`filters`/`parentConditions`, non-2 hash versions, all metric/significance analysis (gnomon).
- TDD: write the failing test first; commit after each task.

---

## File Structure

| Path | Responsibility |
|------|----------------|
| `bucket.go` (new) | Pure GrowthBook bucketing math: `getEqualWeights`, `getBucketRanges`, `chooseVariation` |
| `bucket_test.go` (new) | Unit + `cases.json` compat for the bucketing math |
| `evaluator.go` (modify) | `Result` experiment fields; experiment-rule branch in `Evaluate` |
| `evaluator_experiment_test.go` (new) | Assignment determinism, coverage, weights, no-id, condition targeting |
| `compat_test.go` (modify) | Stop skipping experiment rules; add `TestCompatRun` |
| `track.go` (modify) | Add `HashAttribute` / `HashValue` to `Exposure` |
| `client.go` (modify) | `ForContext`; per-binding fire-once; fire `tracker.Track` on assignment |
| `client_experiment_test.go` (new) | Exposure firing semantics |
| `trackpg/trackpg.go` (new) | Async-batching Postgres `Tracker` |
| `trackpg/schema.sql` (new) | Canonical exposures DDL (the gnomon contract) |
| `trackpg/trackpg_test.go` (new) | DB-gated batch/flush/overflow tests |
| `README.md`, `USAGE.md` (modify) | Experiments docs, firing semantics, `trackpg`, contract |

---

## Task 1: Bucketing math (`getEqualWeights`, `getBucketRanges`, `chooseVariation`)

**Files:**
- Create: `bucket.go`
- Create: `bucket_test.go`
- Test: `bucket_test.go`

**Interfaces:**
- Produces:
  - `getEqualWeights(n int) []float64`
  - `getBucketRanges(numVariations int, coverage float64, weights []float64) [][2]float64`
  - `chooseVariation(bucket float64, ranges [][2]float64) int`

- [ ] **Step 1: Write the failing compat tests**

`bucket_test.go`:
```go
package flagpole

import (
	"encoding/json"
	"math"
	"testing"
)

func approxRanges(a, b [][2]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i][0]-b[i][0]) > 1e-9 || math.Abs(a[i][1]-b[i][1]) > 1e-9 {
			return false
		}
	}
	return true
}

func TestCompatGetEqualWeights(t *testing.T) {
	all := loadCases(t)
	var cases [][]json.RawMessage
	if err := json.Unmarshal(all["getEqualWeights"], &cases); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range cases {
		var n int
		_ = json.Unmarshal(c[0], &n)
		var want []float64
		_ = json.Unmarshal(c[1], &want)
		got := getEqualWeights(n)
		if len(got) != len(want) {
			t.Errorf("getEqualWeights(%d) len = %d, want %d", n, len(got), len(want))
			continue
		}
		for i := range want {
			if math.Abs(got[i]-want[i]) > 1e-9 {
				t.Errorf("getEqualWeights(%d)[%d] = %v, want %v", n, i, got[i], want[i])
			}
		}
	}
}

func TestCompatGetBucketRange(t *testing.T) {
	all := loadCases(t)
	var cases [][]json.RawMessage
	if err := json.Unmarshal(all["getBucketRange"], &cases); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range cases {
		var name string
		_ = json.Unmarshal(c[0], &name)
		var args []json.RawMessage
		_ = json.Unmarshal(c[1], &args)
		var n int
		_ = json.Unmarshal(args[0], &n)
		var coverage float64
		_ = json.Unmarshal(args[1], &coverage)
		var weights []float64
		_ = json.Unmarshal(args[2], &weights) // null -> nil
		var want [][2]float64
		_ = json.Unmarshal(c[2], &want)
		got := getBucketRanges(n, coverage, weights)
		if !approxRanges(got, want) {
			t.Errorf("%s: getBucketRanges = %v, want %v", name, got, want)
		}
	}
}

func TestCompatChooseVariation(t *testing.T) {
	all := loadCases(t)
	var cases [][]json.RawMessage
	if err := json.Unmarshal(all["chooseVariation"], &cases); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range cases {
		var name string
		_ = json.Unmarshal(c[0], &name)
		var bucket float64
		_ = json.Unmarshal(c[1], &bucket)
		var ranges [][2]float64
		_ = json.Unmarshal(c[2], &ranges)
		var want int
		_ = json.Unmarshal(c[3], &want)
		if got := chooseVariation(bucket, ranges); got != want {
			t.Errorf("%s: chooseVariation(%v) = %d, want %d", name, bucket, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -run 'TestCompatGetEqualWeights|TestCompatGetBucketRange|TestCompatChooseVariation' .`
Expected: FAIL (`undefined: getEqualWeights` / `getBucketRanges` / `chooseVariation`).

- [ ] **Step 3: Implement the bucketing math**

`bucket.go`:
```go
package flagpole

import "math"

// getEqualWeights returns n equal weights summing to 1. n<=0 yields an empty
// slice (matches GrowthBook).
func getEqualWeights(n int) []float64 {
	if n <= 0 {
		return []float64{}
	}
	w := make([]float64, n)
	for i := range w {
		w[i] = 1.0 / float64(n)
	}
	return w
}

// round6 rounds to 6 decimal places, matching GrowthBook's bucket-range
// rounding so boundaries compare cleanly against the fixtures.
func round6(f float64) float64 { return math.Round(f*1e6) / 1e6 }

// getBucketRanges returns cumulative [start,end) ranges, one per variation,
// each scaled by coverage. Weights are equalized when absent, the wrong length,
// or not summing to ~1 (GrowthBook semantics).
func getBucketRanges(numVariations int, coverage float64, weights []float64) [][2]float64 {
	if coverage < 0 {
		coverage = 0
	}
	if coverage > 1 {
		coverage = 1
	}
	if len(weights) != numVariations {
		weights = getEqualWeights(numVariations)
	} else {
		sum := 0.0
		for _, w := range weights {
			sum += w
		}
		if sum < 0.99 || sum > 1.01 {
			weights = getEqualWeights(numVariations)
		}
	}
	ranges := make([][2]float64, len(weights))
	cumulative := 0.0
	for i, w := range weights {
		start := cumulative
		cumulative += w
		ranges[i] = [2]float64{round6(start), round6(start + coverage*w)}
	}
	return ranges
}

// chooseVariation returns the index of the range containing bucket, or -1 when
// the bucket falls in no range (not in the experiment).
func chooseVariation(bucket float64, ranges [][2]float64) int {
	for i, r := range ranges {
		if bucket >= r[0] && bucket < r[1] {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -run 'TestCompatGetEqualWeights|TestCompatGetBucketRange|TestCompatChooseVariation' .`
Expected: PASS.

- [ ] **Step 5: Full suite + commit**

Run: `go test ./...`
Expected: PASS (no regressions).
```bash
git add bucket.go bucket_test.go
git commit -m "feat(experiments): GrowthBook bucketing math (equal weights, ranges, choose)"
```

---

## Task 2: Experiment assignment in the evaluator

**Files:**
- Modify: `evaluator.go`
- Modify: `compat_test.go` (the `usesUnsupported` helper + add `TestCompatRun`)
- Create: `evaluator_experiment_test.go`
- Test: `evaluator_experiment_test.go`, `compat_test.go`

**Interfaces:**
- Consumes: `getBucketRanges`, `chooseVariation` (Task 1); `hashV2`, `stringAttr`, `truthy`, `matchCondition` (existing).
- Produces: extended `Result{Value, On, VariationID, InExperiment, HashAttribute, HashValue}`; `Evaluate` assigns variations for experiment rules.

- [ ] **Step 1: Write the failing evaluator tests**

`evaluator_experiment_test.go`:
```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -run TestEvaluateExperiment .`
Expected: FAIL (`r1.InExperiment undefined` — `Result` has no experiment fields yet).

- [ ] **Step 3: Extend `Result` and add the experiment branch**

In `evaluator.go`, replace the `Result` struct:
```go
// Result is the outcome of evaluating a feature.
type Result struct {
	Value any  // resolved value (force value, variation value, or defaultValue)
	On    bool // truthiness of Value

	// Experiment metadata (zero-valued for non-experiment results).
	VariationID   int    // assigned variation index; meaningful only when InExperiment
	InExperiment  bool   // true only on a genuine hash-based assignment
	HashAttribute string // attribute used for bucketing
	HashValue     string // the actual unit value bucketed (the join key)
}
```

In `evaluator.go`, replace the body of `Evaluate` (the `for` loop) so the experiment branch runs before the rollout-coverage block:
```go
func Evaluate(f Feature, featureKey string, attrs Attributes) Result {
	for _, rule := range f.Rules {
		if rule.Condition != nil {
			ok, err := matchCondition(rule.Condition, attrs)
			if err != nil {
				continue // a rule we cannot understand must not silently apply
			}
			if !ok {
				continue
			}
		}

		// Experiment rule: variations present => assign a variation.
		if len(rule.Variations) >= 2 {
			if r, matched := assignExperiment(rule, attrs); matched {
				return r
			}
			continue // not bucketed in => fall through to next rule / default
		}

		if rule.Coverage != nil {
			if !inCoverage(rule, featureKey, attrs) {
				continue
			}
		}
		// Rule applies. A rule with neither force nor coverage is a plain match.
		val := rule.Force
		if val == nil {
			val = f.DefaultValue
		}
		return Result{Value: val, On: truthy(val)}
	}
	return Result{Value: f.DefaultValue, On: truthy(f.DefaultValue)}
}

// assignExperiment buckets a unit into an experiment rule. matched is false when
// the unit is not in the experiment (no identifier, unsupported hash version, or
// outside coverage), in which case the caller falls through.
func assignExperiment(rule Rule, attrs Attributes) (Result, bool) {
	// hashVersion 2 only; an explicit other version is outside our subset.
	if rule.HashVersion != nil && *rule.HashVersion != 2 {
		return Result{}, false
	}
	hashAttr := rule.HashAttribute
	if hashAttr == "" {
		hashAttr = "id"
	}
	hashValue := stringAttr(attrs[hashAttr])
	if hashValue == "" {
		return Result{}, false // no identifier => never in an experiment
	}
	seed := rule.Seed
	if seed == "" {
		seed = rule.Key // experiment key is the default seed
	}
	coverage := 1.0
	if rule.Coverage != nil {
		coverage = *rule.Coverage
	}
	ranges := getBucketRanges(len(rule.Variations), coverage, rule.Weights)
	i := chooseVariation(hashV2(seed, hashValue), ranges)
	if i < 0 {
		return Result{}, false // outside coverage
	}
	val := rule.Variations[i]
	return Result{
		Value:         val,
		On:            truthy(val),
		VariationID:   i,
		InExperiment:  true,
		HashAttribute: hashAttr,
		HashValue:     hashValue,
	}, true
}
```

- [ ] **Step 4: Run the evaluator tests**

Run: `go test -run TestEvaluateExperiment .`
Expected: PASS.

- [ ] **Step 5: Stop skipping experiment rules in the feature-compat harness**

In `compat_test.go`, edit `usesUnsupported` to remove the experiment-variations skip (experiments are now supported) while keeping the rest. Replace:
```go
	for _, r := range f.Rules {
		if len(r.Variations) > 0 {
			return true
		}
		if len(r.Range) > 0 {
```
with:
```go
	for _, r := range f.Rules {
		if len(r.Range) > 0 {
```
(Leave the `Range`/`Filters`/`ParentConditions`/`HashVersion`/`Condition` checks intact.)

- [ ] **Step 6: Add the experiment `run` compat suite**

Append to `compat_test.go`:
```go
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
			Key           string    `json:"key"`
			Variations    []any     `json:"variations"`
			Weights       []float64 `json:"weights"`
			Coverage      *float64  `json:"coverage"`
			HashAttribute string    `json:"hashAttribute"`
			Seed          string    `json:"seed"`
			HashVersion   *int      `json:"hashVersion"`
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
		if exp.HashVersion != nil && *exp.HashVersion != 2 {
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
```

- [ ] **Step 7: Run the compat suite + full suite**

Run: `go test ./...`
Expected: PASS — including `TestCompatRun` (logs "run: N supported experiment cases asserted", N>0) and the now-unskipped experiment feature cases.

- [ ] **Step 8: Commit**

```bash
git add evaluator.go evaluator_experiment_test.go compat_test.go
git commit -m "feat(experiments): variation assignment in evaluator + GrowthBook run() compat"
```

---

## Task 3: Exposure shape + firing layer

**Files:**
- Modify: `track.go`
- Modify: `client.go`
- Create: `client_experiment_test.go`
- Test: `client_experiment_test.go`

**Interfaces:**
- Consumes: extended `Result` (Task 2); `Tracker`, `Exposure` (existing).
- Produces:
  - `Exposure{ExperimentKey, VariationID, HashAttribute, HashValue, Attributes, At}`
  - `(*Client).ForContext(ctx context.Context, attrs Attributes) *Evaluation`
  - `Evaluation` fires `tracker.Track` once per feature key per binding when `InExperiment`.

- [ ] **Step 1: Write the failing firing test**

`client_experiment_test.go`:
```go
package flagpole

import (
	"context"
	"sync"
	"testing"
)

type recordingTracker struct {
	mu  sync.Mutex
	exp []Exposure
}

func (r *recordingTracker) Track(_ context.Context, e Exposure) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exp = append(r.exp, e)
}

func (r *recordingTracker) all() []Exposure {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Exposure(nil), r.exp...)
}

func newExpClient(t *testing.T, tr Tracker) *Client {
	t.Helper()
	src := StaticSource{Features: map[string]Feature{
		"feat": {
			DefaultValue: "control",
			Rules: []Rule{{
				Key:        "exp1",
				Variations: []any{"a", "b"},
			}},
		},
	}}
	c, err := New(context.Background(), src, WithRefreshInterval(0), WithTracker(tr))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestExposureFiresOncePerBinding(t *testing.T) {
	tr := &recordingTracker{}
	c := newExpClient(t, tr)
	ev := c.For(Attributes{"id": "user-1"})
	_ = ev.IsOn("feat")
	_ = ev.Value("feat", nil) // second read of same key, same binding
	got := tr.all()
	if len(got) != 1 {
		t.Fatalf("expected 1 exposure, got %d", len(got))
	}
	e := got[0]
	if e.ExperimentKey != "exp1" || e.HashAttribute != "id" || e.HashValue != "user-1" {
		t.Errorf("exposure = %+v", e)
	}
	if e.At.IsZero() {
		t.Error("exposure timestamp not set")
	}
}

func TestExposureDoesNotFireWhenNotInExperiment(t *testing.T) {
	tr := &recordingTracker{}
	c := newExpClient(t, tr)
	// No identifier => not in experiment => no exposure.
	_ = c.For(Attributes{"plan": "pro"}).IsOn("feat")
	if n := len(tr.all()); n != 0 {
		t.Errorf("expected 0 exposures, got %d", n)
	}
}

func TestForContextPropagates(t *testing.T) {
	type ctxKey struct{}
	var seen any
	tr := trackerFunc(func(ctx context.Context, _ Exposure) { seen = ctx.Value(ctxKey{}) })
	c := newExpClient(t, tr)
	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	_ = c.ForContext(ctx, Attributes{"id": "user-1"}).IsOn("feat")
	if seen != "v" {
		t.Errorf("context value not propagated to Track: %v", seen)
	}
}

type trackerFunc func(context.Context, Exposure)

func (f trackerFunc) Track(ctx context.Context, e Exposure) { f(ctx, e) }
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -run 'TestExposure|TestForContext' .`
Expected: FAIL (`ForContext` undefined; exposures not fired; `Exposure` has no `HashAttribute`/`HashValue`).

- [ ] **Step 3: Extend `Exposure`**

In `track.go`, replace the `Exposure` struct:
```go
// Exposure records that a unit was exposed to a variation of an experiment.
// Field shape mirrors GrowthBook exposure logging so downstream analysis can be
// done by GrowthBook or by hand-written SQL.
type Exposure struct {
	ExperimentKey string
	VariationID   int
	HashAttribute string     // attribute used for bucketing, e.g. "id"
	HashValue     string     // the actual unit value bucketed (the join key)
	Attributes    Attributes // dimensions snapshot
	At            time.Time
}
```

- [ ] **Step 4: Add `ForContext` + per-binding fire-once to `client.go`**

In `client.go`, replace `For` and the `Evaluation` type + `result`:
```go
// For binds attributes for evaluation, using a background context for any
// exposure tracking.
func (c *Client) For(attrs Attributes) *Evaluation {
	return c.ForContext(context.Background(), attrs)
}

// ForContext binds attributes for evaluation. The context is passed to the
// Tracker when an experiment exposure fires, so tracing/cancellation flows
// through.
func (c *Client) ForContext(ctx context.Context, attrs Attributes) *Evaluation {
	return &Evaluation{client: c, ctx: ctx, attrs: attrs}
}

// Evaluation evaluates flags for a fixed set of attributes. It memoizes results
// per feature key for the lifetime of the binding, so an experiment fires at
// most one exposure per key per binding (not cross-unit dedup — that is the
// warehouse's job).
type Evaluation struct {
	client *Client
	ctx    context.Context
	attrs  Attributes

	mu     sync.Mutex
	cached map[string]Result
}

func (e *Evaluation) result(key string) Result {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cached != nil {
		if r, ok := e.cached[key]; ok {
			return r
		}
	}

	e.client.mu.RLock()
	feat, ok := e.client.features[key]
	e.client.mu.RUnlock()
	var r Result
	if ok {
		r = Evaluate(feat, key, e.attrs)
	}

	if r.InExperiment {
		e.client.tracker.Track(e.ctx, Exposure{
			ExperimentKey: feat.experimentKey(key, r),
			VariationID:   r.VariationID,
			HashAttribute: r.HashAttribute,
			HashValue:     r.HashValue,
			Attributes:    e.attrs,
			At:            time.Now(),
		})
	}

	if e.cached == nil {
		e.cached = make(map[string]Result)
	}
	e.cached[key] = r
	return r
}
```

Add the small helper to `evaluator.go` (the experiment key is the matched experiment rule's `Key`, falling back to the feature key):
```go
// experimentKey returns the experiment key recorded on an exposure: the Key of
// the experiment rule that produced the in-experiment result, or featureKey as
// a fallback.
func (f Feature) experimentKey(featureKey string, r Result) string {
	for _, rule := range f.Rules {
		if len(rule.Variations) >= 2 && rule.Key != "" {
			return rule.Key
		}
	}
	return featureKey
}
```

Confirm `client.go` already imports `context`, `sync`, and `time` (it does).

- [ ] **Step 5: Run the firing tests**

Run: `go test -run 'TestExposure|TestForContext' .`
Expected: PASS.

- [ ] **Step 6: Full suite + commit**

Run: `go test -race ./...`
Expected: PASS (race-clean — `Evaluation` mutates `cached` under its own mutex).
```bash
git add track.go client.go evaluator.go client_experiment_test.go
git commit -m "feat(experiments): enrich Exposure with hash unit; fire exposures (ForContext, per-binding)"
```

---

## Task 4: `trackpg` — async-batching Postgres tracker

**Files:**
- Create: `trackpg/trackpg.go`
- Create: `trackpg/schema.sql`
- Create: `trackpg/trackpg_test.go`
- Test: `trackpg/trackpg_test.go`

**Interfaces:**
- Consumes: `flagpole.Tracker`, `flagpole.Exposure` (Task 3); `pgxpool` (already a dep).
- Produces:
  - `trackpg.New(pool *pgxpool.Pool, opts ...Option) *Tracker`
  - Options: `WithBatchSize(int)`, `WithFlushInterval(time.Duration)`, `WithBufferSize(int)`, `WithTable(string)`, `WithOnError(func(error))`
  - `(*Tracker).Track(ctx, flagpole.Exposure)` (non-blocking), `(*Tracker).Close(ctx) error`, `(*Tracker).Dropped() int64`

- [ ] **Step 1: Write the canonical schema**

`trackpg/schema.sql`:
```sql
-- Canonical exposures table written by trackpg and read by downstream BI
-- (e.g. gnomon). This file is the contract: column names and types here are the
-- single source of truth. Running this DDL/migration is the consumer's job
-- (same stance as sourcepg).
CREATE TABLE experiment_exposures (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    experiment_key TEXT        NOT NULL,
    variation_id   INTEGER     NOT NULL,
    hash_attribute TEXT        NOT NULL,
    hash_value     TEXT        NOT NULL,
    attributes     JSONB       NOT NULL DEFAULT '{}',
    exposed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_experiment_exposures_key_time
    ON experiment_exposures (experiment_key, exposed_at);
```

- [ ] **Step 2: Write the failing DB-gated test**

`trackpg/trackpg_test.go`:
```go
package trackpg

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sudarkoff/flagpole"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("FLAGPOLE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLAGPOLE_TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), "TRUNCATE experiment_exposures"); err != nil {
		t.Fatalf("truncate (did you apply schema.sql?): %v", err)
	}
	return pool
}

func countRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM experiment_exposures").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestTrackBatchesAndFlushesOnClose(t *testing.T) {
	pool := testPool(t)
	tr := New(pool, WithBatchSize(100), WithFlushInterval(time.Hour)) // flush only on Close
	for i := 0; i < 5; i++ {
		tr.Track(context.Background(), flagpole.Exposure{
			ExperimentKey: "exp1",
			VariationID:   i % 2,
			HashAttribute: "id",
			HashValue:     "u",
			Attributes:    flagpole.Attributes{"plan": "pro"},
			At:            time.Now(),
		})
	}
	if err := tr.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if n := countRows(t, pool); n != 5 {
		t.Errorf("rows = %d, want 5", n)
	}
}

func TestTrackDropsOnOverflow(t *testing.T) {
	pool := testPool(t)
	// Tiny buffer, never flush until Close, so the queue overflows.
	tr := New(pool, WithBufferSize(2), WithBatchSize(1000), WithFlushInterval(time.Hour))
	for i := 0; i < 50; i++ {
		tr.Track(context.Background(), flagpole.Exposure{
			ExperimentKey: "exp1", HashAttribute: "id", HashValue: "u", At: time.Now(),
		})
	}
	if tr.Dropped() == 0 {
		t.Error("expected some dropped exposures under overflow")
	}
	if err := tr.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `FLAGPOLE_TEST_DATABASE_URL=postgres://... go test ./trackpg/...`
Expected: FAIL (`undefined: New`). (Without the env var the test SKIPs — set it to a real test DB with `schema.sql` applied to actually exercise it.)

- [ ] **Step 4: Implement `trackpg`**

`trackpg/trackpg.go`:
```go
// Package trackpg provides a Postgres-backed, async-batching flagpole.Tracker
// over an experiment_exposures table. See schema.sql for the canonical DDL —
// that schema is the contract downstream readers (e.g. gnomon) depend on.
package trackpg

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sudarkoff/flagpole"
)

// Tracker batches exposures and writes them to Postgres on a background
// goroutine. Track never blocks: when the buffer is full, exposures are dropped
// and counted (analytics data must never sit in the evaluation hot path).
type Tracker struct {
	pool          *pgxpool.Pool
	table         string
	batchSize     int
	flushInterval time.Duration
	onError       func(error)

	ch      chan flagpole.Exposure
	dropped atomic.Int64

	closeOnce sync.Once
	done      chan struct{}
	cancel    context.CancelFunc
}

// Option configures a Tracker.
type Option func(*Tracker)

// WithBatchSize sets the max rows per INSERT (default 100).
func WithBatchSize(n int) Option {
	return func(t *Tracker) {
		if n > 0 {
			t.batchSize = n
		}
	}
}

// WithFlushInterval sets the max delay before a partial batch is written
// (default 2s).
func WithFlushInterval(d time.Duration) Option {
	return func(t *Tracker) {
		if d > 0 {
			t.flushInterval = d
		}
	}
}

// WithBufferSize sets the in-memory queue depth (default 10000). When full,
// Track drops and counts the exposure.
func WithBufferSize(n int) Option {
	return func(t *Tracker) {
		if n > 0 {
			t.ch = make(chan flagpole.Exposure, n)
		}
	}
}

// WithTable overrides the table name (default "experiment_exposures"). The
// column contract is fixed by schema.sql.
func WithTable(name string) Option {
	return func(t *Tracker) {
		if name != "" {
			t.table = name
		}
	}
}

// WithOnError sets a callback invoked when a batch INSERT fails. Keep it cheap
// and non-blocking; it runs on the writer goroutine.
func WithOnError(fn func(error)) Option {
	return func(t *Tracker) {
		if fn != nil {
			t.onError = fn
		}
	}
}

var _ flagpole.Tracker = (*Tracker)(nil)

// New starts a Tracker and its background writer goroutine.
func New(pool *pgxpool.Pool, opts ...Option) *Tracker {
	t := &Tracker{
		pool:          pool,
		table:         "experiment_exposures",
		batchSize:     100,
		flushInterval: 2 * time.Second,
		onError:       func(error) {},
		ch:            make(chan flagpole.Exposure, 10000),
		done:          make(chan struct{}),
	}
	for _, o := range opts {
		o(t)
	}
	var ctx context.Context
	ctx, t.cancel = context.WithCancel(context.Background())
	go t.loop(ctx)
	return t
}

// Track enqueues an exposure. Non-blocking: a full buffer drops and counts it.
func (t *Tracker) Track(_ context.Context, e flagpole.Exposure) {
	select {
	case t.ch <- e:
	default:
		t.dropped.Add(1)
	}
}

// Dropped returns the number of exposures dropped due to a full buffer.
func (t *Tracker) Dropped() int64 { return t.dropped.Load() }

// Close stops accepting new exposures, flushes what is queued, and waits for the
// writer goroutine to finish.
func (t *Tracker) Close(ctx context.Context) error {
	t.closeOnce.Do(func() { t.cancel() })
	select {
	case <-t.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Tracker) loop(ctx context.Context) {
	defer close(t.done)
	ticker := time.NewTicker(t.flushInterval)
	defer ticker.Stop()
	batch := make([]flagpole.Exposure, 0, t.batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := t.write(context.Background(), batch); err != nil {
			t.onError(err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// Drain whatever is buffered, then flush and return.
			for {
				select {
				case e := <-t.ch:
					batch = append(batch, e)
					if len(batch) >= t.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case e := <-t.ch:
			batch = append(batch, e)
			if len(batch) >= t.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (t *Tracker) write(ctx context.Context, batch []flagpole.Exposure) error {
	rows := make([][]any, len(batch))
	for i, e := range batch {
		attrs, err := json.Marshal(e.Attributes)
		if err != nil {
			attrs = []byte("{}")
		}
		at := e.At
		if at.IsZero() {
			at = time.Now()
		}
		rows[i] = []any{e.ExperimentKey, e.VariationID, e.HashAttribute, e.HashValue, attrs, at}
	}
	_, err := t.pool.CopyFrom(ctx,
		pgx.Identifier{t.table},
		[]string{"experiment_key", "variation_id", "hash_attribute", "hash_value", "attributes", "exposed_at"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("trackpg: write batch of %d: %w", len(batch), err)
	}
	return nil
}
```

- [ ] **Step 5: Run the DB-gated tests**

Run: `FLAGPOLE_TEST_DATABASE_URL=postgres://... go test ./trackpg/...`
(Apply `trackpg/schema.sql` to that DB first.)
Expected: PASS — `TestTrackBatchesAndFlushesOnClose` writes 5 rows; `TestTrackDropsOnOverflow` reports a non-zero `Dropped()`.

- [ ] **Step 6: Build + vet + commit**

Run: `go build ./... && go vet ./...`
Expected: clean.
```bash
git add trackpg/trackpg.go trackpg/schema.sql trackpg/trackpg_test.go go.mod go.sum
git commit -m "feat(trackpg): async-batching Postgres exposure tracker + canonical schema"
```

---

## Task 5: Documentation

**Files:**
- Modify: `README.md`
- Modify: `USAGE.md`

**Interfaces:**
- Consumes: everything from Tasks 1–4.
- Produces: no code; user-facing docs aligned with the shipped behavior.

- [ ] **Step 1: Update the README**

In `README.md`:
1. **GrowthBook compatibility** section — move experiment `variations`/`weights`/`key` and experiment `coverage` and `condition` targeting from "Out of scope" to "Supported"; keep namespaces, `range`, `filters`, `parentConditions`, sticky/fallback, non-2 hash versions under "Out of scope".
2. **Experiment exposure tracking** section — document that an experiment is a rule with `Variations` (`len >= 2`); the feature's `Value`/`IsOn` resolves to the assigned variation; an exposure fires on every genuine assignment (no cross-unit dedup — BI takes first-exposure); `ForContext(ctx, attrs)` propagates a context to the tracker; the enriched `Exposure` carries `HashAttribute`/`HashValue`.
3. **Roadmap** — replace the "Phase B — experiments" bullet with a line noting experiments shipped, and (optionally) list remaining nice-to-haves (namespaces, sticky bucketing) as future work.

- [ ] **Step 2: Update USAGE with a `trackpg` cookbook**

In `USAGE.md`, add a section that:
1. Shows wiring `trackpg`: `tr := trackpg.New(pool, trackpg.WithOnError(...))`, `flagpole.New(ctx, src, flagpole.WithTracker(tr))`, and `defer tr.Close(ctx)`.
2. States the firing contract: fire-every-assignment, drop-on-overflow (`tr.Dropped()`), async batching off the hot path.
3. Documents that **`trackpg/schema.sql` is the canonical contract** — downstream readers (gnomon) target that exact, versioned DDL; neither library imports the other.
4. Shows a roll-your-own `Tracker` example for consumers not using Postgres (implement `Track(ctx, flagpole.Exposure)` against the same column shape).

- [ ] **Step 3: Sanity check + commit**

Run: `go test ./...` (docs-only change must not break anything) and re-read both docs for accuracy against the code.
```bash
git add README.md USAGE.md
git commit -m "docs: experiments, exposure firing semantics, and trackpg cookbook"
```

---

## Self-Review

**Spec coverage:**
- §1 experiment-rule schema (`len(Variations) >= 2`, coverage reinterpretation, weights/condition) → Task 2.
- §2 pure evaluator + `getBucketRanges`/`chooseVariation` + `Result` fields → Tasks 1, 2.
- §3 `ForContext` + per-binding fire-once + firing → Task 3.
- §4 `Exposure` hash unit → Task 3.
- §5 `trackpg` async batching + `schema.sql` → Task 4.
- §6 GrowthBook compat (run + bucket fixtures, skip unsupported) → Tasks 1, 2.
- §7 docs + gnomon contract → Task 5.
- §8 testing strategy → tests in Tasks 1–4.
- §9 out-of-scope items are not implemented (namespaces/sticky/range/filters/parentConditions stay skipped).

**Type consistency:** `Result` fields (`VariationID`, `InExperiment`, `HashAttribute`, `HashValue`) defined in Task 2 are consumed verbatim in Task 3. `Exposure` fields (Task 3) map 1:1 to `schema.sql` columns and `trackpg.write` (Task 4). `getBucketRanges`/`chooseVariation` signatures (Task 1) match their callers in Task 2. `trackpg.New`/options/`Close(ctx)`/`Dropped()` match the tests.

**Placeholder scan:** none — every code step contains complete code; every run step has an expected result.

**Downstream (not in this plan):** twocal adopts `trackpg` + adds `hash_*` columns via expand/contract migration; gnomon builds its reader against `schema.sql`.
</content>
