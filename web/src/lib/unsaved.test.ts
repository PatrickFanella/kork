import { describe, expect, it } from "vitest";
import { confirmLeave, hasUnsaved, markForTest } from "./unsaved";

describe("unsaved changes", () => {
  it("asks only when a form has unsaved changes", () => {
    let asked = 0;
    const ask = () => {
      asked++;
      return false;
    };
    expect(hasUnsaved()).toBe(false);
    expect(confirmLeave(ask)).toBe(true);
    expect(asked).toBe(0);
    markForTest("profile", true);
    expect(confirmLeave(ask)).toBe(false);
    expect(confirmLeave(() => true)).toBe(true);
    markForTest("profile", false);
    expect(confirmLeave(ask)).toBe(true);
    expect(asked).toBe(1);
    // A self-saving form (the theme) doesn't make leaving ask.
    markForTest("theme", true, false);
    expect(confirmLeave(ask)).toBe(true);
    expect(asked).toBe(1);
    markForTest("theme", false);
  });
});
