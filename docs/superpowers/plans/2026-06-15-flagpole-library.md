# flagpole Library Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `flagpole`, a generic, dependency-light Go feature-flag library whose evaluator is a strict drop-in for GrowthBook (exact hashing + a subset of its feature schema), with batteries-included Postgres storage and a mountable admin HTTP handler.

**Architecture:** A zero-dependency core (`package flagpole`) holds the GrowthBook-compatible hashing, the feature/rule schema, the pure evaluator, and a `Client` that caches definitions from a `Source` and refreshes them on a ticker. Optional subpackages add a Postgres `Source` (`sourcepg`) and an admin `http.Handler` (`adminhttp`). Drop-in compatibility is proven by running the evaluator against GrowthBook's published `cases.json` SDK test fixtures.

**Tech Stack:** Go 1.22+, `pgx/v5` (sourcepg only), standard `net/http` (adminhttp only). No third-party deps in the core.

**Scope of THIS plan:** the Go library, Phase A (flags). It is a complete, publishable artifact on its own (tag `v0.1.0`).

**Explicitly out of scope — separate follow-up plans:**
- `@flagpole/react` npm package (provider + hooks).
- twocal integration (User→Attributes, bootstrap endpoint, custops UI, wiring the real flags). The library must contain **zero** references to twocal.
- Phase B experiment *analysis* (metrics, significance). The schema fields and `Tracker` seam are built here; analysis is not.

**Source spec:** `twocal/docs/superpowers/specs/2026-06-15-flagpole-feature-flags-design.md`

---

## File Structure

Core package `flagpole` (module root `github.com/sudarkoff/flagpole`):

| File | Responsibility |
|------|----------------|
| `go.mod` / `go.sum` | Module definition |
| `attributes.go` | `Attributes` type |
| `feature.go` | `Feature`, `Rule` schema types (GrowthBook subset) |
| `hash.go` | GrowthBook FNV-1a v2 hashing |
| `condition.go` | Condition evaluation (equality + `$in`/`$eq`/`$ne`), errors on unknown operators |
| `evaluator.go` | Pure `Evaluate(feature, key, attrs)` |
| `source.go` | `Source` interface + `StaticSource` (in-memory / JSON payload) |
| `track.go` | `Tracker` interface, `Exposure`, no-op tracker |
| `client.go` | `Client` (cache + ticker refresh), `Evaluation`, `For`, `IsOn`, `Value` |

Subpackages:

| Path | Responsibility |
|------|----------------|
| `sourcepg/sourcepg.go` | Postgres-backed `Source` over a `feature_flags` table |
| `sourcepg/schema.sql` | Reference DDL (consumers run their own migrations) |
| `adminhttp/adminhttp.go` | Mountable `http.Handler` exposing JSON CRUD over a store |

Test/fixtures:

| Path | Responsibility |
|------|----------------|
| `testdata/cases.json` | GrowthBook's published SDK test suite (vendored) |
| `compat_test.go` | Runs the evaluator against `cases.json` |

---

## Task 1: Repo scaffolding

**Files:**
- Create: `go.mod`
- Create: `.github/workflows/ci.yml`
- Create: `.gitignore`
- Create: `README.md` (stub; fleshed out in Task 12)

- [ ] **Step 1: Initialize the module**

Run:
```bash
cd /Users/george/src/flagpole
go mod init github.com/sudarkoff/flagpole
go mod edit -go=1.22
```

- [ ] **Step 2: Add `.gitignore`**

Create `.gitignore`:
```
/dist/
*.out
*.test
.DS_Store
```

- [ ] **Step 3: Add CI workflow**

Create `.github/workflows/ci.yml`:
```yaml
name: ci
on:
  push:
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: flagpole_test
        ports: ["5432:5432"]
        options: >-
          --health-cmd "pg_isready -U postgres"
          --health-interval 5s --health-timeout 5s --health-retries 10
    env:
      FLAGPOLE_TEST_DATABASE_URL: postgres://postgres:postgres@localhost:5432/flagpole_test?sslmode=disable
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go vet ./...
      - run: go test -race ./...
```

- [ ] **Step 4: Add README stub**

Create `README.md`:
```markdown
# flagpole

A lightweight Go feature-flag library with a GrowthBook-compatible evaluator.

Status: pre-release. See `docs/superpowers/plans/`.
```

