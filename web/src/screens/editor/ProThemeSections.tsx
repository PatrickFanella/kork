import { useEffect, useId, useRef, useState } from "react";
import { api, errorMessage, type FontUploads } from "../../api";
import { Icon } from "../../components/Icon";
import { ProBadge, UpgradeNote } from "../../components/Pro";
import { Notice, Spinner } from "../../components/ui";
import { cssByteLength, CUSTOM_CSS_MAX_BYTES, fontFileProblem } from "../../lib/proStyle";
import { planAllows, usePlan } from "../../plan";
import type { EditorProps } from "./types";

const FONT_SLOTS: { key: keyof FontUploads; label: string; hint: string }[] = [
  { key: "body", label: "Body text", hint: "Buttons, bio and text blocks." },
  { key: "display", label: "Headings", hint: "Your page title and heading blocks." },
];

/** Font upload controls; the chosen IDs save with the rest of the theme. */
export function FontsSection({
  value,
  onChange,
}: {
  value: FontUploads | undefined;
  onChange: (next: FontUploads | undefined) => void;
}) {
  const { plan } = usePlan();
  const allowed = planAllows(plan, "custom_fonts");
  const hasFonts = Boolean(value?.body || value?.display);

  function set(key: keyof FontUploads, id: string | undefined) {
    const next: FontUploads = { ...value, [key]: id };
    if (!next.body) delete next.body;
    if (!next.display) delete next.display;
    onChange(next.body || next.display ? next : undefined);
  }

  return (
    <section className="card section" aria-labelledby="fonts-heading">
      <h2 id="fonts-heading">Custom fonts {!allowed && <ProBadge />}</h2>
      <p className="muted small">
        Upload a WOFF2, WOFF, TTF or OTF file up to 1 MB. Only upload fonts whose license allows web use. If a file can't
        load, the Font chosen above is used.
      </p>
      <div className="stack">
        {FONT_SLOTS.map((slot) => (
          <FontSlot
            key={slot.key}
            label={slot.label}
            hint={slot.hint}
            uploadId={value?.[slot.key]}
            allowed={allowed}
            onChange={(id) => set(slot.key, id)}
          />
        ))}
      </div>
      {!allowed && (
        <UpgradeNote>
          {hasFonts ? "Your fonts are saved but the page uses the preset font until you have Pro." : "Font uploads are part of Pro."}
        </UpgradeNote>
      )}
    </section>
  );
}

function FontSlot({
  label,
  hint,
  uploadId,
  allowed,
  onChange,
}: {
  label: string;
  hint: string;
  uploadId: string | undefined;
  allowed: boolean;
  onChange: (id: string | undefined) => void;
}) {
  const id = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleFile(file: File) {
    const problem = fontFileProblem(file);
    if (problem) {
      setError(problem);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const up = await api.uploadFont(file);
      onChange(up.id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  return (
    <div className={`field font-slot${error ? " has-error" : ""}`}>
      <span className="field-label" id={`${id}-label`}>
        {label}
      </span>
      <div className="font-slot-row">
        <span className="muted small">{uploadId ? "Uploaded font" : "Preset font"}</span>
        <input
          ref={inputRef}
          type="file"
          accept=".woff2,.woff,.ttf,.otf,font/woff2,font/woff,font/ttf,font/otf"
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
          disabled={busy || !allowed}
          onClick={() => inputRef.current?.click()}
        >
          {busy ? <Spinner label="Uploading" /> : <Icon name="upload" />}
          <span>{uploadId ? "Replace" : "Upload font"}</span>
        </button>
        {uploadId && (
          <button
            type="button"
            className="btn btn-sm btn-ghost"
            disabled={busy}
            onClick={() => {
              setError(null);
              onChange(undefined);
            }}
          >
            Remove
          </button>
        )}
      </div>
      <p className="field-hint">{hint}</p>
      {error && <p className="field-error">{error}</p>}
    </div>
  );
}

/** Custom CSS editor. Saves on demand and shows what the server removed. */
export function CustomCSSSection({ page, setPage, saved }: EditorProps) {
  const id = useId();
  const { plan } = usePlan();
  const allowed = planAllows(plan, "custom_css");
  const stored = page.custom_css ?? "";
  const [text, setText] = useState(stored);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [savedOnce, setSavedOnce] = useState(false);

  useEffect(() => {
    setText(page.custom_css ?? "");
  }, [page.id, page.custom_css]);

  const bytes = cssByteLength(text);
  const tooLarge = bytes > CUSTOM_CSS_MAX_BYTES;
  const dirty = text !== stored;
  // Without Pro the stored CSS can only be removed.
  const editable = allowed;

  async function save(css: string) {
    setBusy(true);
    setError(null);
    try {
      const res = await api.setPageCSS(page.id, css);
      setText(res.css);
      setWarnings(res.warnings);
      setSavedOnce(true);
      setPage((p) => ({ ...p, custom_css: res.css }));
      saved();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="card section" aria-labelledby={`${id}-heading`}>
      <h2 id={`${id}-heading`}>Custom CSS {!allowed && <ProBadge />}</h2>
      <p className="muted small" id={`${id}-help`}>
        Your rules apply only to this page's content: each selector is placed under{" "}
        <code>body.kork-page main.page</code>, so <code>.btn</code> styles your buttons and <code>body</code> styles the
        page column. The footer with Share and Report can't be changed. <code>url()</code> can
        only use files you uploaded (<code>/u/…</code>). @import, remote addresses and nested rules are removed. Up to 20
        KB.
      </p>
      <div className="field">
        <label htmlFor={`${id}-css`}>CSS</label>
        <textarea
          id={`${id}-css`}
          className="code-textarea"
          rows={10}
          spellCheck={false}
          value={text}
          readOnly={!editable}
          disabled={!editable && !stored}
          aria-describedby={`${id}-help ${id}-size`}
          aria-invalid={tooLarge || undefined}
          placeholder={".btn {\n  letter-spacing: .02em;\n}"}
          onChange={(e) => setText(e.target.value)}
        />
        <p className={`field-hint${tooLarge ? " field-error" : ""}`} id={`${id}-size`}>
          {(bytes / 1024).toFixed(1)} of 20 KB
        </p>
      </div>
      <div className="row">
        {editable && (
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={busy || !dirty || tooLarge}
            onClick={() => void save(text)}
          >
            {busy && <Spinner label="Saving" />}
            <span>Save CSS</span>
          </button>
        )}
        {stored && (
          <button type="button" className="btn btn-sm btn-ghost" disabled={busy} onClick={() => void save("")}>
            Remove CSS
          </button>
        )}
        {savedOnce && !dirty && !busy && !error && (
          <span className="muted small">
            <Icon name="check" size={14} /> Saved
          </span>
        )}
      </div>
      {error && <Notice tone="danger">Couldn't save CSS: {error}</Notice>}
      {warnings.length > 0 && (
        <Notice tone="warning" title="Some CSS was removed">
          <ul className="css-warnings">
            {warnings.map((w, i) => (
              <li key={`${i}-${w}`}>{w}</li>
            ))}
          </ul>
        </Notice>
      )}
      {!allowed && (
        <UpgradeNote>
          {stored ? "Your CSS is saved but not shown on your page until you have Pro." : "Custom CSS is part of Pro."}
        </UpgradeNote>
      )}
    </section>
  );
}
