import type { ReactNode } from "react";
import { Link } from "react-router-dom";

/** Where "Upgrade" links point: the plan section in Settings, which holds the billing controls. */
export const UPGRADE_PATH = "/settings#plan";

export function ProBadge() {
  return (
    <span className="badge badge-pro" title="Part of Pro">
      Pro
    </span>
  );
}

/** Explains why a control is disabled and links to the upgrade options. */
export function UpgradeNote({ children }: { children: ReactNode }) {
  return (
    <p className="upgrade-note small">
      <ProBadge /> <span>{children}</span> <Link to={UPGRADE_PATH}>Upgrade</Link>
    </p>
  );
}
