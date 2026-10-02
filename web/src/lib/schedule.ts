/** A block's visibility window as the API sends it: RFC 3339 UTC or null. */
export interface BlockSchedule {
  starts_at: string | null;
  ends_at: string | null;
}

const pad = (n: number) => String(n).padStart(2, "0");

/** Converts an RFC 3339 time to a datetime-local input value in the browser's zone. */
export function toLocalInput(iso: string | null | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Converts a datetime-local input value (local time) to RFC 3339 UTC; empty means no bound. */
export function fromLocalInput(value: string): string | null {
  if (!value) return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return null;
  return d.toISOString().replace(/\.\d{3}Z$/, "Z");
}

/** Returns an error for the end field when the window is empty, else null. */
export function scheduleError(s: BlockSchedule): string | null {
  if (s.starts_at && s.ends_at && Date.parse(s.ends_at) <= Date.parse(s.starts_at)) {
    return "End time must be after the start time.";
  }
  return null;
}

export type ScheduleState = "always" | "scheduled" | "live" | "expired";

/** Where now falls relative to the window. The start is inclusive, the end exclusive. */
export function scheduleState(s: BlockSchedule, now: number = Date.now()): ScheduleState {
  if (!s.starts_at && !s.ends_at) return "always";
  if (s.starts_at && now < Date.parse(s.starts_at)) return "scheduled";
  if (s.ends_at && now >= Date.parse(s.ends_at)) return "expired";
  return "live";
}

export function formatWhen(iso: string, locale?: string): string {
  return new Date(iso).toLocaleString(locale, { dateStyle: "medium", timeStyle: "short" });
}

/** Short text for the block list, or null for blocks without a schedule. */
export function scheduleBadge(s: BlockSchedule, now: number = Date.now(), locale?: string): string | null {
  switch (scheduleState(s, now)) {
    case "always":
      return null;
    case "scheduled":
      return `Shows from ${formatWhen(s.starts_at!, locale)}`;
    case "expired":
      return `Expired ${formatWhen(s.ends_at!, locale)}`;
    case "live":
      return s.ends_at ? `Until ${formatWhen(s.ends_at, locale)}` : null;
  }
}
