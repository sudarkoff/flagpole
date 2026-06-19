# flagpole

A lightweight Go feature-flag library with a **GrowthBook-compatible** local evaluator.

Flags are evaluated in-process — no per-flag network call and no external service in the hot path. Feature definitions use the same JSON schema as [GrowthBook](https://www.growthbook.io/), and bucketing uses the same FNV-1a v2 hash, so a flag written for flagpole ports to GrowthBook (and back) unchanged, and the **same users stay in the same cohorts** if you ever switch.

```go
c, _ := flagpole.New(ctx, src)
defer c.Close()

if c.For(flagpole.Attributes{"id": userID, "plan": "pro"}).IsOn("new-checkout") {
    // ship the new thing to this user
}
```

> **New here?** This README is the reference. For a task-oriented walkthrough —
> wiring flagpole into a real service, attribute design, rollout recipes,
> runtime flag management, staleness alerting, frontend hydration, and a
> GrowthBook migration guide — see **[USAGE.md](USAGE.md)**, a cookbook drawn
> from a production Go API + worker integration.

---

## Contents

- [Usage cookbook (USAGE.md)](USAGE.md)
- [Why flagpole](#why-flagpole)
- [Install](#install)
- [Quick start](#quick-start)
- [Concepts](#concepts)
- [Targeting & rollout](#targeting--rollout)
- [Evaluation API](#evaluation-api)
- [Sources](#sources)
- [The Client](#the-client)
- [Admin HTTP handler](#admin-http-handler)
- [Experiment exposure tracking](#experiment-exposure-tracking)
- [GrowthBook compatibility](#growthbook-compatibility)
- [How it works](#how-it-works)
- [Testing](#testing)
- [React bindings](#react-bindings)
- [Roadmap](#roadmap)
- [License](#license)

---

## Why flagpole

- **Local evaluation.** The client loads all flag definitions once and refreshes them on an interval; every `IsOn`/`Value` call is a pure in-memory computation. Nothing flagpole-shaped ever sits in your request or job hot path.
- **Deterministic, consistent bucketing.** Percentage rollouts hash `hash(seed + attribute)` with GrowthBook's exact FNV-1a v2 algorithm. The same identifier always lands in the same bucket — across processes, across restarts, and across a migration to or from GrowthBook.
- **No new infrastructure required.** Bring any `Source` (a static JSON payload, your own loader, or the included Postgres adapter). No daemon, no sidecar.
- **Batteries included.** A Postgres-backed source (`sourcepg`) and a mountable admin CRUD handler (`adminhttp`) ship in the box, while the core package stays dependency-free.
- **Honest about its scope.** flagpole implements a *strict, tested subset* of GrowthBook. Anything outside that subset is **skipped, never silently mis-evaluated**, and compatibility is verified against GrowthBook's own published test fixtures.

The core package (`flagpole`) has **zero third-party dependencies**. The `sourcepg` adapter pulls in `pgx`; the `adminhttp` handler uses only the standard library.

## Install

```
go get github.com/sudarkoff/flagpole
```

Requires Go 1.25+.

## Quick start

### Static source (testing, or a payload fetched out of band)

```go
import "github.com/sudarkoff/flagpole"

payload := []byte(`{
  "features": {
    "dark-mode": {
      "defaultValue": false,
      "rules": [
        {"condition": {"plan": "pro"}, "force": true}
      ]
    }
  }
}`)

src, err := flagpole.StaticSourceFromJSON(payload)
if err != nil {
    log.Fatal(err)
}

c, err := flagpole.New(ctx, src)
if err != nil {
    log.Fatal(err)
}
defer c.Close()

attrs := flagpole.Attributes{"id": "user-123", "plan": "pro"}
if c.For(attrs).IsOn("dark-mode") {
    // flag is on for this user
}
```

### Postgres source

```go
import (
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/sudarkoff/flagpole"
    "github.com/sudarkoff/flagpole/sourcepg"
)

pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))

c, err := flagpole.New(ctx, sourcepg.New(pool),
    flagpole.WithRefreshInterval(30*time.Second),
)
if err != nil {
    log.Fatal(err)
}
defer c.Close()

ev := c.For(flagpole.Attributes{"id": userID, "plan": plan})
if ev.IsOn("new-checkout") {
    // ...
}
color := ev.Value("button-color", "blue") // "blue" if the flag is unknown
```

## Concepts

### Attributes

`Attributes` is a flat `map[string]any` describing the unit you're evaluating for (usually a user). Keys are arbitrary and are referenced by your flag definitions' `hashAttribute` and `condition` fields. The default hash attribute used for percentage rollouts (when a rule doesn't set `hashAttribute`) is `"id"`.

```go
attrs := flagpole.Attributes{
    "id":     "user-abc",   // default bucketing key
    "plan":   "pro",
    "country": "US",
    "beta":   true,
    "roles":  []any{"admin", "billing"},
}
```

Numbers from JSON arrive as `float64`; flagpole compares numbers numerically so an `int` attribute still matches a JSON number in a condition.

### Feature & Rule

A **Feature** is one flag: a `defaultValue` plus an ordered list of `rules`. Rules are evaluated top to bottom and **the first matching rule wins**; if none match, `defaultValue` is returned.

```go
type Feature struct {
    DefaultValue any
    Rules        []Rule
}
```

A **Rule** can force a value, gate on a condition, and/or apply a percentage rollout. The fields flagpole evaluates:

| Field | JSON | Meaning |
|-------|------|---------|
| `Condition` | `condition` | Targeting object (subset of GrowthBook conditions). Rule applies only if it matches. |
| `Force` | `force` | The value to return when the rule applies. If omitted, `defaultValue` is used. |
| `Coverage` | `coverage` | Percentage rollout in `[0,1]`. The rule applies only if the hashed unit falls under this fraction. |
| `HashAttribute` | `hashAttribute` | Attribute used for rollout bucketing (default `"id"`). |
| `Seed` | `seed` | Rollout hash seed (default: the feature key). Change it to re-randomize a rollout independently. |
| `HashVersion` | `hashVersion` | Must be `2` (or omitted). A rule requesting any other version is skipped. |

The JSON shape is a strict subset of GrowthBook's feature schema, so the same definitions load into GrowthBook unchanged.

## Targeting & rollout

### Conditions

A condition is an object of `attribute → match`. All entries are AND-ed.

```jsonc
// implicit equality
{"plan": "pro"}

// operators
{"plan": {"$in": ["pro", "team"]}}
{"plan": {"$ne": "free"}}
{"country": {"$eq": "US"}}

// multiple fields (AND)
{"plan": "pro", "country": "US"}
```

Supported operators: implicit equality (including array/object deep-equality), `$eq`, `$ne`, and `$in`. `$in` matches by **set intersection** when the attribute itself is an array — e.g. `{"roles": {"$in": ["admin"]}}` matches `roles: ["billing", "admin"]`.

Any condition using an operator outside this set — including top-level logical operators like `$or`/`$and`/`$not`/`$nor` — causes the rule to be **skipped** (evaluation falls through to the next rule), never silently mis-evaluated.

### Percentage rollout

```jsonc
{
  "defaultValue": false,
  "rules": [
    { "condition": {"plan": "pro"}, "coverage": 0.25, "force": true }
  ]
}
```

This turns the flag on for a deterministic 25% of `pro` users. Bucketing is `hash(seed + attributes[hashAttribute])` using FNV-1a v2; a given `id` always lands in the same place, so ramping `coverage` from `0.25` → `0.50` only *adds* users — nobody who was on gets turned off. Set `seed` to roll an independent dice for a different rollout.

A rule can combine all three concerns — a condition (who's eligible), a coverage (what fraction of them), and a force value (what they get).

## Evaluation API

`c.For(attrs)` returns an `*Evaluation` bound to those attributes:

```go
ev := c.For(flagpole.Attributes{"id": userID, "plan": plan})

on   := ev.IsOn("new-checkout")              // bool; unknown flag → false
color := ev.Value("button-color", "blue")    // any; unknown/nil → "blue"
```

- `IsOn(key) bool` — true if the flag resolves to a truthy value (`false`, `0`, `""`, `"false"`, `"0"`, and nil are falsy). Unknown flags are off.
- `Value(key string, def any) any` — the resolved value, or `def` if the flag is unknown or resolves to nil.

For one-off evaluation without a Client, the pure function `flagpole.Evaluate(feature, key, attrs) Result` is also exported (`Result{Value any; On bool}`).

## Sources

A `Source` supplies the full set of feature definitions. The Client loads it on startup and on each refresh.

```go
type Source interface {
    Load(ctx context.Context) (map[string]Feature, error)
}
```

### StaticSource / StaticSourceFromJSON

`StaticSource{Features: ...}` serves a fixed map — handy for tests or when you fetch and parse the payload yourself. `StaticSourceFromJSON([]byte)` parses a GrowthBook-style `{"features": {...}}` envelope.

> Don't mutate `StaticSource.Features` after handing it to a Client — `Load` returns the map directly, so a later mutation would bypass the Client's lock.

### sourcepg — Postgres-backed source

`sourcepg.New(pool *pgxpool.Pool) *sourcepg.Source` returns a `flagpole.Source` backed by a `feature_flags` table. Only `archived = false` rows are loaded. Run the reference schema (`sourcepg/schema.sql`) with your own migration tooling:

```sql
CREATE TABLE IF NOT EXISTS feature_flags (
    key         TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    definition  JSONB NOT NULL,          -- the Feature JSON (GrowthBook-subset shape)
    archived    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Custom sources

Implement `Load` over anything — an HTTP endpoint serving a GrowthBook-format payload, a config file, a key-value store. The Client treats every source identically.

## The Client

`flagpole.New(ctx, src, opts...)` loads features **synchronously once** (so a Source error surfaces immediately) and then refreshes them in the background until you call `Close()`. The Client is safe for concurrent use.

```go
c, err := flagpole.New(ctx, src,
    flagpole.WithRefreshInterval(30*time.Second),
    flagpole.WithTracker(myTracker),
)
```

| Option | Description |
|--------|-------------|
| `WithRefreshInterval(d)` | How often to reload from the Source (default `60s`; `≤ 0` loads once and never refreshes). |
| `WithTracker(tr)` | Experiment exposure tracker (default `NoopTracker`). |
| `WithOnError(fn)` | Callback invoked when a background refresh fails (default no-op). The Client keeps serving the last good snapshot; use this to log/metric/alert instead of failing silently. |

`Close()` stops the background refresh and is safe to call once the Client is no longer needed (and safe to call more than once).

A background refresh that fails leaves the Client serving its last good snapshot. `LastRefresh()` returns the time of the most recent *successful* load; alert when `time.Since(client.LastRefresh())` exceeds a small multiple of the refresh interval to catch a Client wedged on stale flags (e.g. a Source that has been unreachable for minutes).

Because evaluation is local and bucketing is deterministic, multiple processes pointed at the same Source (e.g. an API server and a background worker sharing one Postgres) will independently agree on the result for any given user — no coordination required.

## Admin HTTP handler

`adminhttp.NewHandler(store adminhttp.Store) http.Handler` is a mountable JSON CRUD handler for managing flag definitions at runtime. It is **auth-agnostic** — wrap it with your own authentication/authorization before mounting.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/flags` | List all feature definitions (`{key: Feature}`). |
| `PUT` | `/flags/{key}` | Upsert a feature (body: Feature JSON). |
| `DELETE` | `/flags/{key}` | Archive a feature. |

```go
mux := http.NewServeMux()
admin := adminhttp.NewHandler(myStore)
mux.Handle("/admin/flags", requireAdmin(http.StripPrefix("/admin", admin)))
mux.Handle("/admin/flags/", requireAdmin(http.StripPrefix("/admin", admin)))
```

Implement the `Store` interface against your persistence layer:

```go
type Store interface {
    List(ctx context.Context) (map[string]flagpole.Feature, error)
    Upsert(ctx context.Context, key string, f flagpole.Feature) error
    Archive(ctx context.Context, key string) error // idempotent
}
```

A minimal Postgres `Store` over the same `feature_flags` table:

```go
type pgStore struct{ pool *pgxpool.Pool }

func (s pgStore) List(ctx context.Context) (map[string]flagpole.Feature, error) {
    return sourcepg.New(s.pool).Load(ctx) // or your own query incl. archived rows
}
func (s pgStore) Upsert(ctx context.Context, key string, f flagpole.Feature) error {
    def, _ := json.Marshal(f)
    _, err := s.pool.Exec(ctx, `
        INSERT INTO feature_flags (key, definition) VALUES ($1, $2)
        ON CONFLICT (key) DO UPDATE SET definition = EXCLUDED.definition, updated_at = NOW()`,
        key, def)
    return err
}
func (s pgStore) Archive(ctx context.Context, key string) error {
    _, err := s.pool.Exec(ctx,
        `UPDATE feature_flags SET archived = true, updated_at = NOW() WHERE key = $1`, key)
    return err
}
```

## Experiment exposure tracking

An **experiment** is a rule with `Variations` — a Feature is running an experiment iff a matched rule has `len(Variations) >= 2`. When such a rule matches, flagpole assigns a variation (using deterministic GrowthBook-compatible weighted bucketing via `hashVersion: 2`), and the feature's `Value`/`IsOn` resolve to the assigned variation's value.

**Experiment knobs:**
- `variations` — array of variation values (must have `≥ 2` entries for an experiment rule)
- `weights` — optional array of per-variation weights that should sum to ~1.0 (default: equal split); e.g. `[0.3, 0.7]` is a 30%/70% split. A weights array whose sum falls outside 0.99–1.01, or whose length ≠ the number of variations, is replaced with an equal split.
- `coverage` — optional fraction of matched units admitted to the experiment (the rest fall through to the feature default with no exposure); ramps the experiment
- `condition` — optional targeting (existing condition subset) applies before assignment
- `key` — optional explicit experiment key (used in `Exposure` for analysis; defaults to the feature key)
- `seed` — optional hash seed (default: the rule's `key`); set it to re-randomize an experiment independently
- `hashAttribute` — optional bucketing attribute (default `"id"`)

```go
// An experiment rule with variations, weights, and rollout coverage:
{
  "key": "button-color-v2",
  "condition": {"plan": "pro"},  // target pro users only
  "coverage": 0.5,               // run on 50% of pro users
  "variations": ["red", "blue", "green"],
  "weights": [0.2, 0.3, 0.5],    // 20% red, 30% blue, 50% green (must sum to ~1.0)
  "hashVersion": 2
}
```

Exposures fire **on every genuine assignment** — no cross-unit dedup (downstream BI takes first-exposure-per-unit per the GrowthBook warehouse convention). Within a single `For(...)`/`ForContext(...)` binding, an exposure fires at most once per feature key.

**Exposure firing and context propagation:**

```go
ev := c.For(attrs)                    // background context, no tracing propagation
on := ev.IsOn("my-experiment")

// Or with a traced context (recommended for analytics):
ev := c.ForContext(ctx, attrs)        // ctx propagates to Tracker
on := ev.IsOn("my-experiment")        // fires exposure via Tracker.Track(ctx, ...)
```

`ForContext(ctx, attrs)` propagates a context to the Tracker, so tracing/cancellation flows through. `For(attrs)` uses a background context.

The enriched **`Exposure` shape** carries bucketing metadata for analysis:

```go
type Exposure struct {
    ExperimentKey string      // matched experiment rule's Key (or feature key if rule.Key is unset)
    VariationID   int         // assigned variation index
    HashAttribute string      // attribute used for bucketing (e.g. "id")
    HashValue     string      // the actual unit value bucketed — the join key for analysis
    Attributes    Attributes  // dimensions snapshot
    At            time.Time
}
```

The default is `NoopTracker` (discards exposures). Provide one with `WithTracker(tr)` to feed your analytics pipeline. The `Exposure` shape mirrors GrowthBook's exposure logging, so downstream analysis can be done by GrowthBook's warehouse-native tooling or by your own SQL.

**`trackpg` — batteries-included Postgres Tracker:** Use the bundled `trackpg` subpackage for a Postgres-backed, async-batching tracker that never blocks the hot path (drops and counts on buffer overflow). See [USAGE.md](USAGE.md) for the wiring and firing contract.

## GrowthBook compatibility

flagpole implements a strict, tested subset of the GrowthBook feature evaluation algorithm.

**Supported:**
- `defaultValue` / `force` values
- Percentage rollout via `coverage` (deterministic FNV-1a v2 bucketing, `hashVersion: 2`)
- `hashAttribute` and `seed` on rollout rules
- Condition operators: equality (implicit, incl. array/object deep-equality), `$eq`, `$ne`, `$in` (set-intersection when the attribute is an array)
- Experiment `variations` / `weights` / `key` on rules with `len(Variations) >= 2`
- Experiment `coverage` (fractional rollout ramp)
- Condition targeting on experiment rules

**Out of scope (skipped, not evaluated):**
- Namespaces (experiment exclusion groups)
- `range`, `filters`, `parentConditions`
- `hashVersion` other than 2 (a rule requesting any other version is skipped)
- Sticky/fallback bucketing (always deterministic per unit)
- Condition operators beyond: equality, `$eq`, `$ne`, `$in` (unsupported operators like `$gt`, `$regex`, `$or`, `$and`, `$nor`, etc. cause the rule to be skipped)

A condition using an unsupported operator — including top-level `$or`/`$and`/`$not`/`$nor` — causes its rule to be **skipped**, never silently mis-evaluated.

**hashVersion caveat:** flagpole evaluates with `hashVersion: 2` and treats a missing `hashVersion` as v2 (the flagpole default). **GrowthBook's default is v1**, so on any rollout or experiment rule you intend to share between flagpole and GrowthBook, set `hashVersion: 2` explicitly. A rule requesting any other version is skipped.

**Migrating from GrowthBook:** Ensure any rollout rule you share with GrowthBook has `hashVersion: 2` explicitly set so bucketing stays identical across both systems.

Compatibility is validated against GrowthBook's published `cases.json` SDK test fixtures (`compat_test.go`): the hashing vectors, the feature-evaluation suite, and the `evalCondition` oracle for the supported operator subset. Unsupported fixtures are explicitly skipped rather than silently passed.

## How it works

1. **Definitions** live wherever your `Source` reads them (Postgres, JSON, etc.) in GrowthBook-subset shape.
2. On `New`, the **Client** loads all definitions into an in-memory snapshot and starts a refresh ticker. Each refresh swaps the whole snapshot under a write lock; readers take a read lock, so they always see a consistent set.
3. `For(attrs).IsOn(key)` looks up the feature and runs the pure **evaluator**: walk rules in order, check each rule's condition and coverage, return the first match (or the default).
4. **Coverage** hashes `seed + attributes[hashAttribute]` with FNV-1a v2 into `[0,1)` and compares against the rule's `coverage`. This is the single source of bucketing determinism and the thing that makes flagpole a drop-in for GrowthBook.

There is no network call during evaluation, and the evaluator is allocation-light and lock-cheap (a single read-lock per lookup).

## Testing

```bash
go test ./...                 # core + adminhttp (sourcepg skips without a DB)
go test -race ./...           # with the race detector

# exercise the Postgres adapter against a real database:
export FLAGPOLE_TEST_DATABASE_URL='postgres://user:pass@localhost:5432/flagpole_test?sslmode=disable'
go test ./sourcepg/...
```

The compatibility suite (`compat_test.go`) runs flagpole's hashing and evaluator against GrowthBook's vendored `cases.json` fixtures.

## React bindings

[`@sudarkoff/flagpole-react`](./react) exposes your server-evaluated flags to a React app via a `FlagsProvider` and `useFeatureIsOn` / `useFeatureValue` hooks. Install with `npm install @sudarkoff/flagpole-react`; see [`react/README.md`](./react/README.md) for usage.

## Roadmap

- **Phase B — experiments** ✓ shipped — rules with `variations` and weighted bucketing, exposure firing, and async Postgres batching tracker.
- **Future** — exposure analysis: metric definitions, lift, and significance over experiment exposures; namespaces (experiment exclusion); sticky/fallback bucketing.

## License

Apache-2.0. See [LICENSE](LICENSE).
