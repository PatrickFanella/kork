import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { api, errorMessage } from "../api";
import { uploadProblem } from "../lib/validate";
import { Icon } from "./Icon";

export function Spinner({ label = "Loading" }: { label?: string }) {
  return (
    <span className="spinner" role="status">
      <span className="visually-hidden">{label}</span>
    </span>
  );
}

export function PageLoading({ label = "Loading" }: { label?: string }) {
  return (
    <div className="page-loading">
      <Spinner label={label} />
    </div>
  );
}

type Tone = "info" | "success" | "warning" | "danger";

export function Notice({
  tone = "info",
  title,
  children,
  action,
}: {
  tone?: Tone;
  title?: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className={`notice notice-${tone}`} role={tone === "danger" ? "alert" : "status"}>
      <div className="notice-body">
        {title && <strong className="notice-title">{title}</strong>}
        {children && <div>{children}</div>}
      </div>
      {action && <div className="notice-action">{action}</div>}
    </div>
  );
}

export function FieldError({ id, message }: { id: string; message?: string | null }) {
  if (!message) return null;
  return (
    <p className="field-error" id={id}>
      {message}
    </p>
  );
}

interface TextFieldProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: "text" | "email" | "password" | "url" | "tel" | "search";
  name?: string;
  error?: string | null;
  hint?: ReactNode;
  required?: boolean;
  placeholder?: string;
  autoComplete?: string;
  autoFocus?: boolean;
  maxLength?: number;
  prefix?: string;
  multiline?: boolean;
  rows?: number;
  disabled?: boolean;
  inputMode?: "text" | "email" | "url" | "tel" | "search";
  spellCheck?: boolean;
  status?: ReactNode;
}

export function TextField(props: TextFieldProps) {
  const id = useId();
  const errId = `${id}-err`;
  const hintId = `${id}-hint`;
  const describedBy = [props.error ? errId : null, props.hint ? hintId : null].filter(Boolean).join(" ") || undefined;
  const common = {
    id,
    name: props.name,
    value: props.value,
    required: props.required,
    placeholder: props.placeholder,
    autoFocus: props.autoFocus,
    maxLength: props.maxLength,
    disabled: props.disabled,
    spellCheck: props.spellCheck,
    "aria-invalid": props.error ? true : undefined,
    "aria-describedby": describedBy,
  };
  return (
    <div className={`field${props.error ? " has-error" : ""}`}>
      <label htmlFor={id}>
        {props.label}
        {!props.required && props.label && <span className="optional"> optional</span>}
      </label>
      {props.multiline ? (
        <textarea {...common} rows={props.rows ?? 4} onChange={(e) => props.onChange(e.target.value)} />
      ) : (
        <div className={`input-wrap${props.prefix ? " has-prefix" : ""}`}>
          {props.prefix && (
            <span className="input-prefix" aria-hidden="true">
              {props.prefix}
            </span>
          )}
          <input
            {...common}
            type={props.type ?? "text"}
            inputMode={props.inputMode}
            autoComplete={props.autoComplete}
            onChange={(e) => props.onChange(e.target.value)}
          />
          {props.status && <span className="input-status">{props.status}</span>}
        </div>
      )}
      {props.hint && (
        <p className="field-hint" id={hintId}>
          {props.hint}
        </p>
      )}
      <FieldError id={errId} message={props.error} />
    </div>
  );
}

export function SelectField({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: { key: string; name: string }[];
}) {
  const id = useId();
  const known = options.some((o) => o.key === value);
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <select id={id} value={value} onChange={(e) => onChange(e.target.value)}>
        {!known && value && <option value={value}>{value}</option>}
        {options.map((o) => (
          <option key={o.key} value={o.key}>
            {o.name}
          </option>
        ))}
      </select>
    </div>
  );
}

/** A checkbox styled as a switch. */
export function Switch({
  checked,
  onChange,
  label,
  hideLabel,
  disabled,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
  hideLabel?: boolean;
  disabled?: boolean;
}) {
  return (
    <label className={`switch${disabled ? " is-disabled" : ""}`}>
      <input
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className="switch-track" aria-hidden="true">
        <span className="switch-thumb" />
      </span>
      <span className={hideLabel ? "visually-hidden" : "switch-label"}>{label}</span>
    </label>
  );
}

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Fallback for insecure contexts.
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  }
}

export function CopyButton({ text, label = "Copy", className = "btn btn-sm" }: { text: string; label?: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return (
    <button
      type="button"
      className={className}
      onClick={async () => {
        if (await copyText(text)) {
          setCopied(true);
          window.clearTimeout(timer.current);
          timer.current = window.setTimeout(() => setCopied(false), 1600);
        }
      }}
    >
      <Icon name={copied ? "check" : "copy"} />
      <span>{copied ? "Copied" : label}</span>
      <span className="visually-hidden" aria-live="polite">
        {copied ? "Copied to clipboard" : ""}
      </span>
    </button>
  );
}

/** A labelled read-only value with a copy button, used for DNS records and URLs. */
export function CopyRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="copy-row">
      <span className="copy-row-label">{label}</span>
      <code className="copy-row-value">{value}</code>
      <CopyButton text={value} label={`Copy`} />
    </div>
  );
}

