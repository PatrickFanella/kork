import type { Billing } from "../api";

export type NoticeTone = "info" | "success" | "warning" | "danger";

export interface BillingNotice {
  tone: NoticeTone;
  text: string;
}

/** Message for the ?billing= parameter Stripe Checkout returns with. */
export function checkoutNotice(param: string | null): BillingNotice | null {
  switch (param) {
    case "success":
      return {
        tone: "success",
        text: "Thanks for subscribing. Pro turns on as soon as Stripe confirms the payment, usually within a minute.",
      };
    case "canceled":
      return { tone: "info", text: "Checkout was canceled. You weren't charged." };
  }
  return null;
}

/** Message after connecting Patreon, from the callback response. */
export function patreonNotice(b: Billing): BillingNotice {
  if (b.patreon.entitled) {
    return { tone: "success", text: "Patreon connected. Your Backer membership unlocks Pro." };
  }
  return {
    tone: "warning",
    text:
      b.patreon.patron_status === "declined_patron"
        ? "Patreon connected, but your last payment was declined. Pro turns on once Patreon reports the membership as active."
        : "Patreon connected, but there's no active membership in a tier that unlocks Pro. Kork checks again daily.",
  };
}

/** Button label, e.g. "$5/month"; falls back to the interval name. */
export function priceLabel(price: string | undefined, interval: "month" | "year"): string {
  const p = price?.trim();
  if (!p) return interval === "month" ? "Monthly" : "Yearly";
  return `${p}/${interval}`;
}

/** One-line status for a Stripe subscription, with its date. */
export function subscriptionStatus(s: Billing["stripe"], fmt: (iso: string) => string = (d) => d): string | null {
  const end = s.current_period_end ? fmt(s.current_period_end) : null;
  switch (s.status) {
    case "active":
    case "trialing":
      if (s.cancel_at_period_end) return end ? `Subscription ends on ${end}.` : "Subscription ends at the end of this period.";
      return end ? `Subscription active, renews on ${end}.` : "Subscription active.";
    case "past_due":
      return end ? `Payment overdue. Pro continues until ${end}.` : "Payment overdue.";
    case "unpaid":
      return "Subscription unpaid.";
    case "paused":
      return "Subscription paused.";
    case "canceled":
      return "Subscription canceled.";
  }
  return null;
}
