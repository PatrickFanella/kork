import { createContext, useCallback, useContext, useEffect, useState, type ReactNode, useRef } from "react";
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

  // A plan request for a previous user (or an older refresh) is ignored.
  const seq = useRef(0);
  const refresh = useCallback(async () => {
    const mine = ++seq.current;
    if (!userId) {
      setPlan(null);
      return;
    }
    try {
      const p = await api.plan();
      if (mine === seq.current) setPlan(p);
    } catch {
      // Gated controls fall back to the server's upgrade_required errors.
      if (mine === seq.current) setPlan(null);
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
