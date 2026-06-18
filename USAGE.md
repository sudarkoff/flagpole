# Using flagpole

A practical, recipe-oriented guide to adopting flagpole in a real service. It
draws heavily on how [twocal](https://twocal.com) (a calendar-sync product)
wires flagpole into a Go API + background worker, but it covers the other ways
the library is meant to be used too. The [README](README.md) is the
reference; this is the cookbook.

## Contents

- [The mental model](#the-mental-model)
- [Decide what you need](#decide-what-you-need)
- [Path 1 — the five-minute static setup](#path-1--the-five-minute-static-setup)
- [Path 2 — the production setup (Postgres-backed)](#path-2--the-production-setup-postgres-backed)
- [Building attributes](#building-attributes)
- [Evaluating flags](#evaluating-flags)
- [Targeting & rollout recipes](#targeting--rollout-recipes)
- [Multi-process consistency (API + worker)](#multi-process-consistency-api--worker)
- [Managing flags at runtime (adminhttp)](#managing-flags-at-runtime-adminhttp)
- [Observability: don't serve stale flags blindly](#observability-dont-serve-stale-flags-blindly)
- [Experiment exposure tracking](#experiment-exposure-tracking)
- [Exposing flags to a frontend](#exposing-flags-to-a-frontend)
- [Custom sources](#custom-sources)
- [Testing with flags](#testing-with-flags)
- [Coming from GrowthBook](#coming-from-growthbook)
- [Anti-patterns and gotchas](#anti-patterns-and-gotchas)

---

## The mental model

flagpole has three moving parts and it pays to keep them straight:

1. **A `Source`** — where flag *definitions* live (a JSON blob, a Postgres
   table, an HTTP endpoint, whatever). It hands the client a
   `map[string]Feature`.
2. **A `Client`** — loads the Source once at startup, then refreshes it on a
   timer into an in-memory snapshot. Every evaluation reads that snapshot.
   **There is no network call when you evaluate a flag.**
3. **An `Evaluation`** — `client.For(attrs)` binds a set of *attributes* (the
   user/account you're deciding for) and answers `IsOn(key)` / `Value(key, def)`
   against the current snapshot.

The single most important consequence: because evaluation is a pure, in-memory
computation and bucketing is deterministic, **any number of processes pointed at
the same Source independently agree on the answer for a given user** — no
coordination, no per-flag RPC, no sidecar. twocal leans on exactly this: its API
server and its sync worker both build a client from the same Postgres table and
never have to ask each other anything.

## Decide what you need

| If you want to… | Use |
|---|---|
| Try the API, or ship a flag baked into a deploy | `StaticSourceFromJSON` |
| Flip flags at runtime without redeploying | `sourcepg` + `adminhttp` (Postgres) |
| Load definitions from your own store/endpoint | a custom `Source` |
| Keep an API and a worker in lockstep | one Source, two clients |
| Catch a wedged/stale flag cache | `WithOnError` + `LastRefresh()` |
| Run real A/B experiments | a `Tracker` + downstream analysis |
| Read flags in a browser SPA | server-evaluate, hydrate, read in React |

You can start at the top and move down as the product grows — the evaluation API
(`For(attrs).IsOn(...)`) never changes, only the Source behind it does.

## Path 1 — the five-minute static setup

A `StaticSource` serves a fixed map of definitions. Good for tests, demos, or a
service that fetches its flag payload out of band (e.g. from object storage at
boot).

```go
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

if c.For(flagpole.Attributes{"id": "user-123", "plan": "pro"}).IsOn("dark-mode") {
    // ...
}
```

For a one-off decision with no client at all, `flagpole.Evaluate(feature, key, attrs)`
returns a `Result{Value, On}` directly.

> Don't mutate `StaticSource.Features` after handing it to a client — `Load`
> returns the map by reference, so a later mutation sidesteps the client's lock.
> If you need runtime changes, you want a real Source (next section).

## Path 2 — the production setup (Postgres-backed)

This is twocal's setup. Definitions live in a `feature_flags` table; the bundled
`sourcepg` adapter reads it; a short refresh interval means an edit propagates to
every process within seconds.

First, create the table (use your own migration tooling — flagpole ships the
reference schema at `sourcepg/schema.sql`):

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

Then build the client. twocal wraps `flagpole.New` in a small constructor so the
API and worker share one policy — a refresh interval *and* an error hook (more on
why the hook matters [below](#observability-dont-serve-stale-flags-blindly)):

```go
// internal/flags/client.go
const RefreshInterval = 30 * time.Second

func NewClient(ctx context.Context, pool *pgxpool.Pool, opts ...flagpole.Option) (*flagpole.Client, error) {
    base := []flagpole.Option{
        flagpole.WithRefreshInterval(RefreshInterval),
        flagpole.WithOnError(func(err error) {
            log.Warn().Err(err).Msg("feature flags: refresh failed, serving stale snapshot")
        }),
    }
    return flagpole.New(ctx, sourcepg.New(pool), append(base, opts...)...)
}
```

Wire it once in `main` and keep it for the process lifetime:

```go
flagClient, err := flags.NewClient(context.Background(), pool)
if err != nil {
    log.Fatal().Err(err).Msg("init feature flags")
}
defer flagClient.Close()
```

The constructor pattern is worth copying: it gives you one place to set defaults,
it keeps the `flagpole`/`sourcepg` imports out of your handlers, and the
variadic `opts...` still lets a test or a special process override anything.

## Building attributes

`Attributes` is just a flat `map[string]any` describing the unit you're deciding
for. The key `"id"` is the default bucketing key for percentage rollouts, so it
should be stable per user. Everything else is fair game for `condition` targeting.

The useful idea from twocal is to build attributes at **different richness levels
depending on what's cheap to know at the call site** — don't do a DB round-trip
just to evaluate a flag if you don't have to.

**From a JWT, no DB hit** — for hot request paths:

```go
func Attributes(c auth.Claims) flagpole.Attributes {
    return flagpole.Attributes{
        "id":    c.UserID.String(),
        "email": c.Email,
        "plan":  c.Plan,
        "role":  c.Role,
    }
}
```

**From a loaded user row** — adds tenure for "users older than N days" targeting:

```go
func AttributesForUser(u sqlc.User) flagpole.Attributes {
    return flagpole.Attributes{
        "id":             u.ID.String(),
        "email":          u.Email,
        "plan":           u.Plan,
        "role":           u.Role,
        "accountAgeDays": int(time.Since(u.CreatedAt).Hours() / 24),
    }
}
```

**Enriched** — usage- and membership-derived signals the caller already loaded,
including the user's cohort keys, which feed `$in` cohort targeting:

```go
a := flagpole.Attributes{
    "id":             u.ID.String(),
    "plan":           u.Plan,
    "accountAgeDays": int(time.Since(u.CreatedAt).Hours() / 24),
    "isPaying":       u.SubscriptionStatus.Valid && u.SubscriptionStatus.String == "active",
    "pairCount":      pairCount,
    "providers":      providers,        // []string
    "cohorts":        cohorts,          // []string — drives `cohorts $in [...]`
}
```

Two practical notes:

- Numbers coming from JSON definitions arrive as `float64`, but flagpole
  compares numbers numerically, so an `int` attribute still matches a JSON number
  in a condition. You don't have to pre-convert.
- Pass real typed slices (`[]string`) for set membership — flagpole's `$in`
  intersects against typed slices directly, so cohort keys flow through naturally
  on both the request path and the worker path.

## Evaluating flags

Bind attributes once, then ask as many questions as you like:

```go
ev := c.For(flagpole.Attributes{"id": userID, "plan": plan})

on    := ev.IsOn("new-checkout")           // bool; unknown flag → false
color := ev.Value("button-color", "blue")  // any;  unknown/nil → "blue"
```

- `IsOn` is truthiness: `false`, `0`, `""`, `"false"`, `"0"`, and nil are off;
  an **unknown flag is off**. This makes "flag doesn't exist yet" and "flag is
  off" behave identically, which is what you want during rollout.
- `Value(key, def)` returns the resolved value or your default for
  unknown/nil — use it for multivariate values (colors, limits, copy).

A clean integration habit (again from twocal) is to keep a tiny helper that
tolerates a nil client, so code paths that may run without flags configured
don't panic:

```go
func (e *Engine) flagOn(attrs flagpole.Attributes, key string) bool {
    if e.flags == nil {
        return false   // no client configured ⇒ feature off
    }
    return e.flags.For(attrs).IsOn(key)
}
```

And name your flag keys as typed constants near where they're gated, so the
string lives in exactly one place:

```go
const flagSkipOnSync = "skip-on-sync"
const flagFieldLevelMerge = "field-level-merge"
```

When you gate several flags for the same unit, build the attributes **once** and
reuse the `Evaluation` — especially if building them costs a query (e.g. a cohort
lookup). Don't call `For` per flag with a freshly-built attribute map.

## Targeting & rollout recipes

A `Feature` is a `defaultValue` plus an ordered list of `rules`. Rules are tried
top to bottom; **the first matching rule wins**; if none match you get the
default. Each rule can combine *who's eligible* (`condition`), *what fraction of
them* (`coverage`), and *what they get* (`force`).

**Simple boolean gate for one segment:**

```jsonc
{
  "defaultValue": false,
  "rules": [
    {"condition": {"plan": "pro"}, "force": true}
  ]
}
```

**Internal-only kill switch / dogfood:**

```jsonc
{
  "defaultValue": false,
  "rules": [
    {"condition": {"role": "admin"}, "force": true}
  ]
}
```

**Deterministic percentage rollout** — turn it on for a stable 25% of pro users:

```jsonc
{
  "defaultValue": false,
  "rules": [
    {"condition": {"plan": "pro"}, "coverage": 0.25, "force": true}
  ]
}
```

Bucketing hashes `seed + attributes[hashAttribute]` (default `hashAttribute` is
`"id"`, default `seed` is the feature key) with FNV-1a v2. Ramping `coverage`
from `0.25` → `0.50` only **adds** users — nobody who was on gets flipped off.
Set an explicit `seed` to roll an independent dice for a different rollout that
shouldn't correlate with this one.

**Cohort / beta targeting via `$in`** — this is how twocal opts specific users
into a beta. Users carry a `cohorts: []string` attribute; the rule fires when the
cohort set intersects:

```jsonc
{
  "defaultValue": false,
  "rules": [
    {"condition": {"cohorts": {"$in": ["skip-on-sync-beta"]}}, "force": true}
  ]
}
```

`$in` is set-intersection when the attribute itself is an array, so a user with
`cohorts: ["billing", "skip-on-sync-beta"]` matches.

**Layered rollout** (combine the above — order matters):

```jsonc
{
  "defaultValue": false,
  "rules": [
    {"condition": {"role": "admin"}, "force": true},                       // staff always
    {"condition": {"cohorts": {"$in": ["beta"]}}, "force": true},          // opted-in betas
    {"condition": {"plan": "pro"}, "coverage": 0.5, "force": true}         // 50% of pro
  ]
}
```

Supported condition operators are deliberately a small, tested set: implicit
equality (incl. deep array/object equality), `$eq`, `$ne`, and `$in`. **Anything
outside that set — `$gt`, `$regex`, `$or`, `$and`, etc. — causes the rule to be
skipped, not mis-evaluated.** Evaluation falls through to the next rule. Keep
your conditions inside the supported subset or they silently won't fire (see
[Coming from GrowthBook](#coming-from-growthbook)).

## Multi-process consistency (API + worker)

twocal runs two binaries that must make the *same* flag decision for the same
user: the API (which tells the SPA what to show and gates write endpoints) and
the sync worker (which gates backend sync behavior). The pattern is simply: both
build a client from the same Source.

```go
// worker main.go
// Same pool/source as the API ⇒ identical flag hashing ⇒ identical decisions.
flagClient, err := flags.NewClient(context.Background(), pool)
if err != nil {
    log.Fatal().Err(err).Msg("init feature flags")
}
defer flagClient.Close()
engine.SetFlags(flagClient)
```

Because bucketing is deterministic and depends only on the attributes + the
definition, the API and worker never disagree even though they refresh
independently on their own 30-second timers. A flag flipped in the admin handler
reaches both within one refresh interval. There's nothing to synchronize.

The worker injects the client into its sync engine via a setter, then gates
behavior with the nil-tolerant helper shown earlier:

```go
const flagSkipOnSync = "skip-on-sync"

func (e *Engine) SetFlags(c *flagpole.Client) { e.flags = c }

// ... later, deciding whether to apply a skip transform:
attrs := e.flagAttributes(ctx, pair.OwnerUserID)   // built once per sync
if e.flagOn(attrs, flagSkipOnSync) {
    // drop the event (ensure-absent)
}
```

Note `flagAttributes` does the (potentially failing) cohort lookup **once per
sync** and degrades gracefully — if the cohort query fails it falls back to
id-only targeting and logs, rather than failing the whole sync. That's a good
template for any attribute that requires I/O: never let attribute-building take
down the work the flag was only meant to modulate.

## Managing flags at runtime (adminhttp)

`adminhttp.NewHandler(store)` is a mountable JSON CRUD handler for editing
definitions live. It's **auth-agnostic** — you wrap it with your own
authn/authz before mounting. twocal mounts it behind admin-only middleware:

```go
// adminhttp serves /flags and /flags/{key}; StripPrefix lets it match under
// /api/v1/admin. chi needs both the exact and wildcard patterns to route the
// list and per-key endpoints.
flagAdmin := http.StripPrefix("/api/v1/admin", adminhttp.NewHandler(flags.NewStore(pool)))
r.With(authMW, auth.RequireAdmin).Handle("/admin/flags", flagAdmin)
r.With(authMW, auth.RequireAdmin).Handle("/admin/flags/*", flagAdmin)
```

| Method | Path | Description |
|---|---|---|
| `GET` | `/flags` | List all definitions (`{key: Feature}`) |
| `PUT` | `/flags/{key}` | Upsert a feature (body: Feature JSON) |
| `DELETE` | `/flags/{key}` | Archive a feature |

You implement the `Store` interface against your persistence. twocal's store is a
thin wrapper over the same `feature_flags` table the Source reads:

```go
type Store struct{ pool *pgxpool.Pool }

func (s *Store) List(ctx context.Context) (map[string]flagpole.Feature, error) { /* SELECT ... WHERE archived = false */ }
func (s *Store) Upsert(ctx context.Context, key string, f flagpole.Feature) error { /* INSERT ... ON CONFLICT */ }
func (s *Store) Archive(ctx context.Context, key string) error { /* UPDATE ... SET archived = true */ }
```

> **pgx + PgBouncer gotcha worth stealing:** twocal runs pgx under the simple
> query protocol (for PgBouncer compatibility). Under that protocol, pgx encodes
> a `[]byte` argument as a bytea literal, which Postgres rejects for a `jsonb`
> column ("invalid input syntax for type json"). The fix is to pass the marshaled
> definition as a **`string`**, not `[]byte`, so it's sent as text and parsed as
> jsonb. This bit them in both the admin `Upsert` and the exposure `Tracker`.

The CRUD handler writes to the table; the running clients pick the change up on
their next refresh. No restart, no deploy.

## Observability: don't serve stale flags blindly

This is the part teams forget. When a background refresh **fails**, flagpole does
*not* error your evaluations — it keeps serving the **last good snapshot**. That's
the right default for availability, but it means a Source that's been unreachable
for ten minutes is completely invisible unless you instrument it. A worker could
gate sync on stale flags indefinitely and you'd never know.

Two seams make it visible:

**1. `WithOnError` — know that a refresh failed.** twocal logs it (you could also
bump a counter):

```go
flagpole.WithOnError(func(err error) {
    log.Warn().Err(err).Msg("feature flags: refresh failed, serving stale snapshot")
})
```

**2. `LastRefresh()` — know *how* stale you are.** It returns the time of the
last *successful* load. twocal exports it as a Prometheus gauge and alerts when
it falls behind a small multiple of the refresh interval:

```go
type FlagRefreshCollector struct {
    src         interface{ LastRefresh() time.Time }
    lastRefresh *prometheus.Desc
}

func (c *FlagRefreshCollector) Collect(ch chan<- prometheus.Metric) {
    if t := c.src.LastRefresh(); !t.IsZero() {
        ch <- prometheus.MustNewConstMetric(c.lastRefresh, prometheus.GaugeValue, float64(t.Unix()))
    }
}
```

Their alert rule: `last_refresh` stale `> 3 × refresh_interval` (≈90s at the 30s
default) ⇒ "flag cache wedged; decisions are stale." Because `RefreshInterval` is
an exported constant, the alert threshold is derived from the same number the
client uses — they can't drift apart.

If you take one thing from this section: **wire `WithOnError` and alert on
`LastRefresh()` the day you move off a static source.**

## Experiment exposure tracking

`flagpole.Tracker` is the seam for logging that a unit was *exposed* to an
experiment variation, so you can analyze lift later. The default is a no-op;
provide one with `WithTracker`.

```go
type Tracker interface {
    Track(ctx context.Context, e Exposure)
}

type Exposure struct {
    ExperimentKey string
    VariationID   int
    Attributes    Attributes
    At            time.Time
}
```

twocal persists exposures to a Postgres table for warehouse analysis (note the
same `string(attrs)` jsonb trick as the admin store):

```go
func (t *Tracker) Track(ctx context.Context, e flagpole.Exposure) {
    attrs, _ := json.Marshal(e.Attributes)
    hashUnit, _ := e.Attributes["id"].(string)
    _, err := t.pool.Exec(ctx, `
        INSERT INTO experiment_exposures (experiment_key, variation_id, hash_unit, attributes, exposed_at)
        VALUES ($1, $2, $3, $4, $5)`,
        e.ExperimentKey, e.VariationID, hashUnit, string(attrs), e.At)
    if err != nil {
        log.Warn().Err(err).Str("experiment", e.ExperimentKey).Msg("track exposure failed")
    }
}
```

Wire it with `flags.NewClient(ctx, pool, flagpole.WithTracker(flags.NewTracker(pool)))`.
The `Exposure` shape mirrors GrowthBook's, so downstream analysis can use
GrowthBook's warehouse-native tooling or your own SQL. (Exposure *analysis* —
metrics, significance — is on the roadmap, not in this release; the seam is here
so it's additive.)

A tracker that writes synchronously to a DB on every exposure can become a hot
path of its own — buffer/batch or fire-and-forget if exposure volume is high.

## Exposing flags to a frontend

flagpole evaluates on the server. To use a flag in a browser SPA you
**server-evaluate a curated set, embed the results in a response, and read them
in the client.** Don't ship raw definitions or attributes to the browser.

**Server: evaluate a curated allow-list.** twocal keeps an explicit list of
client-facing keys — backend-only gates are deliberately not included — and
resolves them into a plain `map[string]bool`:

```go
var ClientKeys = []string{"field-level-merge"}

func Evaluate(c *flagpole.Client, attrs flagpole.Attributes) map[string]bool {
    out := make(map[string]bool, len(ClientKeys))
    ev := c.For(attrs)
    for _, k := range ClientKeys {
        out[k] = ev.IsOn(k)
    }
    return out
}
```

That map rides along on the user-bootstrap response:

```go
render.JSON(w, r, map[string]any{
    // ...user fields...
    "flags": flags.Evaluate(fc, flags.AttributesForUserEnriched(user, count, providers, cohortKeys)),
})
```

**Client: hydrate and read.** You have two options:

- **The official bindings, `@sudarkoff/flagpole-react`** (`npm i
  @sudarkoff/flagpole-react`) — a `FlagsProvider` plus `useFeatureIsOn` /
  `useFeatureValue` hooks. Reach for this if you want the maintained package and
  its full hook surface. See [`react/README.md`](react/README.md).

- **A hand-rolled provider** when all you need is booleans. twocal hydrates the
  server-sent map into a trivial context and reads it with a one-line hook —
  about 20 lines, no dependency:

  ```tsx
  const FlagsContext = createContext<Record<string, boolean>>({})

  export function FlagsProvider({ flags, children }) {
    return <FlagsContext.Provider value={flags ?? {}}>{children}</FlagsContext.Provider>
  }

  /** unknown flags are off — same semantics as the Go IsOn */
  export function useFlag(key: string): boolean {
    return useContext(FlagsContext)[key] ?? false
  }
  ```

  ```tsx
  <FlagsProvider flags={user?.flags ?? {}}>
    <App />
  </FlagsProvider>
  ```

Either way, keep the **unknown-flag-is-off** semantics on the client so it
matches the server, and keep the allow-list small — every client key is a public
surface.

## Custom sources

A `Source` is one method:

```go
type Source interface {
    Load(ctx context.Context) (map[string]Feature, error)
}
```

Implement it over anything — the client treats every source identically (loads
once at `New`, refreshes on the timer). Two common shapes:

**An HTTP endpoint serving a GrowthBook-format payload:**

```go
type httpSource struct{ url string; hc *http.Client }

func (s httpSource) Load(ctx context.Context) (map[string]flagpole.Feature, error) {
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
    resp, err := s.hc.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }
    src, err := flagpole.StaticSourceFromJSON(body)  // reuse the envelope parser
    if err != nil {
        return nil, err
    }
    return src.Load(ctx)
}
```

A failed `Load` here just triggers your `WithOnError` hook and keeps the last
good snapshot — which is exactly why that hook matters for a flaky upstream.

**A config file watched on the refresh interval** — read and parse the file in
`Load`; the client re-reads it every interval, so editing the file rolls out the
change without a restart.

Whatever the backend, the GrowthBook-subset JSON shape is the contract: produce
that shape and any source works.

## Testing with flags

The static source makes flag-dependent code trivial to test without a database:

```go
src, _ := flagpole.StaticSourceFromJSON([]byte(`{
  "features": {"new-thing": {"defaultValue": false,
    "rules": [{"condition": {"plan": "pro"}, "force": true}]}}
}`))
c, _ := flagpole.New(ctx, src, flagpole.WithRefreshInterval(0)) // load once, never refresh
defer c.Close()

// pro user gets it, free user doesn't:
require.True(t,  c.For(flagpole.Attributes{"id": "u1", "plan": "pro"}).IsOn("new-thing"))
require.False(t, c.For(flagpole.Attributes{"id": "u2", "plan": "free"}).IsOn("new-thing"))
```

`WithRefreshInterval(0)` (or any `≤ 0`) loads once and never starts the
background ticker — handy for deterministic tests.

On the **client side**, the same allow-list/provider split makes component tests
easy — render under a provider with a hard-coded map:

```tsx
render(
  <FlagsProvider flags={{ 'skip-on-sync': true }}>
    <RuleRow />
  </FlagsProvider>,
)
// and the off-case: no provider ⇒ useFlag returns false ⇒ affordance absent
```

For the `sourcepg` adapter itself, point a test at a real database via
`FLAGPOLE_TEST_DATABASE_URL`; without it those tests skip.

## Coming from GrowthBook

flagpole implements a **strict, tested subset** of GrowthBook's evaluation
algorithm, validated against GrowthBook's own published `cases.json` fixtures.
The point of the subset is portability: a flag written for flagpole loads into
GrowthBook unchanged, and **the same users stay in the same cohorts** if you
switch either direction, because both use FNV-1a v2 hashing.

The two things to internalize:

1. **Unsupported features are skipped, never silently mis-evaluated.** Out of
   scope: experiment `variations`/`weights`/`key`; operators beyond
   equality/`$eq`/`$ne`/`$in` (so no `$gt`, `$regex`, `$or`, `$and`, `$not`,
   etc.); `range`, `filters`, `parentConditions`. A rule using any of these is
   dropped and evaluation falls through to the next rule. If a rule "isn't
   firing," check it isn't using an unsupported operator.

2. **Set `hashVersion: 2` explicitly on rollout rules you intend to share with
   GrowthBook.** flagpole buckets with v2 and treats a missing `hashVersion` as
   v2 — but GrowthBook's default is v1. A rule requesting any version other than
   2 is skipped. Being explicit keeps bucketing identical across both systems.

## Anti-patterns and gotchas

- **Don't create a client per request.** `flagpole.New` spins up a background
  refresher; build one client at startup, hold it for the process lifetime,
  `Close()` on shutdown.
- **Don't mutate `StaticSource.Features` after handing it over** — `Load`
  returns it by reference and the mutation bypasses the client's lock. Use a real
  Source for runtime changes.
- **Don't skip the error hook on a non-static source.** Failed refreshes are
  silent by design; without `WithOnError` + `LastRefresh()` alerting, a wedged
  cache is invisible.
- **Don't pass `[]byte` to a jsonb column under pgx's simple query protocol** —
  marshal then pass as `string`, or you'll get "invalid input syntax for type
  json."
- **Don't rebuild attributes per flag** when several flags share a unit (and
  especially when building them costs a query) — build once, reuse the
  `Evaluation`.
- **Don't leak definitions or attributes to the browser.** Server-evaluate a
  small curated allow-list and send booleans.
- **Keep `"id"` stable per user.** It's the default bucketing key; if it changes,
  the user can flip cohorts mid-rollout.
