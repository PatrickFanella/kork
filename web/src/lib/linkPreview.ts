import type { LinkPreview } from "../api";

export type LinkFill = "title" | "subtitle" | "thumbnail_upload_id";

/**
 * Which link fields a fetched preview should fill in: the ones the user has
 * left empty. Fields that already hold text keep it; the preview is offered
 * as a suggestion for those instead.
 */
export function previewFills(p: LinkPreview, current: { title: string; subtitle: string; thumbnailId: string }): [LinkFill, string][] {
  const fills: [LinkFill, string][] = [];
  if (p.title && !current.title.trim()) fills.push(["title", p.title]);
  if (p.description && !current.subtitle.trim()) fills.push(["subtitle", p.description]);
  if (p.thumbnail_upload_id && !current.thumbnailId) fills.push(["thumbnail_upload_id", p.thumbnail_upload_id]);
  return fills;
}
