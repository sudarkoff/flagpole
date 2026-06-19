# flagpole Phase B — Experiments — Design Spec

**Date:** 2026-06-19
**Status:** Approved, pending implementation plan
**Source spec (Phase A):** see twocal's `docs/superpowers/specs/2026-06-15-flagpole-feature-flags-design.md` §6 (Phase B — designed-for, not built)

---

## Scope

Phase A shipped feature-flag rollout control plus the dormant experiment seam
(`Tracker`, `Exposure`, `WithTracker`, and the `Key`/`Variations`/`Weights`
fields on `Rule`). Nothing in flagpole currently **assigns** an experiment
variation or **fires** an exposure — `variations`/`weights`/`key` are parsed but
skipped.

Phase B makes flagpole actually run experiments and emit the exposure data a BI
layer needs. The analysis itself (metrics, lift, significance) lives in a
**separate, independent library (`gnomon`)** and is explicitly **out of scope
here**. flagpole stays a generic, reusable library that knows nothing about any
consumer's tables.

**In scope:**

- **Experiment evaluation** — GrowthBook-compatible variation assignment for an
  "experiment rule" (a `Rule` carrying `Variations`), surfaced through the
  existing feature-centric `Value`/`IsOn` API (experiment-as-rule).
- **Exposure emission** — fire `tracker.Track` on every genuine assignment, with
  an enriched `Exposure` that carries the explicit bucketing unit.
- **`trackpg`** — a batteries-included, async-batching Postgres `Tracker` plus a
  canonical `trackpg/schema.sql` that is the published contract `gnomon` reads.
- **GrowthBook compatibility** for the supported experiment subset, validated
  against the vendored `cases.json` fixtures.
- **Docs** (README/USAGE) covering experiment rules, firing semantics, `trackpg`,
  and the gnomon contract.

**Out of scope (named so the schema never contradicts them):**

- Namespaces (mutually-exclusive experiments).
- Sticky / fallback bucketing.
- `range` / `filters` / `parentConditions`; hash versions other than 2.
- All metric / lift / significance analysis — that is `gnomon`.
- twocal wiring (replacing twocal's bespoke tracker with `trackpg`, the
  expand/contract migration adding the `hash_*` columns) — a **downstream
  consumer task**, tracked separately.

---

## Decisions of record

Settled during brainstorming; these override defaults:

1. **flagpole stays generic and independent.** It owns the experiment→exposure
   path; BI lives in `gnomon`, which only *reads* exposures.
2. **Experiment-as-rule (feature-centric).** No standalone `run(experiment)` API;
   an experiment is a rule inside a `Feature`, and the feature's resolved
   `Value`/`IsOn` becomes the assigned variation's value.
3. **Explicit hash unit in the exposure.** `Exposure` gains `HashAttribute` +
   `HashValue` (the join key). The variation value/name is a runtime concern and
   is **not** part of the analysis contract; `Attributes` remains the dimensions
   snapshot. This matches GrowthBook's warehouse "assignment query" shape (the
   minimal `{timestamp, identifier, experiment_id, variation_id, + dimensions}`),
   not the richer SDK `trackingCallback` payload.
4. **Fire every assignment, no dedup.** Emit on every genuine hash-based
   assignment; BI takes first-exposure-per-unit (the GrowthBook warehouse
   convention). No cross-unit dedup state in a long-lived server client.
5. **Pure evaluator stays pure (Approach A).** `Evaluate` gains experiment logic
   and returns richer metadata but remains side-effect-free and context-free, so
   the `cases.json` compat tests keep working. The side effect (`tracker.Track`)
   happens only in the stateful `Evaluation`/`Client` layer that already holds the
   tracker.
6. **`trackpg` ships, async-batching, with `trackpg/schema.sql` as the canonical
   contract.** Mirrors `sourcepg`: pgx confined to the subpackage, core stays
   zero-dep. `gnomon`'s reader references that exact, versioned DDL — the
   documented mitigation for cross-library schema drift.
