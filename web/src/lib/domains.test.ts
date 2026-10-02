import { describe, expect, it } from "vitest";
import type { Domain } from "../api";
import { domainBadge, domainRecords } from "./domains";

const base: Domain = {
  id: "d1",
  hostname: "links.alice.example",
  verified: false,
  txt_name: "_zelda.links.alice.example",
  txt_value: "zelda-verify=abc",
  target: "customers.subcult.tv",
  created_at: "2026-10-02T00:00:00Z",
};

describe("domainRecords", () => {
  it("builds TXT and CNAME records for servers without records", () => {
    expect(domainRecords(base)).toEqual([
      { type: "TXT", name: "_zelda.links.alice.example", value: "zelda-verify=abc", purpose: "ownership" },
      { type: "CNAME", name: "links.alice.example", value: "customers.subcult.tv", purpose: "routing" },
    ]);
  });

  it("uses the server's records when present", () => {
    const records = [{ type: "CNAME", name: "links.alice.example", value: "customers.subcult.tv", purpose: "routing" as const }];
    expect(domainRecords({ ...base, records })).toBe(records);
  });
});

describe("domainBadge", () => {
  it("labels each status", () => {
    expect(domainBadge(base, "dns").label).toBe("Waiting for DNS");
    expect(domainBadge({ ...base, status: "pending" }, "cloudflare").label).toBe("Pending");
    expect(domainBadge({ ...base, status: "error" }, "cloudflare").className).toBe("badge-danger");
    expect(domainBadge({ ...base, verified: true }, "dns").label).toBe("Verified");
  });
});
