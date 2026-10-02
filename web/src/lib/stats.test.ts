import { describe, expect, it } from "vitest";
import { countryLabel, formatPercent, referrerLabel } from "./stats";

describe("stats labels", () => {
  it("names the special referrer rows", () => {
    expect(referrerLabel("direct")).toBe("Direct or unknown");
    expect(referrerLabel("other")).toBe("Other sites");
    expect(referrerLabel("instagram.com")).toBe("instagram.com");
  });

  it("turns country codes into names", () => {
    expect(countryLabel("DE", "en")).toBe("Germany");
    expect(countryLabel("unknown")).toBe("Unknown");
    expect(countryLabel("zz")).toBe("Unknown");
  });

  it("formats click-through", () => {
    expect(formatPercent(0)).toBe("0%");
    expect(formatPercent(0.25)).toBe("25%");
    expect(formatPercent(0.0333)).toBe("3.3%");
    expect(formatPercent(1.5)).toBe("150%");
  });
});
