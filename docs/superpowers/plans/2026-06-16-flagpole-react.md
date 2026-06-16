# @flagpole/react Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `@flagpole/react`, a tiny React package exposing a pre-evaluated flag map (from a flagpole-backed server) through `FlagsProvider` + `useFeatureIsOn`/`useFeatureValue` hooks, living in the flagpole repo under `react/`.

**Architecture:** A React context holds the host-supplied `{key: value}` map; the provider passes the **live prop** as the context value (so host refetches propagate); two hooks read context. The only real logic is a `truthy` helper that mirrors the Go evaluator's truthiness, so the browser agrees with server-side evaluation. No client-side evaluation, no fetching, no runtime dependencies.

**Tech Stack:** TypeScript, React (peer `>=18`), tsup (ESM+CJS+dts build), Vitest + @testing-library/react (jsdom).

**Source spec:** `docs/superpowers/specs/2026-06-16-flagpole-react-design.md`

**Working directory:** all paths below are relative to the flagpole repo root; the package lives in `react/`. Run npm/test commands from inside `react/`.

---

## File Structure

| Path | Responsibility |
|------|----------------|
| `react/package.json` | Manifest, scripts, peer/dev deps, npm name `@flagpole/react` |
| `react/tsconfig.json` | TypeScript config |
| `react/tsup.config.ts` | Build (ESM+CJS+dts) |
| `react/vitest.config.ts` | Test config (jsdom env) |
| `react/src/context.ts` | `FlagValue`/`Flags` types + the React context |
| `react/src/truthy.ts` | Go-matching truthiness helper |
| `react/src/provider.tsx` | `FlagsProvider` |
| `react/src/hooks.ts` | `useFeatureIsOn`, `useFeatureValue` |
| `react/src/index.ts` | Public exports |
| `react/src/truthy.test.ts` | truthy unit tests |
| `react/src/hooks.test.tsx` | provider + hooks + reactivity tests |
| `react/README.md` | Package docs |
| `.github/workflows/ci.yml` | add a `react` job |
| `README.md` | cross-link from roadmap |

---

## Task 1: Scaffold the `react/` package

**Files:** Create `react/package.json`, `react/tsconfig.json`, `react/tsup.config.ts`, `react/vitest.config.ts`.

- [ ] **Step 1: Create `react/package.json`**

```json
{
  "name": "@flagpole/react",
  "version": "0.1.0",
  "description": "React hooks for flagpole feature flags — pre-evaluated, GrowthBook-compatible names.",
  "license": "Apache-2.0",
  "type": "module",
  "main": "./dist/index.cjs",
  "module": "./dist/index.js",
  "types": "./dist/index.d.ts",
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "import": "./dist/index.js",
      "require": "./dist/index.cjs"
    }
  },
  "files": ["dist"],
  "sideEffects": false,
  "scripts": {
    "build": "tsup",
    "test": "vitest run",
    "typecheck": "tsc --noEmit"
  },
  "peerDependencies": {
    "react": ">=18"
  },
  "devDependencies": {
    "@testing-library/react": "^16.1.0",
    "@types/react": "^18.3.12",
    "jsdom": "^25.0.1",
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "tsup": "^8.3.5",
    "typescript": "^5.6.3",
    "vitest": "^2.1.8"
  }
}
```

- [ ] **Step 2: Create `react/tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "jsx": "react-jsx",
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "types": ["vitest/globals"],
    "strict": true,
    "declaration": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "noEmit": true
  },
  "include": ["src"]
}
```

- [ ] **Step 3: Create `react/tsup.config.ts`**

```ts
import { defineConfig } from "tsup";

export default defineConfig({
  entry: ["src/index.ts"],
  format: ["esm", "cjs"],
  dts: true,
  clean: true,
  sourcemap: true,
  external: ["react"],
});
```

- [ ] **Step 4: Create `react/vitest.config.ts`**

```ts
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    globals: true,
  },
});
```

> `globals: true` enables Testing Library's automatic per-test cleanup. Tests still import `describe`/`it`/`expect` from `vitest` explicitly.

- [ ] **Step 5: Install dependencies**

Run:
```bash
cd react
npm install
```
Expected: creates `node_modules/` and `react/package-lock.json`. (No source yet, so nothing to build/test.)

- [ ] **Step 6: Commit**

