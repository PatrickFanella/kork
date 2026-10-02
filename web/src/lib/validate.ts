// Pure validation and formatting helpers shared by the editor screens.

export const SLUG_MIN = 3;
export const SLUG_MAX = 32;
export const PASSWORD_MIN = 10;
export const MAX_PAGES = 5;
export const MAX_BLOCKS = 100;
export const MAX_UPLOAD_BYTES = 5 * 1024 * 1024;
export const UPLOAD_TYPES = ["image/png", "image/jpeg", "image/webp", "image/gif"];

const SLUG_RE = /^[a-z0-9][a-z0-9_-]*$/;

/** Lowercase and trim what the user typed; spaces become hyphens. */
export function normalizeSlug(input: string): string {
  return input.trim().toLowerCase().replace(/\s+/g, "-");
}

/**
 * Client-side slug format check. Returns a human message, or null when the slug
 * is well formed. Reserved words are only known to the server.
 */
export function slugProblem(slug: string): string | null {
  if (slug.length === 0) return "Choose an address for your page.";
  if (slug.length < SLUG_MIN) return `Use at least ${SLUG_MIN} characters.`;
  if (slug.length > SLUG_MAX) return `Use at most ${SLUG_MAX} characters.`;
  if (!/^[a-z0-9]/.test(slug)) return "Start with a letter or number.";
  if (!SLUG_RE.test(slug)) return "Use only lowercase letters, numbers, hyphens and underscores.";
  return null;
}

export function passwordProblem(password: string): string | null {
  if (password.length < PASSWORD_MIN) return `Use at least ${PASSWORD_MIN} characters.`;
  return null;
}

const HOST_LABEL = /^(?!-)[a-z0-9-]{1,63}(?<!-)$/;

/** Accepts "links.example.com"; strips a pasted scheme, path, and trailing dot. */
export function normalizeHostname(input: string): string {
  let h = input.trim().toLowerCase();
  h = h.replace(/^[a-z]+:\/\//, "");
  h = h.split(/[/?#]/)[0] ?? "";
  h = h.replace(/\.$/, "");
  return h;
}

export function hostnameProblem(hostname: string): string | null {
  if (!hostname) return "Enter a hostname, like links.example.com.";
  if (hostname.length > 253) return "That hostname is too long.";
  const labels = hostname.split(".");
  if (labels.length < 2) return "Enter a full hostname, like links.example.com.";
  if (!labels.every((l) => HOST_LABEL.test(l))) return "That doesn't look like a valid hostname.";
  return null;
}

export function uploadProblem(file: { size: number; type: string }): string | null {
  if (!UPLOAD_TYPES.includes(file.type)) return "Use a PNG, JPEG, WebP or GIF image.";
  if (file.size > MAX_UPLOAD_BYTES) return "Images must be 5 MB or smaller.";
  return null;
}

const HEX_RE = /^#[0-9a-f]{6}$/i;

export function isHexColor(value: string): boolean {
  return HEX_RE.test(value);
}

/** Expand #abc to #aabbcc and lowercase; returns null if not a hex color. */
export function normalizeHex(value: string): string | null {
  const v = value.trim();
  const short = /^#?([0-9a-f])([0-9a-f])([0-9a-f])$/i.exec(v);
  if (short) return `#${short[1]}${short[1]}${short[2]}${short[2]}${short[3]}${short[3]}`.toLowerCase();
  const long = /^#?([0-9a-f]{6})$/i.exec(v);
  if (long) return `#${long[1]}`.toLowerCase();
  return null;
}

/** Strip a service prefix (e.g. "@") the user typed so the stored value is bare. */
export function stripPrefix(value: string, prefix: string): string {
  const v = value.trim();
  if (prefix && v.startsWith(prefix)) return v.slice(prefix.length);
  return v;
}

/** Case-insensitive match on any of the given fields; empty query matches all. */
export function matchesQuery(query: string, ...fields: string[]): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return fields.some((f) => f.toLowerCase().includes(q));
}

/** Move an item one step; returns a new array or the same array if out of range. */
export function moveItem<T>(items: readonly T[], index: number, delta: -1 | 1): T[] {
  const target = index + delta;
  if (index < 0 || index >= items.length || target < 0 || target >= items.length) {
    return items.slice();
  }
  const next = items.slice();
  const [item] = next.splice(index, 1);
  next.splice(target, 0, item as T);
  return next;
}

/**
 * Fill gaps in daily view counts so the chart shows every day in the range,
 * ending at `end` (YYYY-MM-DD, UTC). Days are returned oldest first.
 */
export function fillDays(
  views: readonly { day: string; count: number }[],
  days: number,
  end: string,
): { day: string; count: number }[] {
  const counts = new Map(views.map((v) => [v.day, v.count]));
  const endMs = Date.parse(`${end}T00:00:00Z`);
  if (Number.isNaN(endMs)) return views.slice();
  const out: { day: string; count: number }[] = [];
  for (let i = days - 1; i >= 0; i--) {
    const day = new Date(endMs - i * 86_400_000).toISOString().slice(0, 10);
    out.push({ day, count: counts.get(day) ?? 0 });
  }
  return out;
}

function channel(c: number): number {
  const s = c / 255;
  return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

function luminance(hex: string): number | null {
  const n = normalizeHex(hex);
  if (!n) return null;
  const r = parseInt(n.slice(1, 3), 16);
  const g = parseInt(n.slice(3, 5), 16);
  const b = parseInt(n.slice(5, 7), 16);
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/** WCAG contrast ratio between two hex colors (1–21), or null if either is invalid. */
export function contrastRatio(a: string, b: string): number | null {
  const la = luminance(a);
  const lb = luminance(b);
  if (la === null || lb === null) return null;
  const [hi, lo] = la > lb ? [la, lb] : [lb, la];
  return (hi + 0.05) / (lo + 0.05);
}
