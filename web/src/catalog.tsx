import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api, errorMessage, type Catalog } from "./api";

type CatalogState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; catalog: Catalog };

interface CatalogContextValue {
  state: CatalogState;
  reload: () => void;
}

const CatalogContext = createContext<CatalogContextValue | null>(null);

export function CatalogProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<CatalogState>({ status: "loading" });

  const reload = useCallback(() => {
    setState({ status: "loading" });
    api
      .catalog()
      .then((catalog) => setState({ status: "ready", catalog }))
      .catch((err: unknown) => setState({ status: "error", message: errorMessage(err) }));
  }, []);

  useEffect(reload, [reload]);

  return <CatalogContext.Provider value={{ state, reload }}>{children}</CatalogContext.Provider>;
}

export function useCatalogState(): CatalogContextValue {
  const ctx = useContext(CatalogContext);
  if (!ctx) throw new Error("useCatalogState must be used inside CatalogProvider");
  return ctx;
}

/** Use only below a component that has checked the catalog is ready. */
export function useCatalog(): Catalog {
  const { state } = useCatalogState();
  if (state.status !== "ready") throw new Error("Catalog not loaded");
  return state.catalog;
}
