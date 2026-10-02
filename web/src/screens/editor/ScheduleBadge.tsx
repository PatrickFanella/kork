import type { Block } from "../../api";
import { scheduleBadge, scheduleState } from "../../lib/schedule";

/** Shows a block's schedule in the block list; renders nothing for unscheduled blocks. */
export function ScheduleBadge({ block }: { block: Block }) {
  const schedule = { starts_at: block.starts_at ?? null, ends_at: block.ends_at ?? null };
  const now = Date.now();
  const text = scheduleBadge(schedule, now);
  if (!text) return null;
  return <span className={`schedule-badge is-${scheduleState(schedule, now)}`}>{text}</span>;
}