```bash
git add react/package.json react/tsconfig.json react/tsup.config.ts react/vitest.config.ts react/package-lock.json
git commit -m "chore(react): scaffold @flagpole/react package"
```

---

## Task 2: Types, context, and the truthy helper (TDD)

**Files:** Create `react/src/context.ts`, `react/src/truthy.ts`, `react/src/truthy.test.ts`.

- [ ] **Step 1: Write the failing test**

`react/src/truthy.test.ts`:
```ts
import { describe, it, expect } from "vitest";
import { truthy } from "./truthy";

describe("truthy (matches the flagpole Go evaluator)", () => {
  it("treats falsy values as off", () => {
    for (const v of [undefined, null, false, 0, "", "false", "0"] as const) {
      expect(truthy(v)).toBe(false);
    }
  });

  it("treats real values as on", () => {
    for (const v of [true, 1, -1, 3.14, "x", "true", "hello"] as const) {
      expect(truthy(v)).toBe(true);
    }
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd react && npx vitest run src/truthy.test.ts`
Expected: FAIL (cannot resolve `./truthy`).

- [ ] **Step 3: Implement context and truthy**

`react/src/context.ts`:
```ts
import { createContext } from "react";

export type FlagValue = boolean | string | number | null;
export type Flags = Record<string, FlagValue>;

/** FlagsContext holds the host-supplied, pre-evaluated flag map. */
export const FlagsContext = createContext<Flags>({});
```

`react/src/truthy.ts`:
```ts
import type { FlagValue } from "./context";

/**
 * truthy mirrors the flagpole Go evaluator's truthiness so browser results agree
 * with server-side evaluation. Off: undefined, null, false, 0, "", "false", "0".
 * Everything else is on.
 */
export function truthy(v: FlagValue | undefined): boolean {
  if (v === undefined || v === null) return false;
  if (typeof v === "boolean") return v;
  if (typeof v === "number") return v !== 0;
  if (typeof v === "string") return v !== "" && v !== "false" && v !== "0";
  return true;
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd react && npx vitest run src/truthy.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add react/src/context.ts react/src/truthy.ts react/src/truthy.test.ts
git commit -m "feat(react): flag types, context, and Go-matching truthiness"
```

---

## Task 3: FlagsProvider (TDD)

**Files:** Create `react/src/provider.tsx`. Test added in Task 5 (alongside the hooks, which exercise the provider). This task implements the provider and verifies it compiles/builds.

- [ ] **Step 1: Implement the provider**

`react/src/provider.tsx`:
```tsx
import type { ReactNode } from "react";
import { FlagsContext, type Flags } from "./context";

/**
 * FlagsProvider makes a pre-evaluated flag map available to descendant hooks.
 * The map is passed straight through as the context value (no internal caching),
 * so supplying a new `flags` object re-renders all consumers — this is how host
 * refetches propagate. A missing/undefined `flags` is treated as {}.
 */
export function FlagsProvider({
  flags,
  children,
}: {
  flags: Flags;
  children: ReactNode;
}) {
  return <FlagsContext.Provider value={flags ?? {}}>{children}</FlagsContext.Provider>;
}
```

- [ ] **Step 2: Typecheck**

Run: `cd react && npx tsc --noEmit`
Expected: PASS (no type errors). The provider is exercised by the hook tests in Task 5.

- [ ] **Step 3: Commit**

```bash
git add react/src/provider.tsx
git commit -m "feat(react): FlagsProvider"
```

---

## Task 4: useFeatureIsOn & useFeatureValue (TDD)

**Files:** Create `react/src/hooks.ts`.

- [ ] **Step 1: Write the failing test**

