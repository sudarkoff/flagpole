# flagpole

A lightweight Go feature-flag library with a GrowthBook-compatible local evaluator.

Flags are evaluated in-process — no per-flag network call, no external service dependency at evaluation time. Feature definitions use the same JSON schema as GrowthBook, and bucketing uses the same FNV-1a v2 hash, so a flag definition written for flagpole ports to GrowthBook unchanged.

## Install

```
go get github.com/sudarkoff/flagpole
```

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
    // flag is on
}
```

### Postgres source

```go
import (
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
color := ev.Value("button-color", "blue") // returns "blue" if flag is unknown
```

## Attributes

`Attributes` is a flat `map[string]any`. Keys are arbitrary strings that match the `hashAttribute` or `condition` fields in your flag definitions. The default hash attribute (used for percentage rollouts when `hashAttribute` is not set in the rule) is `"id"`.

```go
attrs := flagpole.Attributes{
    "id":    "user-abc",
    "plan":  "pro",
    "beta":  true,
}
```

## Evaluation

`c.For(attrs)` returns an `*Evaluation` bound to those attributes. From it:

- `IsOn(key) bool` — returns true if the flag resolves to a truthy value. Unknown flags are off.
- `Value(key string, def any) any` — returns the resolved value, or `def` if the flag is unknown or resolves to nil.

Rules are tried in order; the first matching rule wins. If no rule matches, `DefaultValue` is returned.

## Sources

### `Source` interface

```go
type Source interface {
    Load(ctx context.Context) (map[string]Feature, error)
}
```

Any type implementing `Load` can act as a source. The `Client` calls `Load` once on startup and again on each refresh interval.

### `StaticSource` / `StaticSourceFromJSON`

`StaticSource` holds a fixed map of features — useful in tests or when you fetch and parse the feature payload yourself.

`StaticSourceFromJSON([]byte)` parses a GrowthBook-style `{"features": {...}}` envelope.

### `sourcepg` — Postgres-backed source

`sourcepg.New(pool *pgxpool.Pool) *sourcepg.Source` returns a `flagpole.Source` that queries a `feature_flags` table. Only rows with `archived = false` are loaded.

The expected schema is in `sourcepg/schema.sql` — consumers run it via their own migration tooling:

```sql
CREATE TABLE IF NOT EXISTS feature_flags (
    key         TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    definition  JSONB NOT NULL,
    archived    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

## Admin HTTP handler

`adminhttp.NewHandler(store adminhttp.Store) http.Handler` returns a mountable handler for managing flag definitions at runtime. It is auth-agnostic — wrap it with your own authentication/authorization middleware before mounting.

Routes:

| Method | Path           | Description                          |
|--------|----------------|--------------------------------------|
| GET    | `/flags`       | List all feature definitions         |
| PUT    | `/flags/{key}` | Upsert a feature (body: Feature JSON)|
| DELETE | `/flags/{key}` | Archive a feature                    |

Implement the `adminhttp.Store` interface (`List`, `Upsert`, `Archive`) against whatever persistence layer you use — for `sourcepg` users, the store typically wraps the same Postgres pool.

## Experiment exposure tracking

`flagpole.Tracker` is an interface for logging experiment exposures:

```go
type Tracker interface {
    Track(ctx context.Context, e Exposure)
}
```

`Exposure` carries `ExperimentKey`, `VariationID`, `Attributes`, and a timestamp. The default is `NoopTracker`, which discards exposures. Provide a `Tracker` via `WithTracker(tr)` to wire up your own logging pipeline. Exposure analysis is not part of this release.

## Client options

| Option                          | Description                                                |
|---------------------------------|------------------------------------------------------------|
| `WithRefreshInterval(d)`        | How often to reload from the Source (default: 60s; ≤0 = load once) |
| `WithTracker(tr)`               | Experiment exposure tracker (default: `NoopTracker`)       |

## GrowthBook compatibility

flagpole implements a strict, tested subset of the GrowthBook feature evaluation algorithm:

**Supported:**
- `defaultValue` / `force` values
- Percentage rollout via `coverage` (deterministic FNV-1a v2 bucketing, `hashVersion: 2`)
- `hashAttribute` and `seed` on rollout rules
- Condition operators: equality (implicit), `$eq`, `$ne`, `$in`

**Out of scope (skipped, not evaluated):**
- Experiment `variations` / `weights` / `key` (Phase B)
- Condition operators: `$gt`, `$gte`, `$lt`, `$lte`, `$regex`, `$exists`, `$not`, `$or`, `$and`
- `range`, `filters`, `parentConditions`
- `hashVersion` other than 2

Compatibility is validated against GrowthBook's published `cases.json` SDK test fixtures in `compat_test.go`.

## License

Apache-2.0. See [LICENSE](LICENSE).
