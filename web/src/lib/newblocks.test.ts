import { describe, expect, it } from "vitest";
import { blockFields, blockSummary, cleanBlockData, missingRequired } from "./blocks";
import { eventsProblem, upcomingEvents } from "./newblocks";

describe("email sign-up form", () => {
  it("shows Brevo fields only for Brevo lists, and the key is optional once saved", () => {
    const keys = (data: Record<string, unknown>, brevo = true) =>
      blockFields("email_signup", undefined, { brevo, data: data as never }).map((f) => f.key);
    expect(keys({ destination: "kork" })).not.toContain("brevo_api_key");
    expect(keys({ destination: "brevo" })).toEqual(expect.arrayContaining(["brevo_list_id", "brevo_api_key"]));
    const fresh = blockFields("email_signup", undefined, { brevo: true, data: { destination: "brevo" } });
    expect(Object.keys(missingRequired(fresh, { title: "News", destination: "brevo", brevo_list_id: "7" }))).toEqual(["brevo_api_key"]);
    const saved = blockFields("email_signup", undefined, { brevo: true, data: { destination: "brevo", brevo_key_set: true } });
    expect(missingRequired(saved, { title: "News", destination: "brevo", brevo_list_id: "7", brevo_key_set: true })).toEqual({});
  });

  it("hides the Brevo option when the server can't use it", () => {
    const dest = blockFields("email_signup", undefined, { brevo: false, data: { destination: "kork" } }).find((f) => f.key === "destination");
    expect(dest?.options?.map((o) => o.key)).toEqual(["kork"]);
  });

  it("drops Brevo settings for Kork lists and never sends brevo_key_set", () => {
    expect(cleanBlockData("email_signup", { title: " News ", destination: "kork", brevo_list_id: "7", brevo_api_key: "k" })).toEqual({
      title: "News",
      destination: "kork",
    });
    expect(
      cleanBlockData("email_signup", { title: "News", destination: "brevo", brevo_list_id: " 7 ", brevo_api_key: "", brevo_key_set: true }),
    ).toEqual({ title: "News", destination: "brevo", brevo_list_id: "7" });
  });
});

describe("events", () => {
  it("cleans each date", () => {
    expect(
      cleanBlockData("events", {
        events: [{ date: "2026-10-09", time: "", title: " The Hall ", city: " ", ticket_url: " https://t.example ", sold_out: false }],
      }),
    ).toEqual({ events: [{ date: "2026-10-09", title: "The Hall", ticket_url: "https://t.example" }] });
  });

  it("reports the first incomplete date", () => {
    expect(eventsProblem([])).toBe("Add at least one date.");
    expect(eventsProblem([{ date: "2026-10-09", title: "A" }, { date: "", title: "B" }])).toBe("Event 2: choose a date.");
    expect(eventsProblem([{ date: "2026-10-09", title: " " }])).toBe("Event 1: enter a title or venue.");
    expect(missingRequired(blockFields("events"), { events: [{ date: "2026-10-09", title: "A" }] })).toEqual({});
  });

  it("summarises the next upcoming date", () => {
    const now = new Date(2026, 9, 2, 12);
    const events = [
      { date: "2026-09-30", title: "Past" },
      { date: "2026-10-20", title: "Later" },
      { date: "2026-10-09", time: "20:00", title: "Next" },
    ];
    expect(upcomingEvents(events, now).map((e) => e.title)).toEqual(["Next", "Later"]);
    expect(blockSummary("events", { events })).toMatch(/^3 dates/);
  });
});

describe("latest", () => {
  it("summarises the source without the YouTube origin", () => {
    expect(blockSummary("latest", { source: "https://www.youtube.com/@alice" })).toBe("@alice");
    expect(blockSummary("latest", { source: "https://alice.example/feed.xml" })).toBe("https://alice.example/feed.xml");
  });
});
