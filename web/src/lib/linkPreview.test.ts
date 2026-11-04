import { describe, expect, it } from "vitest";
import { previewFills } from "./linkPreview";

const preview = { url: "https://example.com", title: "Example", description: "A site", thumbnail_upload_id: "u1" };

describe("previewFills", () => {
  it("fills every empty field", () => {
    expect(previewFills(preview, { title: "", subtitle: "  ", thumbnailId: "" })).toEqual([
      ["title", "Example"],
      ["subtitle", "A site"],
      ["thumbnail_upload_id", "u1"],
    ]);
  });

  it("keeps what the user already typed", () => {
    expect(previewFills(preview, { title: "Mine", subtitle: "", thumbnailId: "u0" })).toEqual([["subtitle", "A site"]]);
  });

  it("skips what the page doesn't share", () => {
    expect(previewFills({ url: "https://example.com", title: "", description: "" }, { title: "", subtitle: "", thumbnailId: "" })).toEqual([]);
  });
});
