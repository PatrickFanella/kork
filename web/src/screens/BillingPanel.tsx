import { useCallback, useEffect, useState } from "react";
import { useLocation, useSearchParams } from "react-router-dom";
import { PATREON_CONNECT_URL, api, errorMessage, type Billing, type Plan } from "../api";
import { Notice, Spinner, formatDate } from "../components/ui";
import { checkoutNotice, priceLabel, subscriptionStatus, type BillingNotice } from "../lib/billing";

/** How often, and how many times, to reload the plan after Checkout while the webhook lands. */
const POLL_MS = 3000;
const POLL_TRIES = 10;

/**
 * Upgrade and billing controls in the Settings plan section: Stripe
 * subscription buttons or "Manage billing", and the Patreon connection.
 * Providers the server hasn't configured are not shown.
 */
export function BillingPanel({ plan, refreshPlan }: { plan: Plan; refreshPlan: () => Promise<void> }) {
  const [billing, setBilling] = useState<Billing | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [params] = useSearchParams();
  const location = useLocation();
  const routed = (location.state as { billingNotice?: BillingNotice } | null)?.billingNotice ?? null;
  const notice = routed ?? checkoutNotice(params.get("billing"));
  const pro = plan.plan === "pro";

  const load = useCallback(async () => {
    try {
      setBilling(await api.billing());
    } catch (err) {
      setError(errorMessage(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // After Checkout, Pro turns on when Stripe's webhook arrives; poll briefly.
  const returnedFromCheckout = params.get("billing") === "success";
  useEffect(() => {
    if (!returnedFromCheckout || pro) return;
    let tries = 0;
    const id = window.setInterval(() => {
      tries += 1;
      void refreshPlan();
      void load();
      if (tries >= POLL_TRIES) window.clearInterval(id);
    }, POLL_MS);
    return () => window.clearInterval(id);
  }, [returnedFromCheckout, pro, refreshPlan, load]);

  async function go(action: string, fn: () => Promise<{ url: string }>) {
    setBusy(action);
    setError(null);
    try {
      const { url } = await fn();
      window.location.assign(url);
    } catch (err) {
      setError(errorMessage(err));
      setBusy(null);
    }
  }

  async function disconnectPatreon() {
    if (!window.confirm("Disconnect Patreon? Pro from your membership ends now.")) return;
    setBusy("patreon");
    setError(null);
    try {
      setBilling(await api.patreonDisconnect());
      await refreshPlan();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  if (!billing) {
    return error ? <Notice tone="danger">{error}</Notice> : <Spinner label="Loading billing" />;
  }
  const { stripe, patreon } = billing;

  if (!stripe.enabled && !patreon.enabled) {
    return pro ? null : (
      <p className="muted small" id="upgrade">
        Paid plans aren't available yet. Pro is granted by hand for now.
      </p>
    );
  }

  const status = subscriptionStatus(stripe, formatDate);
  return (
    <div className="stack" id="upgrade">
      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
      {error && <Notice tone="danger">{error}</Notice>}

      {stripe.enabled && stripe.subscribed && (
        <div className="row">
          <span>{status}</span>
          <button type="button" className="btn btn-sm" disabled={busy !== null} onClick={() => void go("portal", api.billingPortal)}>
            {busy === "portal" ? <Spinner label="Opening billing" /> : "Manage billing"}
          </button>
        </div>
      )}
      {stripe.enabled && stripe.payment_failed && stripe.subscribed && (
        <Notice tone="warning">Your last payment failed. Update your card in Manage billing to keep Pro.</Notice>
      )}
      {stripe.enabled && !stripe.subscribed && (
        <div className="row">
          <span>{pro ? "Subscribe:" : "Upgrade:"}</span>
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy !== null}
            onClick={() => void go("month", () => api.checkout("month"))}
          >
            {busy === "month" ? <Spinner label="Opening checkout" /> : priceLabel(stripe.price_monthly, "month")}
          </button>
          <span className="muted">or</span>
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy !== null}
            onClick={() => void go("year", () => api.checkout("year"))}
          >
            {busy === "year" ? <Spinner label="Opening checkout" /> : priceLabel(stripe.price_annual, "year")}
          </button>
          {stripe.can_manage && (
            <button type="button" className="btn btn-ghost btn-sm" disabled={busy !== null} onClick={() => void go("portal", api.billingPortal)}>
              Billing history
            </button>
          )}
        </div>
      )}

      {patreon.enabled && <PatreonRow patreon={patreon} busy={busy !== null} onDisconnect={() => void disconnectPatreon()} />}
    </div>
  );
}

function PatreonRow({
  patreon,
  busy,
  onDisconnect,
}: {
  patreon: Billing["patreon"];
  busy: boolean;
  onDisconnect: () => void;
}) {
  if (!patreon.connected) {
    return (
      <div className="row">
        <span>SUBCULT Patreon Backers get Pro.</span>
        <a className="btn btn-sm" href={PATREON_CONNECT_URL}>
          Connect Patreon
        </a>
      </div>
    );
  }
  let text: string;
  if (patreon.reconnect) {
    text = "Patreon no longer accepts Kork's access. Reconnect to keep Pro from your membership.";
  } else if (patreon.grace_until) {
    text = `Patreon doesn't show an active membership. Pro stays until ${formatDate(patreon.grace_until)}.`;
  } else if (patreon.entitled) {
    text = "Patreon connected. Your membership unlocks Pro.";
  } else {
    text = "Patreon connected, but there's no active membership in a tier that unlocks Pro.";
  }
  return (
    <div className="row">
      <span>{text}</span>
      {(patreon.reconnect || !patreon.entitled) && (
        <a className="btn btn-sm" href={PATREON_CONNECT_URL}>
          {patreon.reconnect ? "Reconnect" : "Check again"}
        </a>
      )}
      <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={onDisconnect}>
        Disconnect Patreon
      </button>
    </div>
  );
}
