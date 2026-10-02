import { describe, expect, it } from "vitest";
import { CUSTOM_CSS_MAX_BYTES, MAX_FONT_BYTES, cssByteLength, fontFileProblem } from "./proStyle";

describe("custom CSS size", () => {
  it("counts UTF-8 bytes, not characters", () => {
    expect(cssByteLength("a{}")).toBe(3);
    expect(cssByteLength('a::before{content:"é"}')).toBe(23);
    expect(CUSTOM_CSS_MAX_BYTES).toBe(20480);
  });
});

describe("font files", () => {
  it("accepts font extensions in any case", () => {
    expect(fontFileProblem({ name: "Inter.woff2", size: 1000 })).toBeNull();
    expect(fontFileProblem({ name: "inter.WOFF", size: 1000 })).toBeNull();
    expect(fontFileProblem({ name: "a.ttf", size: 1000 })).toBeNull();
    expect(fontFileProblem({ name: "a.otf", size: MAX_FONT_BYTES })).toBeNull();
  });

  it("rejects other files and large fonts", () => {
    expect(fontFileProblem({ name: "a.svg", size: 10 })).toMatch(/WOFF2/);
    expect(fontFileProblem({ name: "woff2", size: 10 })).toMatch(/WOFF2/);
    expect(fontFileProblem({ name: "a.woff2", size: MAX_FONT_BYTES + 1 })).toMatch(/1 MB/);
  });
});
