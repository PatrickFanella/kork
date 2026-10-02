import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { useDebounced } from "../components/ui";
import { slugProblem } from "./validate";

export type SlugStatus =
  | { kind: "idle" }
  | { kind: "invalid"; message: string }
  | { kind: "checking" }
  | { kind: "available" }
  | { kind: "taken"; message: string }
  | { kind: "error"; message: string };

/**
 * Validates a slug locally, then asks the server (debounced) whether it is
 * free. `current` is treated as available without a request.
 */
export function useSlugCheck(slug: string, current?: string): SlugStatus {
  const debounced = useDebounced(slug, 350);
  const [remote, setRemote] = useState<{ slug: string; status: SlugStatus } | null>(null);

  const local = slug ? slugProblem(slug) : null;

  useEffect(() => {
    if (!debounced || slugProblem(debounced) || debounced === current) return;
    let cancelled = false;
    api
      .checkSlug(debounced)
      .then((r) => {
        if (cancelled) return;
        setRemote({
          slug: debounced,
          status: r.available ? { kind: "available" } : { kind: "taken", message: r.reason || "That address is taken." },
        });
      })
      .catch((err: unknown) => {
        if (!cancelled) setRemote({ slug: debounced, status: { kind: "error", message: errorMessage(err) } });
      });
    return () => {
      cancelled = true;
    };
  }, [debounced, current]);

  if (!slug) return { kind: "idle" };
  if (local) return { kind: "invalid", message: local };
  if (slug === current) return { kind: "idle" };
  if (remote && remote.slug === slug) return remote.status;
  return { kind: "checking" };
}