export function uploadUrl(id: string): string {
  return `/u/${encodeURIComponent(id)}`;
}

/**
 * Image upload control. Calls onChange with the new upload id (and URL), or
 * null when removed. Upload errors are shown inline.
 */
export function ImageField({
  label,
  imageUrl,
  onChange,
  error,
  required,
  shape = "square",
  removeLabel = "Remove",
}: {
  label: string;
  imageUrl: string | null;
  onChange: (upload: { id: string; url: string } | null) => void | Promise<void>;
  error?: string | null;
  required?: boolean;
  shape?: "square" | "circle" | "wide";
  removeLabel?: string;
}) {
  const id = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);
  const shown = localError ?? error;

  async function handleFile(file: File) {
    const problem = uploadProblem(file);
    if (problem) {
      setLocalError(problem);
      return;
    }
    setBusy(true);
    setLocalError(null);
    try {
      const up = await api.upload(file);
      await onChange({ id: up.id, url: up.url || uploadUrl(up.id) });
    } catch (err) {
      setLocalError(errorMessage(err));
    } finally {
      setBusy(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  return (
    <div className={`field image-field${shown ? " has-error" : ""}`}>
      <span className="field-label" id={`${id}-label`}>
        {label}
        {!required && <span className="optional"> optional</span>}
      </span>
      <div className="image-field-row">
        <div className={`image-thumb image-thumb-${shape}`}>
          {imageUrl ? <img src={imageUrl} alt="" /> : <Icon name="image" />}
        </div>
        <div className="image-field-actions">
          <input
            ref={inputRef}
            type="file"
            accept="image/png,image/jpeg,image/webp,image/gif"
            className="visually-hidden"
            tabIndex={-1}
            aria-hidden="true"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) void handleFile(f);
            }}
          />
          <button
            type="button"
            className="btn btn-sm"
            aria-describedby={`${id}-label`}
            disabled={busy}
            onClick={() => inputRef.current?.click()}
          >
            {busy ? <Spinner label="Uploading" /> : <Icon name="upload" />}
            <span>{imageUrl ? "Replace" : "Upload image"}</span>
          </button>
          {imageUrl && (
            <button
              type="button"
              className="btn btn-sm btn-ghost"
              disabled={busy}
              onClick={() => {
                setLocalError(null);
                void onChange(null);
              }}
            >
              {removeLabel}
            </button>
          )}
          <span className="field-hint">PNG, JPEG, WebP or GIF, up to 5 MB.</span>
        </div>
      </div>
      <FieldError id={`${id}-err`} message={shown} />
    </div>
  );
}

/** Two-step delete: first click asks, second confirms. */
export function ConfirmButton({
  onConfirm,
  label,
  confirmLabel = "Yes, delete",
  busyLabel = "Deleting",
  className = "btn btn-sm btn-ghost btn-danger-text",
  icon = "trash",
  hideLabel,
}: {
  onConfirm: () => Promise<void> | void;
  label: string;
  confirmLabel?: string;
  busyLabel?: string;
  className?: string;
  icon?: "trash" | "ban";
  hideLabel?: boolean;
}) {
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const confirmRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (asking) confirmRef.current?.focus();
  }, [asking]);

  if (!asking) {
    return (
      <button type="button" className={className} onClick={() => setAsking(true)} title={hideLabel ? label : undefined}>
        <Icon name={icon} />
        <span className={hideLabel ? "visually-hidden" : undefined}>{label}</span>
      </button>
    );
  }
  return (
    <span className="confirm-inline" onKeyDown={(e) => e.key === "Escape" && setAsking(false)}>
      <button
        ref={confirmRef}
        type="button"
        className="btn btn-sm btn-danger"
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          try {
            await onConfirm();
          } finally {
            setBusy(false);
            setAsking(false);
          }
        }}
      >
        {busy ? busyLabel : confirmLabel}
      </button>
      <button type="button" className="btn btn-sm btn-ghost" onClick={() => setAsking(false)} disabled={busy}>
        Cancel
      </button>
    </span>
  );
}

export function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value), ms);
    return () => window.clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

export function useDocumentTitle(title: string) {
  useEffect(() => {
    document.title = title ? `${title} · Kork` : "Kork";
  }, [title]);
}

export function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

export function formatRelative(iso: string): string {
  const d = new Date(iso).getTime();
  if (Number.isNaN(d)) return "";
  const diff = Date.now() - d;
  const min = Math.round(diff / 60000);
  if (min < 1) return "just now";
  if (min < 60) return `${min} min ago`;
  const h = Math.round(min / 60);
  if (h < 24) return `${h} h ago`;
  const days = Math.round(h / 24);
  if (days < 7) return `${days} d ago`;
  return formatDate(iso);
}
