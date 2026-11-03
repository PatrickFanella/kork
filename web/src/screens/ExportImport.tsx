import { useState, type ChangeEvent, type FormEvent } from "react";
import { api, errorMessage, type ExportImportResult, type ExportPreviewPage } from "../api";
import { Notice, Spinner } from "../components/ui";
import { normalizeSlug } from "../lib/validate";

type Choice = ExportPreviewPage & { selected: boolean; newSlug: string };

/** Recreates pages from a Kork account export ZIP (Settings → Download my data). */
export function ExportImport({ onImported, onCancel }: { onImported: () => void; onCancel: () => void }) {
  const [file, setFile] = useState<File | null>(null);
  const [choices, setChoices] = useState<Choice[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<ExportImportResult | null>(null);

  async function onFile(e: ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0] ?? null;
    setFile(f);
    setChoices(null);
    setResult(null);
    setError(null);
    if (!f) return;
    setBusy(true);
    try {
      const { pages } = await api.exportPreview(f);
      setChoices(pages.map((p) => ({ ...p, selected: true, newSlug: p.slug_available ? p.slug : `${p.slug}-copy` })));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  function update(index: number, patch: Partial<Choice>) {
    setChoices((cur) => cur && cur.map((c) => (c.index === index ? { ...c, ...patch } : c)));
  }

  async function onImport(e: FormEvent) {
    e.preventDefault();
    if (!file || !choices) return;
    const picks = choices.filter((c) => c.selected).map((c) => ({ index: c.index, slug: normalizeSlug(c.newSlug) }));
    if (picks.length === 0) return;
    setBusy(true);
    setError(null);
    try {
      const res = await api.importExport(file, picks);
      setResult(res);
      if (res.pages.length > 0) onImported();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="card section" aria-labelledby="export-import-heading">
      <h2 id="export-import-heading">Import from a Kork export</h2>
      <p className="muted small">
        Choose a ZIP from Settings → Download my data, on this or another Kork server. Pages are recreated as drafts with
        their theme, blocks and images. Sign-up contacts, stats and custom domains aren't imported.
      </p>
      <input type="file" accept=".zip,application/zip" onChange={onFile} disabled={busy} aria-label="Export ZIP" />
      {busy && <Spinner label="Reading" />}
      {choices && choices.length === 0 && <Notice tone="warning">That export has no pages.</Notice>}
      {choices && choices.length > 0 && !result && (
        <form className="stack" onSubmit={onImport}>
          <ul className="stack">
            {choices.map((c) => (
              <li key={c.index} className="row">
                <label className="row">
                  <input type="checkbox" checked={c.selected} onChange={(e) => update(c.index, { selected: e.target.checked })} />
                  <span>
                    <strong>{c.title || c.slug}</strong> <span className="muted small">({c.blocks} blocks)</span>
                  </span>
                </label>
                <label className="row small">
                  <span>New address</span>
                  <input
                    value={c.newSlug}
                    onChange={(e) => update(c.index, { newSlug: e.target.value })}
                    maxLength={40}
                    spellCheck={false}
                    disabled={!c.selected}
                  />
                </label>
              </li>
            ))}
          </ul>
          <div className="row">
            <button type="submit" className="btn btn-primary" disabled={busy || !choices.some((c) => c.selected)}>
              Import pages
            </button>
            <button type="button" className="btn btn-ghost" onClick={onCancel}>
              Cancel
            </button>
          </div>
        </form>
      )}
      {result && (
        <Notice tone={result.skipped.length ? "warning" : "success"} title={`Imported ${result.pages.length} ${result.pages.length === 1 ? "page" : "pages"}`}>
          {result.skipped.length > 0 && (
            <ul>
              {result.skipped.map((s, i) => (
                <li key={i}>
                  {s.page}, {s.what}: {s.reason}
                </li>
              ))}
            </ul>
          )}
        </Notice>
      )}
      {error && <Notice tone="danger">{error}</Notice>}
      {(result || (!choices && !busy)) && (
        <button type="button" className="btn btn-ghost" onClick={onCancel}>
          Close
        </button>
      )}
    </section>
  );
}
