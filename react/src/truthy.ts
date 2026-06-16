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
