import { describe, expect, it } from "vitest";
import type { CatalogService } from "../api";
import { blockFields, blockSummary, cleanBlockData, missingRequired } from "./blocks";

const instagram: CatalogService = {
  key: "instagram",
  name: "Instagram",
  category: "social",
  input: "handle",
  placeholder: "username",
  prefix: "@",
  copy_only: false,
};

describe("cleanBlockData", () => {
  it("strips the handle prefix and sets the service key", () => {
    expect(cleanBlockData("service", { value: " @alice ", label: "", show_qr: false }, instagram)).toEqual({
      service: "instagram",
      value: "alice",
    });
  });
  it("drops empty optional fields and keeps true booleans", () => {
    expect(cleanBlockData("link", { url: "https://x.y", title: "X", subtitle: "  ", show_qr: true })).toEqual({
      url: "https://x.y",
      title: "X",
      show_qr: true,
    });
  });
});

describe("missingRequired", () => {
  it("reports empty required fields", () => {
    const fields = blockFields("image");
    expect(Object.keys(missingRequired(fields, { alt: "" }))).toEqual(["upload_id", "alt"]);
  });
  it("service forms hide the QR toggle for copy-only services", () => {
    const keys = blockFields("service", { ...instagram, copy_only: true }).map((f) => f.key);
    expect(keys).not.toContain("show_qr");
  });
});

describe("blockSummary", () => {
  it("shows the prefixed handle for service blocks", () => {
    expect(blockSummary("service", { service: "instagram", value: "alice" }, instagram)).toBe("@alice");
  });
});
