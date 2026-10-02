import type { SlugStatus } from "../lib/useSlugCheck";
import { Icon } from "./Icon";
import { Spinner } from "./ui";

/** Inline indicator shown inside the slug input. */
export function SlugStatusBadge({ status }: { status: SlugStatus }) {
  switch (status.kind) {
    case "checking":
      return <Spinner label="Checking availability" />;
    case "available":
      return (
        <span className="slug-ok">
          <Icon name="check" size={16} />
          <span>Available</span>
        </span>
      );
    default:
      return null;
  }
}

/** Message for the field's error slot, if any. */
export function slugStatusError(status: SlugStatus): string | null {
  if (status.kind === "invalid" || status.kind === "taken" || status.kind === "error") return status.message;
  return null;
}
