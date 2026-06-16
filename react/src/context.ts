import { createContext } from "react";

export type FlagValue = boolean | string | number | null;
export type Flags = Record<string, FlagValue>;

/** FlagsContext holds the host-supplied, pre-evaluated flag map. */
export const FlagsContext = createContext<Flags>({});