`react/src/hooks.test.tsx`:
```tsx
import { describe, it, expect } from "vitest";
import type { ReactNode } from "react";
import { render } from "@testing-library/react";
import { FlagsProvider } from "./provider";
import { useFeatureIsOn, useFeatureValue } from "./hooks";
import type { Flags } from "./context";

function IsOnProbe({ flag }: { flag: string }) {
  return <span>{useFeatureIsOn(flag) ? "on" : "off"}</span>;
}

function ValueProbe({ flag, def }: { flag: string; def: string }) {
  return <span>{useFeatureValue(flag, def)}</span>;
}

function withFlags(flags: Flags, node: ReactNode) {
  return render(<FlagsProvider flags={flags}>{node}</FlagsProvider>);
}

describe("useFeatureIsOn", () => {
  it("is on for truthy flags, off for falsy/missing", () => {
    expect(withFlags({ a: true }, <IsOnProbe flag="a" />).container.textContent).toBe("on");
    expect(withFlags({ a: false }, <IsOnProbe flag="a" />).container.textContent).toBe("off");
    expect(withFlags({ a: 0 }, <IsOnProbe flag="a" />).container.textContent).toBe("off");
    expect(withFlags({ a: "" }, <IsOnProbe flag="a" />).container.textContent).toBe("off");
    expect(withFlags({}, <IsOnProbe flag="missing" />).container.textContent).toBe("off");
  });
});

describe("useFeatureValue", () => {
  it("returns the value, or the default for missing/null", () => {
    expect(withFlags({ c: "red" }, <ValueProbe flag="c" def="blue" />).container.textContent).toBe("red");
    expect(withFlags({}, <ValueProbe flag="c" def="blue" />).container.textContent).toBe("blue");
    expect(withFlags({ c: null }, <ValueProbe flag="c" def="blue" />).container.textContent).toBe("blue");
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd react && npx vitest run src/hooks.test.tsx`
Expected: FAIL (cannot resolve `./hooks`).

- [ ] **Step 3: Implement the hooks**

`react/src/hooks.ts`:
```ts
import { useContext } from "react";
import { FlagsContext, type FlagValue } from "./context";
import { truthy } from "./truthy";

/** useFeatureIsOn returns whether a flag resolves to a truthy value. Unknown flags are off. */
export function useFeatureIsOn(key: string): boolean {
  return truthy(useContext(FlagsContext)[key]);
}

/** useFeatureValue returns a flag's value, or defaultValue when unknown or null. */
export function useFeatureValue<T extends FlagValue>(key: string, defaultValue: T): T {
  const v = useContext(FlagsContext)[key];
  return v === undefined || v === null ? defaultValue : (v as T);
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd react && npx vitest run src/hooks.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add react/src/hooks.ts react/src/hooks.test.tsx
git commit -m "feat(react): useFeatureIsOn and useFeatureValue hooks"
```

---

## Task 5: Reactivity test + public exports

**Files:** Create `react/src/index.ts`; add a reactivity test to `react/src/hooks.test.tsx`.

- [ ] **Step 1: Write the failing reactivity test**

Append this `describe` block to `react/src/hooks.test.tsx` (it reuses the `IsOnProbe`, `render`, and `FlagsProvider` already imported/defined at the top of that file — add no new imports). `rerender` is a method on the object returned by `render()`:
```tsx
describe("reactivity", () => {
  it("re-renders consumers when the flags prop changes", () => {
    const view = render(
      <FlagsProvider flags={{ a: false }}>
        <IsOnProbe flag="a" />
      </FlagsProvider>,
    );
    expect(view.container.textContent).toBe("off");

    // Host supplies a fresh map (e.g. after a refetch) -> consumers update.
    view.rerender(
      <FlagsProvider flags={{ a: true }}>
        <IsOnProbe flag="a" />
      </FlagsProvider>,
    );
    expect(view.container.textContent).toBe("on");
  });
});
```

- [ ] **Step 2: Run to verify the reactivity test fails or errors**

Run: `cd react && npx vitest run src/hooks.test.tsx`
Expected: the new test FAILS only if the provider captured state on mount. With the Task 3 provider (live prop), it should already PASS — in which case this test is a guard that locks the behavior in. If it FAILS, the provider is wrongly memoizing; fix `provider.tsx` to pass the live prop.

- [ ] **Step 3: Create the public exports**

`react/src/index.ts`:
```ts
export { FlagsProvider } from "./provider";
export { useFeatureIsOn, useFeatureValue } from "./hooks";
export type { Flags, FlagValue } from "./context";
```

- [ ] **Step 4: Run the full test suite + typecheck**

Run: `cd react && npx vitest run && npx tsc --noEmit`
Expected: all tests PASS, no type errors.

- [ ] **Step 5: Commit**

```bash
git add react/src/index.ts react/src/hooks.test.tsx
git commit -m "feat(react): public exports + reactivity guarantee test"
```

---

## Task 6: Build verification

**Files:** none new — verify `tsup` produces ESM + CJS + types.

- [ ] **Step 1: Build**

Run:
```bash
cd react && npm run build
```
Expected: a `react/dist/` directory containing `index.js` (ESM), `index.cjs` (CJS), and `index.d.ts`.

