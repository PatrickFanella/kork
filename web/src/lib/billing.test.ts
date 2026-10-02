import { describe, expect, it } from "vitest";
import type { Billing } from "../api";
import { checkoutNotice, patreonNotice, priceLabel, subscriptionStatus } from "./billing";

const billing = (patreon: Partial<Billing["patreon"]>): Billing => ({
  stripe: { enabled: false },
  patreon: { enabled: true, connected: true, ...patreon },
});

describe("billing messages", () => {
  it("maps checkout return parameters", () => {
    expect(checkoutNotice("success")?.tone).toBe("success");
    expect(checkoutNotice("canceled")?.text).toContain("weren't charged");
    expect(checkoutNotice(null)).toBeNull();
    expect(checkoutNotice("other")).toBeNull();
  });

  it("explains the Patreon result", () => {
    expect(patreonNotice(billing({ entitled: true })).tone).toBe("success");
    expect(patreonNotice(billing({ entitled: false, patron_status: "declined_patron" })).text).toContain("declined");
    expect(patreonNotice(billing({ entitled: false })).text).toContain("no active membership");
  });

  it("labels prices from config", () => {
    expect(priceLabel("$5", "month")).toBe("$5/month");
    expect(priceLabel(" $48 ", "year")).toBe("$48/year");
    expect(priceLabel("", "month")).toBe("Monthly");
    expect(priceLabel(undefined, "year")).toBe("Yearly");
  });

  it("describes subscription status", () => {
    const end = "2026-11-02T00:00:00Z";
    const fmt = () => "Nov 2";
    expect(subscriptionStatus({ enabled: true, status: "active", current_period_end: end }, fmt)).toBe(
      "Subscription active, renews on Nov 2.",
    );
    expect(
      subscriptionStatus({ enabled: true, status: "active", cancel_at_period_end: true, current_period_end: end }, fmt),
    ).toBe("Subscription ends on Nov 2.");
    expect(subscriptionStatus({ enabled: true, status: "past_due", current_period_end: end }, fmt)).toBe(
      "Payment overdue. Pro continues until Nov 2.",
    );
    expect(subscriptionStatus({ enabled: true, status: "active" })).toBe("Subscription active.");
    expect(subscriptionStatus({ enabled: true })).toBeNull();
  });
});
