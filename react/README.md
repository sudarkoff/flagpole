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
