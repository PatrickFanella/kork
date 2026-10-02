import { useId } from "react";
import type { EventItem } from "../../api";
import { Icon } from "../../components/Icon";
import { emptyEvent, MAX_EVENTS } from "../../lib/newblocks";

/** Editable list of dates for an events block. */
export function EventsField({
  label,
  value,
  error,
  onChange,
}: {
  label: string;
  value: EventItem[];
  error?: string;
  onChange: (next: EventItem[]) => void;
}) {
  const id = useId();
  const update = (i: number, patch: Partial<EventItem>) => onChange(value.map((e, j) => (j === i ? { ...e, ...patch } : e)));
  const remove = (i: number) => onChange(value.filter((_, j) => j !== i));

  return (
    <fieldset className={`field events-field${error ? " has-error" : ""}`} aria-describedby={error ? `${id}-err` : `${id}-hint`}>
      <legend className="field-label">{label}</legend>
      <p className="field-hint" id={`${id}-hint`}>
        Past dates hide from your page automatically. Dates show in the order they happen.
      </p>
      <ol className="events-list">
        {value.map((e, i) => (
          <li key={i} className="event-row">
            <div className="event-row-grid">
              <label>
                <span>Date</span>
                <input type="date" value={e.date ?? ""} required onChange={(ev) => update(i, { date: ev.target.value })} />
              </label>
              <label>
                <span>
                  Time <span className="optional">optional</span>
                </span>
                <input type="time" value={e.time ?? ""} onChange={(ev) => update(i, { time: ev.target.value })} />
              </label>
              <label className="span-2">
                <span>Title or venue</span>
                <input type="text" value={e.title ?? ""} maxLength={100} required onChange={(ev) => update(i, { title: ev.target.value })} />
              </label>
              <label>
                <span>
                  City <span className="optional">optional</span>
                </span>
                <input type="text" value={e.city ?? ""} maxLength={80} onChange={(ev) => update(i, { city: ev.target.value })} />
              </label>
              <label>
                <span>
                  Ticket link <span className="optional">optional</span>
                </span>
                <input
                  type="url"
                  inputMode="url"
                  spellCheck={false}
                  placeholder="https://"
                  value={e.ticket_url ?? ""}
                  onChange={(ev) => update(i, { ticket_url: ev.target.value })}
                />
              </label>
            </div>
            <div className="event-row-actions">
              <label className="checkbox">
                <input type="checkbox" checked={e.sold_out === true} onChange={(ev) => update(i, { sold_out: ev.target.checked })} />
                <span>Sold out</span>
              </label>
              {value.length > 1 && (
                <button type="button" className="btn btn-sm btn-ghost btn-danger-text" onClick={() => remove(i)}>
                  <Icon name="trash" />
                  <span>Remove date {i + 1}</span>
                </button>
              )}
            </div>
          </li>
        ))}
      </ol>
      <button
        type="button"
        className="btn btn-sm"
        disabled={value.length >= MAX_EVENTS}
        onClick={() => onChange([...value, emptyEvent()])}
      >
        <Icon name="plus" />
        <span>Add date</span>
      </button>
      {value.length >= MAX_EVENTS && <p className="field-hint">A block can list up to {MAX_EVENTS} dates.</p>}
      {error && (
        <p className="field-error" id={`${id}-err`}>
          {error}
        </p>
      )}
    </fieldset>
  );
}
