// Client-side checks for Pro styling. The server repeats every check.

/** Largest custom CSS the server accepts, in bytes. */
export const CUSTOM_CSS_MAX_BYTES = 20 * 1024;

/** Largest font upload, in bytes. */
export const MAX_FONT_BYTES = 1024 * 1024;

const FONT_EXTENSIONS = [".woff2", ".woff", ".ttf", ".otf"];

/** UTF-8 size of the CSS, which is what the server limits. */
export function cssByteLength(css: string): number {
  return new TextEncoder().encode(css).length;
}

/**
 * A reason the file can't be a font upload, or null. Browsers often leave
 * File.type empty for fonts, so this goes by extension; the server checks
 * the file's bytes.
 */
export function fontFileProblem(file: { name: string; size: number }): string | null {
  const name = file.name.toLowerCase();
  if (!FONT_EXTENSIONS.some((ext) => name.endsWith(ext))) return "Use a WOFF2, WOFF, TTF or OTF font file.";
  if (file.size > MAX_FONT_BYTES) return "Fonts must be 1 MB or smaller.";
  return null;
}
