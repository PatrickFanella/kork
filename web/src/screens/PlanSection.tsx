import { useEffect, useRef } from "react";
import { useLocation } from "react-router-dom";
import type { PlanFeature, PlanLimits } from "../api";
import { ProBadge } from "../components/Pro";
import { formatDate } from "../components/ui";
import { usePlan } from "../plan";

const FEATURE_NAMES: Record<PlanFeature, string> = {
  custom_domains: "Custom domains",
  remove_branding: "Hide the “Made with Kork” footer link",
  custom_css: "Custom CSS",
  custom_fonts: "Font uploads",
  stats_export: "Download stats as CSV",
  scheduled_links: "Scheduled links",
};

/** Settings section showing the plan, its limits, and the upgrade placeholder. */
export function PlanSection() {
  const { plan, refresh } = usePlan();
  const location = useLocation();
  const ref = useRef<HTMLElement>(null);

  // Pick up a plan an admin just granted.
  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (location.hash === "#plan") ref.current?.scrollIntoView({ block: "start" });
  }, [location.hash, plan]);

  if (!plan) return null;
  const pro = plan.plan === "pro";
  const lapsed = !pro && plan.stored_plan === "pro";

  return (
    <section className="card section" id="plan" ref={ref} aria-labelledby="plan-heading">
      <div className="section-row">
        <div className="section-intro">
          <h2 id="plan-heading">
            Plan: {pro ? "Pro" : "Free"} {pro && <ProBadge />}
          </h2>
          <p className="muted">
            {pro && plan.expires_at && <>Pro until {formatDate(plan.expires_at)}. </>}
            {pro && !plan.expires_at && <>Pro doesn't expire. </>}
            {lapsed && plan.expires_at && <>Pro ended on {formatDate(plan.expires_at)}. Your data is kept. </>}
            {plan.domains_grace_until && <>Custom domains keep working until {formatDate(plan.domains_grace_until)}.</>}
          </p>
        </div>
        {!pro && (
          // Placeholder until billing exists (#15).
          <a className="btn btn-primary" href="#upgrade" aria-describedby="upgrade-help">
            Upgrade
          </a>
        )}
      </div>
      {!pro && (
        <p className="muted small" id="upgrade-help">
          Paid plans aren't available yet. Pro is granted by hand for now.
        </p>
      )}
      <PlanTable current={plan} pro={plan.pro} />
    </section>
  );
}

function PlanTable({ current, pro }: { current: PlanLimits; pro: PlanLimits }) {
  const yes = (v: boolean | undefined) => (v ? "Yes" : "No");
  const rows: [string, string, string][] = [
    ["Pages", String(current.pages), String(pro.pages)],
    ["Stats history", `${current.stats_days} days`, `${pro.stats_days} days`],
    ["Custom domains", String(current.custom_domains), String(pro.custom_domains)],
    ...(Object.keys(FEATURE_NAMES) as PlanFeature[])
      .filter((f) => f !== "custom_domains")
      .map((f): [string, string, string] => [FEATURE_NAMES[f], yes(current.features[f]), yes(pro.features[f])]),
  ];
  return (
    <div className="table-wrap">
      <table className="data-table">
        <thead>
          <tr>
            <th scope="col">Feature</th>
            <th scope="col">You have</th>
            <th scope="col">Pro</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(([name, mine, theirs]) => (
            <tr key={name}>
              <td>{name}</td>
              <td>{mine}</td>
              <td>{theirs}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
