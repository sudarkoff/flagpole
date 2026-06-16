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
