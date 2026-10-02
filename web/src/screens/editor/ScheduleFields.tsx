import { useId } from "react";
import { FieldError } from "../../components/ui";
import { fromLocalInput, toLocalInput, type BlockSchedule } from "../../lib/schedule";

const zone = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone;
  } catch {
    return "";
  }
})();

/** Optional start and end pickers for a block, in the browser's time zone. */
export function ScheduleFields({
  value,
  errors,
  onChange,
}: {
  value: BlockSchedule;
  errors: Partial<Record<keyof BlockSchedule, string>>;
  onChange: (next: BlockSchedule) => void;
}) {
  const id = useId();
  const hintId = `${id}-hint`;
  const field = (key: keyof BlockSchedule, label: string) => {
    const inputId = `${id}-${key}`;
    const errId = `${inputId}-err`;
    const current = value[key];
    return (
      <div className={`field${errors[key] ? " has-error" : ""}`}>
        <label htmlFor={inputId}>
          {label}
          <span className="optional"> optional</span>
        </label>
        <div className="schedule-input">
          <input
            id={inputId}
            type="datetime-local"
            value={toLocalInput(current)}
            aria-invalid={errors[key] ? true : undefined}
            aria-describedby={[errors[key] ? errId : null, hintId].filter(Boolean).join(" ")}
            onChange={(e) => onChange({ ...value, [key]: fromLocalInput(e.target.value) })}
          />
          {current && (
            <button type="button" className="btn btn-sm btn-ghost" onClick={() => onChange({ ...value, [key]: null })}>
              Clear<span className="visually-hidden"> {label.toLowerCase()}</span>
            </button>
          )}
        </div>
        <FieldError id={errId} message={errors[key]} />
      </div>
    );
  };
  return (
    <fieldset className="schedule-fields">
      <legend>Schedule</legend>
      <p className="field-hint" id={hintId}>
        Leave both empty to always show this block. Times are in your time zone{zone ? ` (${zone})` : ""}.
      </p>
      <div className="schedule-grid">
        {field("starts_at", "Show from")}
        {field("ends_at", "Hide at")}
      </div>
    </fieldset>
  );
}
