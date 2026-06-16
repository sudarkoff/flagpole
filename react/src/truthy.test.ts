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
