// Form schemas and helpers for the email sign-up, events and latest blocks.

import type { BlockData, EventItem } from "../api";
import type { FieldSpec } from "./blocks";

export const MAX_EVENTS = 50;

export interface FieldContext {
  /** The server can send sign-ups to Brevo (catalog feature). */
  brevo?: boolean;
  /** Current form data, for fields that depend on other fields. */
  data?: BlockData;
}

export function signupFields(ctx: FieldContext = {}): FieldSpec[] {
  const data = ctx.data ?? {};
  const brevo = data.destination === "brevo";
  const keySet = data.brevo_key_set === true;
  const fields: FieldSpec[] = [
    { key: "title", label: "Heading", kind: "text", required: true, maxLength: 80, placeholder: "Join my newsletter" },
    { key: "description", label: "Description", kind: "text", maxLength: 200, placeholder: "One email a month about new work." },
    { key: "button_label", label: "Button text", kind: "text", maxLength: 30, placeholder: "Subscribe" },
    {
      key: "destination",
      label: "Where sign-ups go",
      kind: "select",
      options: [
        { key: "kork", name: "Keep the list here (download as CSV)" },
        ...(ctx.brevo || brevo ? [{ key: "brevo", name: "Add them to my Brevo list" }] : []),
      ],
      hint: brevo
        ? "Visitors confirm by email first. Confirmed addresses go to Brevo and aren't kept here."
        : "Visitors confirm by email first. Only confirmed addresses appear in your list.",
    },
  ];
  if (brevo) {
    fields.push(
      {
        key: "brevo_list_id",
        label: "Brevo list ID",
        kind: "text",
        required: true,
        maxLength: 12,
        inputMode: "numeric",
        hint: "In Brevo, open Contacts → Lists. The ID is the number in the list's row.",
      },
      {
        key: "brevo_api_key",
        label: "Brevo API key",
        kind: "password",
        required: !keySet,
        maxLength: 200,
        hint: keySet
          ? "A key is saved. Leave this empty to keep it, or paste a new one."
          : "Brevo → SMTP & API → API keys. It's stored encrypted and never shown again.",
      },
    );
  }
  return fields;
}

export const LATEST_FIELDS: FieldSpec[] = [
  {
    key: "source",
    label: "YouTube channel or feed",
    kind: "text",
    required: true,
    maxLength: 500,
    placeholder: "@yourchannel or https://example.com/feed.xml",
    hint: "A YouTube @handle or channel address, or any RSS or Atom feed. The page shows the newest item and checks for new ones every hour.",
  },
  { key: "label", label: "Small heading", kind: "text", maxLength: 60, placeholder: "Latest video" },
];

export const EVENTS_FIELDS: FieldSpec[] = [{ key: "events", label: "Dates", kind: "events", required: true }];

export function emptyEvent(): EventItem {
  return { date: "", title: "" };
}

export function asEvents(v: unknown): EventItem[] {
  return Array.isArray(v) ? (v as EventItem[]) : [];
}

/** Trim each event and drop empty optional fields. */
export function cleanEvents(list: EventItem[]): EventItem[] {
  return list.map((e) => {
    const out: EventItem = { date: (e.date ?? "").trim(), title: (e.title ?? "").trim() };
    const time = (e.time ?? "").trim();
    const city = (e.city ?? "").trim();
    const ticket = (e.ticket_url ?? "").trim();
    if (time) out.time = time;
    if (city) out.city = city;
    if (ticket) out.ticket_url = ticket;
    if (e.sold_out) out.sold_out = true;
    return out;
  });
}

/** The first problem in an events list, worded like the server's errors. */
export function eventsProblem(list: EventItem[]): string | null {
  if (list.length === 0) return "Add at least one date.";
  if (list.length > MAX_EVENTS) return `A block can list up to ${MAX_EVENTS} dates.`;
  for (const [i, e] of list.entries()) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test((e.date ?? "").trim())) return `Event ${i + 1}: choose a date.`;
    if (!(e.title ?? "").trim()) return `Event ${i + 1}: enter a title or venue.`;
  }
  return null;
}

/**
 * Events dated today or later in the viewer's zone, sorted. The server keeps
 * an event a little longer (until its date has ended everywhere).
 */
export function upcomingEvents(list: EventItem[], now = new Date()): EventItem[] {
  const today = localDate(now);
  return list
    .filter((e) => e.date >= today)
    .sort((a, b) => (a.date + (a.time || "99:99")).localeCompare(b.date + (b.time || "99:99")));
}

function localDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function newBlockSummary(type: string, data: BlockData, now = new Date()): string | null {
  const s = (k: string) => (typeof data[k] === "string" ? (data[k] as string) : "");
  switch (type) {
    case "email_signup":
      if (data.destination === "brevo") return `Brevo list ${String(data.brevo_list_id ?? "")}`.trim();
      return "List kept here";
    case "events": {
      const all = asEvents(data.events);
      const next = upcomingEvents(all, now)[0];
      const count = `${all.length} ${all.length === 1 ? "date" : "dates"}`;
      if (!next) return `${count} · all past`;
      const when = new Date(`${next.date}T00:00:00`).toLocaleDateString(undefined, { month: "short", day: "numeric" });
      return `${count} · next ${when}`;
    }
    case "latest":
      return s("source").replace(/^https:\/\/www\.youtube\.com\//, "");
  }
  return null;
}