- [ ] **Step 5: Commit**

```bash
git add go.mod .gitignore .github README.md
git commit -m "chore: scaffold module, CI, readme"
```

---

## Task 2: GrowthBook v2 hashing

GrowthBook's deterministic bucketing. Algorithm (from the GrowthBook SDK spec):
`hash_v2(seed, value) = fnv32a( decimal_string( fnv32a(seed + value) ) ) % 10000 / 10000`,
where `fnv32a` matches JavaScript's `charCodeAt` iteration (UTF-16 code units).

**Files:**
- Create: `hash.go`
- Test: `hash_test.go`

- [ ] **Step 1: Write failing tests with known vectors**

Create `hash_test.go`:
```go
package flagpole

import "testing"

func TestFNV32a(t *testing.T) {
	// Reference values from GrowthBook's hashing spec.
	cases := map[string]uint32{
		"":  0x811c9dc5,
		"a": 0xe40c292c,
	}
	for in, want := range cases {
		if got := fnv32a(in); got != want {
			t.Errorf("fnv32a(%q) = %#x, want %#x", in, got, want)
		}
	}
}

func TestHashV2Structural(t *testing.T) {
	// Structural guarantees. Exact GrowthBook-vector equality is asserted in
	// Task 6 against the vendored cases.json (the authoritative oracle).
	for _, c := range []struct{ seed, value string }{
		{"", "a"}, {"", "b"}, {"a", "a"}, {"seed", "value"},
	} {
		got := hashV2(c.seed, c.value)
		if got < 0 || got >= 1 {
			t.Errorf("hashV2(%q,%q) = %v, want in [0,1)", c.seed, c.value, got)
		}
		if got != hashV2(c.seed, c.value) {
			t.Errorf("hashV2(%q,%q) is not deterministic", c.seed, c.value)
		}
	}
	// Different inputs should generally produce different buckets.
	if hashV2("", "a") == hashV2("", "b") {
		t.Error("expected distinct buckets for distinct values")
	}
}
```

> The exact GrowthBook hash vectors are asserted in Task 6's `TestCompatHash`, which runs against the vendored `testdata/cases.json` — GrowthBook's own published fixtures. That is the authoritative drop-in check; do not hand-transcribe expected floats here.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestFNV32a ./...`
Expected: FAIL — `undefined: fnv32a`.

- [ ] **Step 3: Implement the hashing**

Create `hash.go`:
```go
package flagpole

import (
	"strconv"
	"unicode/utf16"
)

// fnv32a matches JavaScript's FNV-1a over UTF-16 code units (String.charCodeAt),
// which is what GrowthBook's reference SDKs use.
func fnv32a(s string) uint32 {
	const prime = 0x01000193
	var h uint32 = 0x811c9dc5
	for _, u := range utf16.Encode([]rune(s)) {
		h ^= uint32(u)
		h *= prime
	}
	return h
}

