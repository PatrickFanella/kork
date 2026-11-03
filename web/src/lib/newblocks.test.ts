import { describe, expect, it } from "vitest";
import { blockFields, blockSummary, cleanBlockData, missingRequired } from "./blocks";
import { eventsFields, eventsProblem, feedStatus, upcomingEvents } from "./newblocks";

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

  it("keeps a list ID the server returned as a number", () => {
    // The API stores brevo_list_id as a number; dropping it made every edit fail.
    const saved = { title: "News", destination: "brevo", brevo_list_id: 7, brevo_key_set: true } as never;
    expect(cleanBlockData("email_signup", saved)).toEqual({ title: "News", destination: "brevo", brevo_list_id: "7" });
  });
});

describe("listmonk sign-up form", () => {
  const keys = (data: Record<string, unknown>, listmonk = true) =>
    blockFields("email_signup", undefined, { listmonk, data: data as never }).map((f) => f.key);

  it("shows listmonk fields only for listmonk lists, and the token is optional once saved", () => {
    expect(keys({ destination: "kork" })).not.toContain("listmonk_api_token");
    expect(keys({ destination: "listmonk" })).toEqual(
      expect.arrayContaining(["listmonk_url", "listmonk_list_id", "listmonk_user", "listmonk_api_token"]),
    );
    const saved = blockFields("email_signup", undefined, { listmonk: true, data: { destination: "listmonk", listmonk_key_set: true } });
    expect(
      missingRequired(saved, {
        title: "News",
        destination: "listmonk",
        listmonk_url: "https://lists.example.com",
        listmonk_list_id: "3",
        listmonk_user: "kork",
        listmonk_key_set: true,
      }),
    ).toEqual({});
  });

  it("offers listmonk only when the server can use it", () => {
    const dest = (listmonk: boolean) =>
      blockFields("email_signup", undefined, { listmonk, data: { destination: "kork" } }).find((f) => f.key === "destination");
    expect(dest(false)?.options?.map((o) => o.key)).toEqual(["kork"]);
    expect(dest(true)?.options?.map((o) => o.key)).toEqual(["kork", "listmonk"]);
  });

  it("drops listmonk settings for other lists and never sends listmonk_key_set", () => {
    expect(cleanBlockData("email_signup", { title: "News", destination: "kork", listmonk_url: "https://x.example", listmonk_list_id: 3 })).toEqual({
      title: "News",
      destination: "kork",
    });
    expect(
      cleanBlockData("email_signup", {
        title: "News",
        destination: "listmonk",
        listmonk_url: "https://lists.example.com",
        listmonk_list_id: 3,
        listmonk_user: "kork",
        listmonk_api_token: "",
        listmonk_key_set: true,
      }),
    ).toEqual({ title: "News", destination: "listmonk", listmonk_url: "https://lists.example.com", listmonk_list_id: "3", listmonk_user: "kork" });
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

describe("calendar feed events", () => {
  it("asks for an address instead of dates when reading a feed", () => {
    expect(eventsFields({ data: { source: "ics" } }).map((f) => f.key)).toEqual(["source", "ics_url"]);
    expect(eventsFields({ data: {} }).map((f) => f.key)).toEqual(["source", "events"]);
    expect(cleanBlockData("events", { source: "ics", ics_url: " webcal://c.example/a.ics ", events: [] })).toEqual({
      source: "ics",
      ics_url: "webcal://c.example/a.ics",
    });
  });

  it("describes the last fetch", () => {
    const base = { checked_at: "2026-10-02T12:00:00Z", fetched_at: "2026-10-02T12:00:00Z", dropped: 0, error: "" };
    expect(feedStatus({ ...base, found: 5 })).toEqual({ text: "5 upcoming dates from the calendar.", problem: false });
    expect(feedStatus({ ...base, found: 1, error: "Couldn't reach the calendar." })?.problem).toBe(true);
    expect(feedStatus(undefined)).toBeNull();
  });
});
