import { useEffect, useRef } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api, errorMessage } from "../api";
import { PageLoading, useDocumentTitle } from "../components/ui";
import { patreonNotice, type BillingNotice } from "../lib/billing";
import { usePlan } from "../plan";

/**
 * Patreon redirects here after the patron approves (or declines) access.
 * The code and state go to the API, then the result shows in Settings.
 */
export function PatreonCallbackScreen() {
  useDocumentTitle("Connecting Patreon");
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { refresh } = usePlan();
  const started = useRef(false);

  useEffect(() => {
    // An authorization code works once; don't send it twice.
    if (started.current) return;
    started.current = true;
    const done = (billingNotice: BillingNotice) =>
      navigate("/settings#plan", { replace: true, state: { billingNotice } });

    const code = params.get("code");
    const state = params.get("state");
    if (params.get("error") || !code || !state) {
      done({ tone: "info", text: "Patreon wasn't connected." });
      return;
    }
    api
      .patreonCallback(code, state)
      .then(async (b) => {
        await refresh();
        done(patreonNotice(b));
      })
      .catch((err: unknown) => done({ tone: "danger", text: errorMessage(err) }));
  }, [params, navigate, refresh]);

  return <PageLoading />;
}
