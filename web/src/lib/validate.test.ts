import { describe, expect, it } from "vitest";
import {
  contrastRatio,
  fillDays,
  hostnameProblem,
  matchesQuery,
  moveItem,
  normalizeHex,
  normalizeHostname,
  normalizeSlug,
  passwordProblem,
  slugProblem,
  stripPrefix,
  uploadProblem,
} from "./validate";

describe("slugProblem", () => {
  it("accepts valid slugs", () => {
    for (const s of ["abc", "alice", "a-b_c", "0day", "x".repeat(32)]) {
      expect(slugProblem(s)).toBeNull();
    }
  });
  it("rejects bad slugs with a reason", () => {
    expect(slugProblem("")).toMatch(/Choose/);
    expect(slugProblem("ab")).toMatch(/at least 3/);
    expect(slugProblem("x".repeat(33))).toMatch(/at most 32/);
    expect(slugProblem("-abc")).toMatch(/Start with/);
    expect(slugProblem("_abc")).toMatch(/Start with/);
    expect(slugProblem("Abc")).toMatch(/Start with/);
    expect(slugProblem("ab.c")).toMatch(/lowercase/);
  });
  it("normalizes typed input", () => {
    expect(normalizeSlug("  My Page ")).toBe("my-page");
  });
});

describe("other validators", () => {
  it("passwords need 10 characters", () => {
    expect(passwordProblem("short")).not.toBeNull();
    expect(passwordProblem("long enough")).toBeNull();
  });
  it("hostnames", () => {
    expect(normalizeHostname("https://Links.Example.com/path")).toBe("links.example.com");
    expect(normalizeHostname("example.com.")).toBe("example.com");
    expect(hostnameProblem("links.example.com")).toBeNull();
    expect(hostnameProblem("localhost")).not.toBeNull();
    expect(hostnameProblem("-bad.example.com")).not.toBeNull();
    expect(hostnameProblem("")).not.toBeNull();
  });
  it("uploads", () => {
    expect(uploadProblem({ size: 100, type: "image/png" })).toBeNull();
    expect(uploadProblem({ size: 100, type: "image/svg+xml" })).toMatch(/PNG/);
    expect(uploadProblem({ size: 6 * 1024 * 1024, type: "image/jpeg" })).toMatch(/5 MB/);
  });
  it("hex colors", () => {
    expect(normalizeHex("#ABC")).toBe("#aabbcc");
    expect(normalizeHex("c05cff")).toBe("#c05cff");
    expect(normalizeHex("red")).toBeNull();
  });
  it("prefix and query helpers", () => {
    expect(stripPrefix("@alice", "@")).toBe("alice");
    expect(stripPrefix(" alice ", "@")).toBe("alice");
    expect(matchesQuery("insta", "Instagram", "social")).toBe(true);
    expect(matchesQuery("", "x")).toBe(true);
    expect(matchesQuery("zz", "Instagram")).toBe(false);
  });
});

describe("moveItem", () => {
  it("moves within bounds", () => {
    expect(moveItem(["a", "b", "c"], 0, 1)).toEqual(["b", "a", "c"]);
    expect(moveItem(["a", "b", "c"], 2, -1)).toEqual(["a", "c", "b"]);
  });
  it("ignores moves past the ends", () => {
    expect(moveItem(["a", "b"], 0, -1)).toEqual(["a", "b"]);
    expect(moveItem(["a", "b"], 1, 1)).toEqual(["a", "b"]);
  });
});

describe("fillDays", () => {
  it("fills missing days with zeros, oldest first", () => {
    const out = fillDays([{ day: "2026-09-30", count: 2 }], 3, "2026-10-01");
    expect(out).toEqual([
      { day: "2026-09-29", count: 0 },
      { day: "2026-09-30", count: 2 },
      { day: "2026-10-01", count: 0 },
    ]);
  });
});

describe("contrastRatio", () => {
  it("matches known WCAG values", () => {
    expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 1);
    expect(contrastRatio("#777777", "#ffffff")).toBeCloseTo(4.48, 1);
    expect(contrastRatio("#fff", "#fff")).toBeCloseTo(1, 5);
    expect(contrastRatio("nope", "#fff")).toBeNull();
  });
});