// hashV2 is GrowthBook's hashVersion 2: a double FNV-1a producing a value in [0,1).
func hashV2(seed, value string) float64 {
	h := fnv32a(strconv.FormatUint(uint64(fnv32a(seed+value)), 10))
	return float64(h%10000) / 10000.0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestFNV32a|TestHashV2' ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hash.go hash_test.go
git commit -m "feat: GrowthBook-compatible v2 hashing"
```

---

## Task 3: Feature / Rule schema types

**Files:**
- Create: `attributes.go`
- Create: `feature.go`
- Test: `feature_test.go`

- [ ] **Step 1: Write failing test for JSON round-trip**

Create `feature_test.go`:
```go
package flagpole

import (
	"encoding/json"
	"testing"
)

func TestFeatureJSONRoundTrip(t *testing.T) {
	raw := `{
		"defaultValue": false,
		"rules": [
			{"condition": {"plan": "starter"}, "force": true},
			{"coverage": 0.5, "hashAttribute": "id", "force": true}
		]
	}`
	var f Feature
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.DefaultValue != false {
		t.Errorf("defaultValue = %v", f.DefaultValue)
	}
	if len(f.Rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(f.Rules))
	}
	if f.Rules[1].Coverage == nil || *f.Rules[1].Coverage != 0.5 {
		t.Errorf("coverage = %v", f.Rules[1].Coverage)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestFeatureJSONRoundTrip ./...`
Expected: FAIL — `undefined: Feature`.

- [ ] **Step 3: Implement the types**

Create `attributes.go`:
```go
package flagpole

// Attributes is the flat bag of values a flag is evaluated against.
// Identical in spirit to GrowthBook's attribute model.
type Attributes map[string]any
```

Create `feature.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestFeatureJSONRoundTrip ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add attributes.go feature.go feature_test.go
git commit -m "feat: feature/rule schema (GrowthBook subset)"
```

---

## Task 4: Condition evaluation

Supports the subset we use now: top-level field equality and the `$in`, `$eq`, `$ne` operators. Any other operator is an explicit error so we never silently mis-evaluate.

**Files:**
- Create: `condition.go`
- Test: `condition_test.go`

- [ ] **Step 1: Write failing tests**

Create `condition_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestCondition ./...`
Expected: FAIL — `undefined: matchCondition`.

- [ ] **Step 3: Implement condition matching**

Create `condition.go`:
```go
package flagpole

import "fmt"

// matchCondition evaluates a GrowthBook-style condition object against attrs.
// Supported: top-level field equality, and the $eq, $ne, $in operators.
// All fields are ANDed. Unsupported operators return an error.
func matchCondition(cond map[string]any, attrs Attributes) (bool, error) {
	for field, expected := range cond {
		actual := attrs[field]
		switch exp := expected.(type) {
		case map[string]any:
			ok, err := matchOperators(exp, actual)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		default:
			if !equalValues(actual, expected) {
				return false, nil
			}
		}
	}
	return true, nil
}

func matchOperators(ops map[string]any, actual any) (bool, error) {
	for op, want := range ops {
		switch op {
		case "$eq":
			if !equalValues(actual, want) {
				return false, nil
			}
		case "$ne":
			if equalValues(actual, want) {
				return false, nil
			}
		case "$in":
			list, ok := want.([]any)
			if !ok {
				return false, fmt.Errorf("flagpole: $in expects an array, got %T", want)
			}
			found := false
			for _, v := range list {
				if equalValues(actual, v) {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		default:
			return false, fmt.Errorf("flagpole: unsupported condition operator %q", op)
		}
	}
	return true, nil
}

// equalValues compares two values with JSON-ish semantics (numbers compared as float64).
func equalValues(a, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	return a == b
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run TestCondition ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add condition.go condition_test.go
git commit -m "feat: condition evaluation (equality, \$in/\$eq/\$ne)"
```

---

## Task 5: The evaluator

Evaluates a feature against attributes: walk rules in order; first matching rule wins. A rule matches if its condition passes AND (if it has coverage) the hashed unit falls under coverage. The rollout seed defaults to the rule's `Seed`, else the feature key.

**Files:**
- Create: `evaluator.go`
- Test: `evaluator_test.go`

- [ ] **Step 1: Write failing tests**

Create `evaluator_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestEvaluate ./...`
Expected: FAIL — `undefined: Evaluate`.

- [ ] **Step 3: Implement the evaluator**

Create `evaluator.go`:
```go
package flagpole

import "fmt"

// Result is the outcome of evaluating a feature.
type Result struct {
	Value any  // resolved value (force value or defaultValue)
	On    bool // truthiness of Value
}

// Evaluate resolves a feature for the given attributes. Rules are tried in
// order; the first one that fully matches wins. featureKey is used as the
// default rollout seed (matching GrowthBook).
func Evaluate(f Feature, featureKey string, attrs Attributes) Result {
	for _, rule := range f.Rules {
		if rule.Condition != nil {
			ok, err := matchCondition(rule.Condition, attrs)
			if err != nil {
				// A rule we cannot understand must not silently apply; skip it.
				continue
			}
			if !ok {
				continue
			}
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

func inCoverage(rule Rule, featureKey string, attrs Attributes) bool {
	hashAttr := rule.HashAttribute
	if hashAttr == "" {
		hashAttr = "id"
	}
	value := stringAttr(attrs[hashAttr])
	if value == "" {
		return false // no identifier => never in a partial rollout
	}
	seed := rule.Seed
	if seed == "" {
		seed = featureKey
	}
	return hashV2(seed, value) < *rule.Coverage
}

func stringAttr(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", s)
	}
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != "" && x != "false" && x != "0"
	case float64:
		return x != 0
	default:
		return true
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run TestEvaluate ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add evaluator.go evaluator_test.go
git commit -m "feat: feature evaluator (force, condition, coverage rollout)"
```

---

## Task 6: GrowthBook `cases.json` compatibility suite

The drop-in proof: run the evaluator and hashing against GrowthBook's published SDK fixtures. We assert the cases we support and explicitly skip the rest (experiments, unsupported operators) so unsupported behavior is never silently "passed."

**Files:**
- Create: `testdata/cases.json` (vendored)
- Create: `compat_test.go`

- [ ] **Step 1: Vendor the fixtures**

Run:
```bash
cd /Users/george/src/flagpole
curl -fsSL https://raw.githubusercontent.com/growthbook/growthbook/main/packages/sdk-js/test/cases.json -o testdata/cases.json
test -s testdata/cases.json && echo "fetched $(wc -c < testdata/cases.json) bytes"
```
Expected: a non-empty JSON file. If the path has moved, locate `cases.json` in the `growthbook/growthbook` repo under the JS SDK test directory and vendor that.

- [ ] **Step 2: Write the hash compatibility test**

`cases.json` has a top-level `"hash"` array of `[seed, value, version, result]`. Create `compat_test.go`:
```go
package flagpole

import (
	"encoding/json"
	"math"
	"os"
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
```

- [ ] **Step 3: Run the hash compat test**

Run: `go test -run TestCompatHash ./...`
Expected: PASS. If it fails, the hashing in Task 2 is wrong — fix `hash.go` until GrowthBook's own vectors pass. Do not weaken the tolerance.

- [ ] **Step 4: Write the feature-evaluation compatibility test**

`cases.json` has a `"feature"` array of `[name, context, key, expectedResult]` where `context` includes `attributes` and `features`, and `expectedResult` has `value`, `on`, `source`, etc. Add to `compat_test.go`:
```go
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
	}
}

// usesUnsupported reports whether a feature relies on rule kinds outside this
// library's Phase-A subset (experiment variations, or condition operators we
// don't implement).
func usesUnsupported(f Feature) bool {
	for _, r := range f.Rules {
		if len(r.Variations) > 0 {
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
```

- [ ] **Step 5: Run the feature compat test**

Run: `go test -run TestCompatFeatures -v ./...`
Expected: PASS, with `skip unsupported case` logs for experiment/operator cases. If a *supported* case fails, fix the evaluator — not the test.

- [ ] **Step 6: Commit**

```bash
git add testdata/cases.json compat_test.go
git commit -m "test: GrowthBook cases.json drop-in compatibility suite"
```

---

## Task 7: Source interface + static source

**Files:**
- Create: `source.go`
- Test: `source_test.go`

- [ ] **Step 1: Write failing test**

Create `source_test.go`:
```go
package flagpole

import (
	"context"
	"testing"
)

func TestStaticSourceFromJSON(t *testing.T) {
	payload := `{"features": {"f1": {"defaultValue": true}}}`
	src, err := StaticSourceFromJSON([]byte(payload))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	feats, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := feats["f1"]; !ok {
		t.Fatalf("missing feature f1")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestStaticSource ./...`
Expected: FAIL — `undefined: StaticSourceFromJSON`.

- [ ] **Step 3: Implement source**

Create `source.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestStaticSource ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add source.go source_test.go
git commit -m "feat: Source interface + static/JSON source"
```

---

## Task 8: Tracker interface

**Files:**
- Create: `track.go`
- Test: `track_test.go`

- [ ] **Step 1: Write failing test**

Create `track_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestNoopTracker ./...`
Expected: FAIL — `undefined: Tracker`.

- [ ] **Step 3: Implement tracker**

Create `track.go`:
```go
package flagpole

import (
	"context"
	"time"
)

// Exposure records that a unit was exposed to a variation of an experiment.
// Field shape mirrors GrowthBook exposure logging so downstream analysis can be
// done by GrowthBook or by hand-written SQL.
type Exposure struct {
	ExperimentKey string
	VariationID   int
	Attributes    Attributes
	At            time.Time
}

// Tracker records experiment exposures. Phase A ships only the no-op; consumers
// supply a persistent implementation for Phase B.
type Tracker interface {
	Track(ctx context.Context, e Exposure)
}

// NoopTracker discards exposures.
type NoopTracker struct{}

func (NoopTracker) Track(context.Context, Exposure) {}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestNoopTracker ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add track.go track_test.go
git commit -m "feat: Tracker interface + no-op (Phase B seam)"
```

---

## Task 9: Client with cache + ticker refresh

**Files:**
- Create: `client.go`
- Test: `client_test.go`

- [ ] **Step 1: Write failing tests**

Create `client_test.go`:
```go
package flagpole

import (
	"context"
	"testing"
)

func TestClientEvaluate(t *testing.T) {
	src := StaticSource{Features: map[string]Feature{
		"f1": {DefaultValue: false, Rules: []Rule{
			{Condition: map[string]any{"plan": "starter"}, Force: true},
		}},
		"greeting": {DefaultValue: "hello"},
	}}
	c, err := New(context.Background(), src)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if !c.For(Attributes{"id": "u1", "plan": "starter"}).IsOn("f1") {
		t.Error("f1 should be on for starter")
	}
	if c.For(Attributes{"id": "u1", "plan": "free"}).IsOn("f1") {
		t.Error("f1 should be off for free")
	}
	if got := c.For(Attributes{"id": "u1"}).Value("greeting", "x"); got != "hello" {
		t.Errorf("greeting = %v, want hello", got)
	}
	if got := c.For(Attributes{"id": "u1"}).Value("missing", "fallback"); got != "fallback" {
		t.Errorf("missing = %v, want fallback", got)
	}
}

func TestClientUnknownFlagIsOff(t *testing.T) {
	c, err := New(context.Background(), StaticSource{Features: map[string]Feature{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if c.For(Attributes{"id": "u1"}).IsOn("nope") {
		t.Error("unknown flag must be off")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestClient ./...`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Implement the client**

Create `client.go`:
```go
package flagpole

import (
	"context"
	"sync"
	"time"
)

// Client caches feature definitions from a Source and evaluates them locally.
// It is safe for concurrent use.
type Client struct {
	src     Source
	tracker Tracker

	mu       sync.RWMutex
	features map[string]Feature

	refresh time.Duration
	cancel  context.CancelFunc
	done    chan struct{}
}

// Option configures a Client.
type Option func(*Client)

// WithRefreshInterval sets how often the Client reloads from its Source.
// Zero or negative disables background refresh (load once).
func WithRefreshInterval(d time.Duration) Option {
	return func(c *Client) { c.refresh = d }
}

// WithTracker sets the experiment exposure tracker (default: NoopTracker).
func WithTracker(tr Tracker) Option {
	return func(c *Client) { c.tracker = tr }
}

// New loads features once synchronously, then (unless disabled) refreshes them
// on an interval until Close is called.
func New(ctx context.Context, src Source, opts ...Option) (*Client, error) {
	c := &Client{
		src:     src,
		tracker: NoopTracker{},
		refresh: 60 * time.Second,
		done:    make(chan struct{}),
	}
	for _, o := range opts {
		o(c)
	}
	if err := c.reload(ctx); err != nil {
		return nil, err
	}
	if c.refresh > 0 {
		var bg context.Context
		bg, c.cancel = context.WithCancel(context.Background())
		go c.loop(bg)
	} else {
		close(c.done)
	}
	return c, nil
}

func (c *Client) reload(ctx context.Context) error {
	feats, err := c.src.Load(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.features = feats
	c.mu.Unlock()
	return nil
}

func (c *Client) loop(ctx context.Context) {
	defer close(c.done)
	t := time.NewTicker(c.refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = c.reload(ctx) // keep serving stale on transient errors
		}
	}
}

// Close stops background refresh.
func (c *Client) Close() {
	if c.cancel != nil {
		c.cancel()
	}
	<-c.done
}

// For binds attributes for evaluation.
func (c *Client) For(attrs Attributes) *Evaluation {
	return &Evaluation{client: c, attrs: attrs}
}

// Evaluation evaluates flags for a fixed set of attributes.
type Evaluation struct {
	client *Client
	attrs  Attributes
}

func (e *Evaluation) result(key string) Result {
	e.client.mu.RLock()
	feat, ok := e.client.features[key]
	e.client.mu.RUnlock()
	if !ok {
		return Result{Value: nil, On: false}
	}
	return Evaluate(feat, key, e.attrs)
}

// IsOn reports whether the flag resolves to a truthy value. Unknown flags are off.
func (e *Evaluation) IsOn(key string) bool { return e.result(key).On }

// Value returns the flag's resolved value, or def if unknown/nil.
func (e *Evaluation) Value(key string, def any) any {
	e.client.mu.RLock()
	_, ok := e.client.features[key]
	e.client.mu.RUnlock()
	if !ok {
		return def
	}
	v := e.result(key).Value
	if v == nil {
		return def
	}
	return v
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestClient ./...`
Expected: PASS, no race warnings.

- [ ] **Step 5: Commit**

```bash
git add client.go client_test.go
git commit -m "feat: Client with cache + ticker refresh"
```

---

## Task 10: Postgres source adapter (`sourcepg`)

**Files:**
- Create: `sourcepg/sourcepg.go`
- Create: `sourcepg/schema.sql`
- Test: `sourcepg/sourcepg_test.go`

- [ ] **Step 1: Add pgx dependency**

Run:
```bash
cd /Users/george/src/flagpole
go get github.com/jackc/pgx/v5@latest
```

- [ ] **Step 2: Reference schema**

Create `sourcepg/schema.sql`:
```sql
-- Reference DDL. Consumers run this via their own migration tooling.
CREATE TABLE IF NOT EXISTS feature_flags (
    key         TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    definition  JSONB NOT NULL,
    archived    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

- [ ] **Step 3: Write failing integration test**

Create `sourcepg/sourcepg_test.go`:
```go
package sourcepg

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	_, _ = pool.Exec(context.Background(), "TRUNCATE feature_flags")
	return pool
}

func TestSourceLoadSkipsArchived(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO feature_flags (key, definition, archived) VALUES
		 ('live', '{"defaultValue": true}', false),
		 ('dead', '{"defaultValue": true}', true)`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := New(pool)
	feats, err := src.Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := feats["live"]; !ok {
		t.Error("expected live flag")
	}
	if _, ok := feats["dead"]; ok {
		t.Error("archived flag must not load")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./sourcepg/...`
Expected: FAIL — `undefined: New` (or SKIP if no DB URL; set `FLAGPOLE_TEST_DATABASE_URL` to a local Postgres to actually exercise it).

- [ ] **Step 5: Implement the adapter**

Create `sourcepg/sourcepg.go`:
```go
// Package sourcepg provides a Postgres-backed flagpole.Source over a
// feature_flags table. See schema.sql for the expected DDL.
package sourcepg

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sudarkoff/flagpole"
)

// Source loads non-archived feature definitions from Postgres.
type Source struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Source { return &Source{pool: pool} }

func (s *Source) Load(ctx context.Context) (map[string]flagpole.Feature, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key, definition FROM feature_flags WHERE archived = false`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]flagpole.Feature)
	for rows.Next() {
		var key string
		var def []byte
		if err := rows.Scan(&key, &def); err != nil {
			return nil, err
		}
		var f flagpole.Feature
		if err := json.Unmarshal(def, &f); err != nil {
			return nil, err
		}
		out[key] = f
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `FLAGPOLE_TEST_DATABASE_URL=postgres://... go test ./sourcepg/...`
Expected: PASS (or SKIP without a DB).

- [ ] **Step 7: Commit**

```bash
git add sourcepg go.mod go.sum
git commit -m "feat: Postgres source adapter"
```

---

## Task 11: Admin HTTP handler (`adminhttp`)

A mountable, auth-agnostic `http.Handler` exposing JSON CRUD over a small store interface. The host wraps it with its own auth middleware.

**Files:**
- Create: `adminhttp/adminhttp.go`
- Test: `adminhttp/adminhttp_test.go`

- [ ] **Step 1: Write failing test**

Create `adminhttp/adminhttp_test.go`:
```go
package adminhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sudarkoff/flagpole"
)

// memStore is an in-memory Store for testing.
type memStore struct {
	mu   sync.Mutex
	defs map[string]flagpole.Feature
}

func (m *memStore) List(context.Context) (map[string]flagpole.Feature, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]flagpole.Feature, len(m.defs))
	for k, v := range m.defs {
		cp[k] = v
	}
	return cp, nil
}

func (m *memStore) Upsert(_ context.Context, key string, f flagpole.Feature) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defs[key] = f
	return nil
}

func (m *memStore) Archive(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.defs, key)
	return nil
}

func TestUpsertThenList(t *testing.T) {
	store := &memStore{defs: map[string]flagpole.Feature{}}
	h := NewHandler(store)

	// Upsert
	body := `{"defaultValue": false, "rules": [{"force": true, "coverage": 0.5}]}`
	req := httptest.NewRequest(http.MethodPut, "/flags/new-flag", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status = %d", rec.Code)
	}

	// List
	req = httptest.NewRequest(http.MethodGet, "/flags", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "new-flag") {
		t.Errorf("list missing new-flag: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./adminhttp/...`
Expected: FAIL — `undefined: NewHandler`.

- [ ] **Step 3: Implement the handler**

Create `adminhttp/adminhttp.go`:
```go
// Package adminhttp exposes a mountable, auth-agnostic JSON CRUD handler for
// managing flagpole feature definitions. Wrap it with your own auth middleware.
package adminhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sudarkoff/flagpole"
)

// Store is the persistence the admin handler operates on. A Postgres-backed
// implementation typically lives next to your sourcepg setup.
type Store interface {
	List(ctx context.Context) (map[string]flagpole.Feature, error)
	Upsert(ctx context.Context, key string, f flagpole.Feature) error
	Archive(ctx context.Context, key string) error
}

// NewHandler returns an http.Handler serving:
//   GET    /flags          -> {key: Feature}
//   PUT    /flags/{key}     -> upsert (body = Feature JSON)
//   DELETE /flags/{key}     -> archive
func NewHandler(store Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/flags", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defs, err := store.List(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, defs)
	})
	mux.HandleFunc("/flags/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/flags/")
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			var f flagpole.Feature
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := store.Upsert(r.Context(), key, f); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			if err := store.Archive(r.Context(), key); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./adminhttp/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add adminhttp
git commit -m "feat: mountable admin HTTP handler"
```

---

## Task 12: Docs + first tag

**Files:**
- Modify: `README.md`
- Create: `doc.go`

- [ ] **Step 1: Write package doc**

Create `doc.go`:
```go
// Package flagpole evaluates feature flags locally using a GrowthBook-compatible
// algorithm: the same FNV-1a v2 hashing and a strict subset of GrowthBook's
// feature schema, so definitions and bucketing port to GrowthBook unchanged.
//
// Typical use:
//
//	c, _ := flagpole.New(ctx, src) // src is a Source (e.g. sourcepg.New(pool))
//	defer c.Close()
//	if c.For(flagpole.Attributes{"id": userID, "plan": plan}).IsOn("my-flag") {
//	    // ...
//	}
package flagpole
```

- [ ] **Step 2: Flesh out the README**

Replace `README.md` with usage covering: install, the `Attributes` model, `Source`/`Client`, `sourcepg`, `adminhttp`, and a "GrowthBook compatibility" section noting the `cases.json` suite and the supported subset (force, coverage rollout, equality/`$in`/`$eq`/`$ne`). Keep it generic — no references to any specific consuming app.

- [ ] **Step 3: Full test sweep**

Run: `go vet ./... && go test -race ./...`
Expected: PASS across core + subpackages (sourcepg may SKIP without a DB).

- [ ] **Step 4: Commit and tag**

```bash
git add README.md doc.go
git commit -m "docs: package doc + README"
git tag v0.1.0
```

> Push (`git push && git push --tags`) only when the user asks.

---

## Follow-up plans (not this plan)

1. **`@flagpole/react`** — `FlagsProvider` + `useFeatureIsOn`/`useFeatureValue`, hydrated from a host-supplied payload. Lives in this repo (e.g. `react/`) or its own; published to npm.
2. **twocal integration** — `User→Attributes`, bootstrap endpoint embedding evaluated flags, `Client` in API + worker via `sourcepg`, a Postgres `Store` for `adminhttp`, custops admin UI, an `experiment_exposures` table + a persistent `Tracker`, and wiring `skip-on-sync` + `merge-conflict-resolution`. Written against flagpole `v0.1.0`.
