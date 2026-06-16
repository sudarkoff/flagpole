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

describe("FlagsProvider without flags", () => {
  it("treats an omitted flags prop as empty (all flags off)", () => {
    const view = render(
      <FlagsProvider>
        <IsOnProbe flag="anything" />
      </FlagsProvider>,
    );
    expect(view.container.textContent).toBe("off");
  });
});

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