7. **Supported experiment-rule subset:** `weights`, experiment `coverage`,
   `condition` targeting. Namespaces deferred.

---

## 1. Schema (`feature.go`) — activation, no new fields

`Rule` already carries `Key`, `Variations`, `Weights`. No struct changes needed
for the experiment definition.

- **A rule is an experiment rule iff `len(Variations) >= 2`.**
- For an experiment rule:
  - `Coverage` reinterprets as **experiment coverage** — the fraction of matched
    units admitted *into* the experiment, combined with weights (GrowthBook
    semantics). Units outside coverage are *not in experiment* and fall through.
  - For a non-experiment rule, `Coverage` keeps its current meaning (a simple
    rollout gate). The presence of `Variations` switches the interpretation.
  - `Weights` optional; default to an equal split (`1/n`) when omitted, the wrong
    length, or not summing to ~1 (normalize/equalize per GrowthBook).
  - `Condition` reused for targeting (existing supported subset).
  - `HashAttribute` (default `id`), `Seed` (default the rule's `Key`),
    `HashVersion` (2 only) as today.
  - `Range` / `Filters` / `ParentConditions` remain parsed-but-skipped: a rule
    requesting an unsupported feature is **skipped**, never mis-evaluated.

---

## 2. Evaluator (`evaluator.go`) — assignment, stays pure

- **`Result` gains experiment metadata** (existing `Value`/`On` unchanged for
  current callers):
  ```go
  type Result struct {
      Value any
      On    bool
      // Experiment metadata (zero-valued for non-experiment results):
      VariationID   int    // assigned variation index; meaningful only when InExperiment
      InExperiment  bool   // true only on a genuine hash-based assignment
      HashAttribute string // attribute used for bucketing
      HashValue     string // the actual unit value bucketed
  }
  ```
- **New pure helpers** mirroring GrowthBook:
  - `getBucketRanges(n int, coverage float64, weights []float64) [][2]float64` —
    cumulative `[start,end)` ranges, each scaled by `coverage`; equalizes weights
    when absent/invalid.
  - `chooseVariation(bucket float64, ranges [][2]float64) int` — index of the
    range containing `bucket`, else `-1`.
- **In `Evaluate`**, when a rule matches its `Condition` **and**
  `len(Variations) >= 2`:
  - Resolve `hashAttribute`/`hashValue`; if `hashValue == ""` → not in experiment,
    fall through.
  - `bucket = hashV2(seed, hashValue)` where `seed` defaults to the rule `Key`.
  - `i = chooseVariation(bucket, getBucketRanges(len(Variations), coverage, weights))`.
  - `i >= 0` → return `Result{Value: Variations[i], On: truthy(Variations[i]),
    VariationID: i, InExperiment: true, HashAttribute, HashValue}`.
  - `i == -1` (outside coverage) → fall through to the next rule / default with
    `InExperiment: false`.
- **Purity preserved:** `Evaluate` still takes no context and no tracker and has
  no side effects. Existing `cases.json` compat tests keep working; experiment
  fixtures are added in §6.

---

## 3. Firing layer (`client.go`) — the only side effect

- Add **`ForContext(ctx context.Context, attrs Attributes) *Evaluation`**;
  `For(attrs)` delegates with `context.Background()`. `Evaluation` stores the
  context.
- `Evaluation.result(key)` (shared by `IsOn` and `Value`): after `Evaluate`, if
  `InExperiment` → `tracker.Track(ctx, Exposure{...})` populated from the result.
- **Fire-once per binding:** `Evaluation` keeps a small request-scoped
  `map[string]bool` of feature keys already fired (and memoizes the `Result`), so
  calling both `IsOn(k)` and `Value(k)` on one `For(...)` fires exactly one
  exposure and returns a consistent variation. This is *not* the cross-unit dedup
  we rejected — it is bounded by the keys touched in a single binding.

---

## 4. `Exposure` (`track.go`)

```go
type Exposure struct {
    ExperimentKey string     // the experiment rule's Key
    VariationID   int
    HashAttribute string     // e.g. "id"            ← new
    HashValue     string     // the actual unit value ← new (the join key)
    Attributes    Attributes // dimensions snapshot
    At            time.Time
}
```

`NoopTracker` unchanged. The shape is the contract `gnomon` reads, and maps 1:1
to `trackpg/schema.sql` (§5).

---

## 5. `trackpg` adapter (new subpackage)

- `trackpg.New(pool *pgxpool.Pool, opts ...Option) *Tracker` implementing
  `flagpole.Tracker`. pgx confined to the subpackage (like `sourcepg`); the core
  package stays zero-dep.
- **Async batching:** `Track` enqueues onto a buffered channel; a background
  goroutine batch-INSERTs on batch-size or flush-interval, whichever first.
  `Close(ctx)` drains and flushes.
- **Overflow policy:** best-effort **drop on full buffer**, incrementing a
  dropped counter surfaced via an `OnError`/metrics hook. `Track` never blocks the
  caller — exposures are analytics and must never sit in the evaluation/sync hot
  path.
- **Options:** `WithBatchSize`, `WithFlushInterval`, `WithBufferSize`,
  `WithOnError` (receives flush errors and a dropped-count signal).
- **`trackpg/schema.sql`** — the canonical exposures table and the published
  contract for `gnomon`. Columns map 1:1 to `Exposure`:
  ```sql
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
  flagpole ships the DDL; **running migrations stays the consumer's job** (same
  stance as `sourcepg`). The table name is the recommended default; consumers may
  override via a `WithTable` option if needed (the column contract is what
  `gnomon` depends on).

---

## 6. GrowthBook compatibility (`compat_test.go`)

Extend the `cases.json` harness to run GrowthBook's `run`/experiment fixtures for
the supported subset (weights, experiment coverage, condition, hashVersion 2).
Unsupported fixtures (namespaces, ranges, filters, sticky/fallback,
parentConditions, non-2 hash versions) are **explicitly skipped and tracked**,
never silently passed — preserving the "strict, tested drop-in" guarantee.

---

## 7. Docs & the gnomon contract

- **README:** flip the Roadmap "Phase B — experiments" item to shipped; document
  experiment rules, the firing semantics ("fire every assignment; BI takes
  first-exposure"), `ForContext`, and the supported/unsupported experiment subset.
- **USAGE:** a `trackpg` cookbook; publish `trackpg/schema.sql` as the contract.
  State explicitly that **`trackpg/schema.sql` is the single source of truth**;
  `gnomon`'s reader references that exact, versioned DDL. This is the documented
  mitigation for cross-library drift (neither library imports the other, to keep
  both independent).

---

## 8. Testing strategy

- **Pure evaluator unit tests:** `getBucketRanges` / `chooseVariation`, weight
  normalization/equalization, coverage admission, assignment determinism, and the
  `hashValue == ""` → not-in-experiment path.
- **`client_test.go`:** exposure fires once per binding; carries the correct
  `HashAttribute`/`HashValue`; does not fire when not in experiment; `ForContext`
  propagates the context; `IsOn`+`Value` agree on the variation.
- **`trackpg_test.go`:** DB-gated (`FLAGPOLE_TEST_DATABASE_URL`) — batch insert,
  flush-on-close, drop-on-overflow counter.
- **`compat_test.go`:** experiment fixtures from `cases.json`.

---

## 9. Open follow-ups (not this effort)

- twocal: adopt `trackpg`, retire its bespoke `internal/flags` tracker, and run an
  expand/contract migration to add `hash_attribute`/`hash_value` to its existing
  `experiment_exposures` table.
- `gnomon`: build the reader adapter against `trackpg/schema.sql` and the
  metric/lift/significance analysis.
- Possible later flagpole work: namespaces, sticky bucketing, broader condition
  operators — each additive and schema-compatible by construction.
</content>
