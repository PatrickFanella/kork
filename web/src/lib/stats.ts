// Display helpers for the Stats tab.

/** Label for a referrer row from the stats API. */
export function referrerLabel(name: string): string {
  if (name === "direct") return "Direct or unknown";
  if (name === "other") return "Other sites";
  return name;
}

/** Country name for an ISO 3166 alpha-2 code, falling back to the code. */
export function countryLabel(code: string, locale?: string): string {
  if (code === "unknown" || !/^[A-Z]{2}$/.test(code)) return "Unknown";
  try {
    return new Intl.DisplayNames(locale ? [locale] : undefined, { type: "region" }).of(code) ?? code;
  } catch {
    return code;
  }
}

/** Format a ratio as a percentage: whole numbers, one decimal below 10%. */
export function formatPercent(ratio: number): string {
  const p = ratio * 100;
  if (p > 0 && p < 10) return `${p.toFixed(1)}%`;
  return `${Math.round(p)}%`;
}
