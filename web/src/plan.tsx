import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api, type Plan, type PlanFeature } from "./api";
import { useAuth } from "./auth";

interface PlanContextValue {
  /** Null while loading, when signed out, or if the request failed. */
  plan: Plan | null;
  refresh: () => Promise<void>;
}

const PlanContext = createContext<PlanContextValue | null>(null);

/** Loads the signed-in user's plan and reloads it when the user changes. */
export function PlanProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const userId = user?.id ?? null;
  const [plan, setPlan] = useState<Plan | null>(null);

  const refresh = useCallback(async () => {
    if (!userId) {
      setPlan(null);
      return;
    }
    try {
      setPlan(await api.plan());
    } catch {
      // Gated controls fall back to the server's upgrade_required errors.
      setPlan(null);
    }
  }, [userId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return <PlanContext.Provider value={{ plan, refresh }}>{children}</PlanContext.Provider>;
}

export function usePlan(): PlanContextValue {
  const ctx = useContext(PlanContext);
  if (!ctx) throw new Error("usePlan must be used inside PlanProvider");
  return ctx;
}

/**
 * Whether the plan includes a feature. Unknown (still loading) counts as
 * allowed so controls don't flash disabled; the server enforces the gate.
 */
export function planAllows(plan: Plan | null, feature: PlanFeature): boolean {
  return plan === null || plan.features[feature] === true;
}
