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
  flags?: Flags;
  children: ReactNode;
}) {
  return <FlagsContext.Provider value={flags ?? {}}>{children}</FlagsContext.Provider>;
}