- [ ] **Step 2: Verify the artifacts**

Run:
```bash
cd react && ls dist && node -e "const m=require('./dist/index.cjs'); if(typeof m.useFeatureIsOn!=='function'||typeof m.FlagsProvider!=='function') {throw new Error('CJS export missing')}; console.log('cjs exports ok')"
```
Expected: `dist/` lists `index.js`, `index.cjs`, `index.d.ts` (+ sourcemaps); prints `cjs exports ok`.

- [ ] **Step 3: Confirm dist is gitignored, not committed**

Create `react/.gitignore`:
```
node_modules/
dist/
```
Run: `cd react && git status --short` — confirm `dist/` and `node_modules/` are not staged.

- [ ] **Step 4: Commit**

```bash
git add react/.gitignore
git commit -m "chore(react): ignore build output"
```

---

## Task 7: Docs + CI

**Files:** Create `react/README.md`; modify `.github/workflows/ci.yml`; modify root `README.md`.

- [ ] **Step 1: Write `react/README.md`**

```markdown
# @flagpole/react

React hooks for [flagpole](https://github.com/sudarkoff/flagpole) feature flags.

Your server evaluates flags with the flagpole Go library and embeds the resulting
`{key: value}` map in its bootstrap payload. `@flagpole/react` exposes that
**pre-evaluated** map to your components — no flag rules or evaluation logic ship
to the browser.

## Install

```
npm install @flagpole/react
```

`react >= 18` is a peer dependency.

## Usage

```tsx
import { FlagsProvider, useFeatureIsOn, useFeatureValue } from "@flagpole/react";

function App({ flags }) {
  return (
    <FlagsProvider flags={flags}>
      <Checkout />
    </FlagsProvider>
  );
}

function Checkout() {
  const newFlow = useFeatureIsOn("new-checkout");
  const color = useFeatureValue("button-color", "blue");
  return newFlow ? <NewCheckout color={color} /> : <OldCheckout />;
}
```

- `useFeatureIsOn(key)` — truthiness of the flag; unknown flags are off. Matches the
  Go evaluator's truthiness exactly (off for `false`, `0`, `""`, `"false"`, `"0"`, null).
- `useFeatureValue(key, default)` — the flag's value, or `default` when unknown/null.

## Keeping flags fresh

The provider re-renders all consumers whenever you pass a new `flags` object, so
freshness is just a matter of how often you refetch the map. With a query library:

```tsx
const { data } = useQuery(["user"], fetchUser, {
  refetchInterval: 60_000,
  refetchOnWindowFocus: true,
});

<FlagsProvider flags={data?.flags ?? {}}>
  <App />
</FlagsProvider>;
```

Backend-only flags (e.g. server gates) don't depend on SPA freshness at all. For a
UI kill-switch, latency is the backend refresh interval plus your refetch interval.

## License

Apache-2.0.
```

- [ ] **Step 2: Add a `react` CI job**

In `.github/workflows/ci.yml`, add a job alongside the existing Go `test` job:
```yaml
  react:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: react
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
      - run: npm ci
      - run: npm run typecheck
      - run: npm test
```

- [ ] **Step 3: Cross-link from the root README**

In the root `README.md` "Roadmap" section, replace the `@flagpole/react` bullet's "planned" wording with a shipped link, e.g.:
```markdown
- **[`@flagpole/react`](./react)** — `FlagsProvider` + `useFeatureIsOn`/`useFeatureValue`, hydrated from your server's pre-evaluated flag map.
```

- [ ] **Step 4: Final verification**

Run: `cd react && npm run typecheck && npm test && npm run build`
Expected: typecheck clean, all tests pass, build emits ESM+CJS+dts.

- [ ] **Step 5: Commit**

```bash
git add react/README.md .github/workflows/ci.yml README.md
git commit -m "docs(react): README, CI job, root README cross-link"
```

---

## Notes for the implementer

- **No runtime deps.** Keep `dependencies` empty; `react` stays a peer dependency.
- **Don't over-build.** No client-side evaluator, no fetching, no `useFlags()` — the host owns fetching and passes the map (see spec "Out of scope").
- **The reactivity test is the important one** — it locks in the freshness guarantee that the whole pre-evaluated model depends on.
- **npm vs pnpm:** the plan uses `npm`; if the repo later standardizes on another package manager, adjust the lockfile and CI accordingly.
