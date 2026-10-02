import { describe, expect, it } from "vitest";
import { fromLocalInput, scheduleBadge, scheduleError, scheduleState, toLocalInput } from "./schedule";

describe("local input conversion", () => {
  it("sends local wall time as UTC without milliseconds", () => {
    const expected = new Date(2026, 9, 2, 13, 30).toISOString().replace(".000Z", "Z");
    expect(fromLocalInput("2026-10-02T13:30")).toBe(expected);
    expect(fromLocalInput("")).toBeNull();
    expect(fromLocalInput("not a date")).toBeNull();
  });

  it("round-trips through the input format", () => {
    expect(toLocalInput(fromLocalInput("2026-12-31T23:59"))).toBe("2026-12-31T23:59");
    expect(toLocalInput(null)).toBe("");
    expect(toLocalInput("garbage")).toBe("");
  });
});

describe("schedule rules", () => {
  const start = "2026-10-02T18:00:00Z";
  const end = "2026-10-02T20:00:00Z";
  const t = (iso: string) => Date.parse(iso);

  it("rejects an end at or before the start", () => {
    expect(scheduleError({ starts_at: start, ends_at: start })).toMatch(/after the start/);
    expect(scheduleError({ starts_at: end, ends_at: start })).toMatch(/after the start/);
    expect(scheduleError({ starts_at: start, ends_at: end })).toBeNull();
    expect(scheduleError({ starts_at: null, ends_at: end })).toBeNull();
  });

  it("includes the start and excludes the end", () => {
    const s = { starts_at: start, ends_at: end };
    expect(scheduleState(s, t(start) - 1)).toBe("scheduled");
    expect(scheduleState(s, t(start))).toBe("live");
    expect(scheduleState(s, t(end) - 1)).toBe("live");
    expect(scheduleState(s, t(end))).toBe("expired");
    expect(scheduleState({ starts_at: null, ends_at: null }, t(start))).toBe("always");
  });

  it("labels the block list", () => {
    expect(scheduleBadge({ starts_at: null, ends_at: null }, t(start))).toBeNull();
    expect(scheduleBadge({ starts_at: start, ends_at: null }, t(start) - 1)).toMatch(/^Shows from /);
    expect(scheduleBadge({ starts_at: start, ends_at: null }, t(start))).toBeNull();
    expect(scheduleBadge({ starts_at: null, ends_at: end }, t(start))).toMatch(/^Until /);
    expect(scheduleBadge({ starts_at: null, ends_at: end }, t(end))).toMatch(/^Expired /);
  });
});
