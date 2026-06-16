# @sudarkoff/flagpole-react — Design Spec

**Date:** 2026-06-16
**Status:** Approved, ready for planning

---

## Scope

A small React companion package for flagpole. The host application evaluates
flags **server-side** (via the flagpole Go library) and embeds the resulting
`{key: value}` map in its bootstrap payload (e.g. twocal's `/user` response).
`@sudarkoff/flagpole-react` exposes that map to the component tree through a provider and
two hooks whose names and signatures mirror GrowthBook's React SDK.

**In scope:**
- `FlagsProvider`, `useFeatureIsOn`, `useFeatureValue`.
- Truthiness semantics identical to the Go library's evaluator.
- Reactivity to a changing `flags` prop (so host refetches propagate).
- Package lives in the flagpole repo under `react/`, published as `@sudarkoff/flagpole-react`.

**Out of scope (YAGNI):**
- Any client-side evaluator or hashing (evaluation is server-side; the browser
  never sees flag rules). This is the decided model — not a deferral.
- Built-in fetching/polling. The package is **fetch-agnostic**; the host owns
  fetching and refresh cadence.
- A `useFlags()` escape hatch, async/loading states, or suspense integration.

---

## Decisions of record

- **Pre-evaluated model.** The provider receives an already-evaluated map; no
  evaluation happens in the browser.
- **Monorepo placement.** `react/` subdirectory in the flagpole Go repo, with its
  own `package.json`/`tsconfig`/test+build config. Published as `@sudarkoff/flagpole-react`
  (published under the `@sudarkoff` npm scope).
- **Fetch-agnostic.** The package only reacts to the `flags` prop; the host
  decides when to supply a fresh map.

---

## Public API

```ts
export type FlagValue = boolean | string | number | null;
export type Flags = Record<string, FlagValue>;

export function FlagsProvider(props: {
  flags: Flags;
  children: React.ReactNode;
}): JSX.Element;

export function useFeatureIsOn(key: string): boolean;

export function useFeatureValue<T extends FlagValue>(key: string, defaultValue: T): T;
```

- **`FlagsProvider`** holds `flags` in a React context. A missing/`undefined`
  `flags` prop is treated as `{}` (defensive). The context value is the **live
  prop on every render** — never captured into `useState` on mount — so a new map
  from the host updates all consumers (see Reactivity).
- **`useFeatureIsOn(key)`** returns the truthiness of `flags[key]`. Unknown flags
  are off.
- **`useFeatureValue(key, default)`** returns `flags[key]` when the key is present
  and the value is not `null`; otherwise `default`. Generic over `FlagValue` for
  type inference from `default`.

### Truthiness — must match the Go library

`useFeatureIsOn` mirrors `flagpole.Evaluate`'s `truthy` so the browser agrees
with the server: the following are **off** — `undefined`, `null`, `false`, `0`,
`""`, `"false"`, `"0"`. Everything else is **on**. This is the only real logic in
the package; everything else is context plumbing.

---

## Reactivity & refresh

The Go `Client` refreshes its ruleset on a ticker, so server-evaluated flag
values change over time (admin toggles in custops, rollout ramps). The package
handles this by being **fully reactive to the `flags` prop**: because the
provider passes the live prop as the context value, any time the host renders
`<FlagsProvider>` with a new `flags` object, the context updates and every
`useFeatureIsOn`/`useFeatureValue` consumer re-renders with fresh values. The
package caches nothing stale.

Freshness is therefore a **host concern**, and the package supports any strategy:

- **Recommended:** fetch the bootstrap through a query library (twocal uses
  TanStack Query) with `refetchOnWindowFocus` and/or a `refetchInterval`; pass
  `data.flags` to the provider. Worst-case staleness ≈ backend tick + refetch
  interval.
- A dedicated lightweight poll endpoint (a twocal-integration detail, not part of
  this package).

Notes documented in the README:
- For purely **backend** flags (e.g. sync-engine gates), SPA freshness is moot —
  those decisions are server-side.
- **Kill-switch latency** is the sum of the backend refresh and host refetch
  intervals; lower both if a UI kill-switch must propagate in seconds.

---

## Packaging, build & tooling

- **Location:** `react/` in the flagpole repo. `package.json` `name`:
  `@sudarkoff/flagpole-react`.
- **Build:** TypeScript compiled with **tsup** (esbuild) to **ESM + CJS + `.d.ts`**.
- **Dependencies:** no runtime deps. `react` is a **peerDependency** (`>=18`,
  matching twocal). Dev deps: `typescript`, `tsup`, `vitest`,
  `@testing-library/react`, `jsdom`, `react`, `react-dom`, `@types/react`.
- **CI:** a JavaScript job added alongside the existing Go CI
  (`.github/workflows/ci.yml` or a sibling workflow) that runs typecheck + tests
  for the `react/` package.

---

## Testing

Vitest + `@testing-library/react` (jsdom):

- `FlagsProvider` supplies flags to consumers; `undefined` prop → `{}`.
- `useFeatureIsOn` truthiness table: `true`/`false`/`0`/`""`/`"false"`/`"0"`/
  number-nonzero/string-nonempty/missing key.
- `useFeatureValue`: present value returned; missing key → default; `null` →
  default; type inference from the default.
- **Reactivity:** rerender the provider with a changed `flags` object and assert
  consumers reflect the new values (the core freshness guarantee).

---

## Docs

- `react/README.md`: install, `<FlagsProvider flags={user.flags}>` + hooks usage,
  the "values come pre-evaluated from your flagpole-backed server" note, and the
  host refresh patterns (TanStack Query example).
- Cross-link from the main `README.md` roadmap section.

---

## File structure (informs the plan)

| Path | Responsibility |
|------|----------------|
| `react/package.json` | Package manifest, scripts, peer/dev deps |
| `react/tsconfig.json` | TS config |
| `react/tsup.config.ts` | Build config (ESM+CJS+dts) |
| `react/vitest.config.ts` | Test config (jsdom) |
| `react/src/context.ts` | `Flags`/`FlagValue` types + the React context |
| `react/src/provider.tsx` | `FlagsProvider` |
| `react/src/truthy.ts` | Go-matching truthiness helper |
| `react/src/hooks.ts` | `useFeatureIsOn`, `useFeatureValue` |
| `react/src/index.ts` | Public exports |
| `react/src/*.test.tsx` | Vitest tests |
| `react/README.md` | Package docs |
