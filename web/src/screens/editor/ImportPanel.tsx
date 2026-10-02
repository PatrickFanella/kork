import { useMemo, useState, type FormEvent } from "react";
import { api, errorMessage, type Block, type ImportPreview, type PagePatch } from "../../api";
import { useCatalog } from "../../catalog";
import { BLOCK_ICONS, Icon } from "../../components/Icon";
import { Notice, Spinner, TextField } from "../../components/ui";
import { blockSummary } from "../../lib/blocks";
import { MAX_BLOCKS } from "../../lib/validate";
import { ServiceMonogram } from "./ServicePicker";
import type { EditorProps } from "./types";

interface ImportPanelProps extends EditorProps {
  onClose: () => void;
  /** Announce a result to screen readers. */
  announce: (message: string) => void;
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return url;
  }
}

/**
 * Paste a Linktree (or any link page) address, review what the server found,
 * and import the chosen items as blocks on this page.
 */
export function ImportPanel({ page, setPage, saved, onClose, announce }: ImportPanelProps) {
  const catalog = useCatalog();
  const services = useMemo(() => new Map(catalog.services.map((s) => [s.key, s])), [catalog.services]);
  const [url, setUrl] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [chosen, setChosen] = useState<Set<number>>(new Set());
  const [useTitle, setUseTitle] = useState(false);
  const [useBio, setUseBio] = useState(false);
  const [useAvatar, setUseAvatar] = useState(false);
  const [importing, setImporting] = useState(false);
  const [result, setResult] = useState<{ added: number; problems: string[] } | null>(null);

  const room = MAX_BLOCKS - page.blocks.length;

  async function fetchPreview(e: FormEvent) {
    e.preventDefault();
    if (!url.trim()) {
      setError("Paste the address of your link page.");
      return;
    }
    setLoading(true);
    setError(null);
    setResult(null);
    try {
      const p = await api.importPreview(url.trim());
      setPreview(p);
      setChosen(new Set(p.items.map((_, i) => i).slice(0, Math.max(room, 0))));
      // Only offer to replace profile fields the user hasn't filled in yet.
      setUseTitle(Boolean(p.title) && (page.title === page.slug || !page.title));
      setUseBio(Boolean(p.bio) && !page.bio);
      setUseAvatar(Boolean(p.avatar_url) && !page.avatar_upload_id);
    } catch (err) {
      setPreview(null);
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }

  function toggle(i: number, on: boolean) {
    setChosen((prev) => {
      const next = new Set(prev);
      if (on) next.add(i);
      else next.delete(i);
      return next;
    });
  }

  async function runImport() {
    if (!preview) return;
    setImporting(true);
    setError(null);
    const problems: string[] = [];
    const patch: PagePatch = {};
    if (useAvatar && preview.avatar_url) {
      try {
        patch.avatar_upload_id = (await api.importImage(preview.avatar_url)).id;
      } catch (err) {
        problems.push(`Profile picture: ${errorMessage(err)}`);
      }
    }
    if (useTitle && preview.title) patch.title = preview.title;
    if (useBio && preview.bio) patch.bio = preview.bio;
    if (Object.keys(patch).length) {
      try {
        const updated = await api.updatePage(page.id, patch);
        setPage((p) => ({ ...updated, blocks: p.blocks }));
      } catch (err) {
        problems.push(`Profile: ${errorMessage(err)}`);
      }
    }
    const items = preview.items.filter((_, i) => chosen.has(i)).slice(0, Math.max(room, 0));
    const added: Block[] = [];
    for (const item of items) {
      try {
        const block = await api.createBlock(page.id, item.type, item.data, true);
        added.push(block);
        setPage((p) => ({ ...p, blocks: [...p.blocks, block] }));
      } catch (err) {
        problems.push(`${item.label}: ${errorMessage(err)}`);
      }
    }
    setImporting(false);
    saved();
    setResult({ added: added.length, problems });
    announce(`Imported ${added.length} ${added.length === 1 ? "block" : "blocks"}.`);
    if (!problems.length) {
      setPreview(null);
      setUrl("");
    }
  }

  const selectedCount = chosen.size;
  const anything = selectedCount > 0 || useTitle || useBio || useAvatar;

  return (
    <section className="card add-panel import-panel" aria-labelledby="import-panel-title">
      <div className="add-panel-header">
        <h2 id="import-panel-title">Import from another link page</h2>
        <button type="button" className="icon-btn" onClick={onClose} title="Close">
          <Icon name="close" size={16} />
          <span className="visually-hidden">Close</span>
        </button>
      </div>

      <form className="import-form" onSubmit={fetchPreview} noValidate>
        <TextField
          label="Link page address"
          value={url}
          onChange={setUrl}
          type="url"
          inputMode="url"
          placeholder="https://linktr.ee/yourname"
          hint="Linktree pages work best. For any other public page we collect its outbound links. Nothing changes until you press Import."
          spellCheck={false}
          required
          autoFocus
          disabled={loading || importing}
        />
        <div className="form-actions">
          <button type="submit" className="btn" disabled={loading || importing}>
            {loading ? <Spinner label="Reading page" /> : <Icon name="search" />}
            <span>{loading ? "Reading page…" : "Find links"}</span>
          </button>
        </div>
      </form>

      {error && <Notice tone="danger">{error}</Notice>}
      {result && (
        <Notice tone={result.problems.length ? "warning" : "success"} title={`Imported ${result.added} ${result.added === 1 ? "block" : "blocks"}`}>
          {result.problems.length > 0 && (
            <ul className="import-problems">
              {result.problems.map((p) => (
                <li key={p}>{p}</li>
              ))}
            </ul>
          )}
        </Notice>
      )}

      {preview && (
        <div className="import-preview">
          <p className="muted small">
            Found on {hostOf(preview.source_url)}. Untick anything you don't want.
          </p>

          {(preview.title || preview.bio || preview.avatar_url) && (
            <fieldset className="import-group">
              <legend>Profile</legend>
              {preview.title && (
                <label className="checkbox">
                  <input type="checkbox" checked={useTitle} onChange={(e) => setUseTitle(e.target.checked)} />
                  <span>
                    Title: <strong>{preview.title}</strong>
                  </span>
                </label>
              )}
              {preview.bio && (
                <label className="checkbox">
                  <input type="checkbox" checked={useBio} onChange={(e) => setUseBio(e.target.checked)} />
                  <span>Bio: {preview.bio}</span>
                </label>
              )}
              {preview.avatar_url && (
                <label className="checkbox">
                  <input type="checkbox" checked={useAvatar} onChange={(e) => setUseAvatar(e.target.checked)} />
                  <span>Profile picture (copied from {hostOf(preview.avatar_url)})</span>
                </label>
              )}
            </fieldset>
          )}

          {preview.items.length === 0 ? (
            <p className="muted">No links found on that page.</p>
          ) : (
            <fieldset className="import-group">
              <legend>
                Blocks ({selectedCount} of {preview.items.length} selected)
              </legend>
              <div className="import-bulk">
                <button
                  type="button"
                  className="btn btn-sm btn-ghost"
                  onClick={() => setChosen(new Set(preview.items.map((_, i) => i).slice(0, Math.max(room, 0))))}
                >
                  Select all
                </button>
                <button type="button" className="btn btn-sm btn-ghost" onClick={() => setChosen(new Set())}>
                  Select none
                </button>
              </div>
              {selectedCount > room && (
                <Notice tone="warning">
                  This page has room for {Math.max(room, 0)} more {room === 1 ? "block" : "blocks"}; only the first{" "}
                  {Math.max(room, 0)} selected will be imported.
                </Notice>
              )}
              <ul className="import-items">
                {preview.items.map((item, i) => {
                  const service = item.type === "service" ? services.get(String(item.data.service ?? "")) : undefined;
                  const summary = blockSummary(item.type, item.data, service);
                  return (
                    <li key={i}>
                      <label className="import-item">
                        <input type="checkbox" checked={chosen.has(i)} onChange={(e) => toggle(i, e.target.checked)} />
                        <span className="block-icon">
                          {service ? <ServiceMonogram service={service} size={28} /> : <Icon name={BLOCK_ICONS[item.type] ?? "blocks"} />}
                        </span>
                        <span className="block-text">
                          <span className="block-label">{item.label}</span>
                          <span className="block-meta">
                            <span className="block-type">{service ? service.name : item.type === "heading" ? "Heading" : "Link"}</span>
                            {summary && summary !== item.label && <span className="block-summary">{summary}</span>}
                          </span>
                        </span>
                      </label>
                    </li>
                  );
                })}
              </ul>
            </fieldset>
          )}

          <div className="form-actions">
            <button type="button" className="btn btn-primary" disabled={!anything || importing} onClick={() => void runImport()}>
              {importing ? <Spinner label="Importing" /> : <Icon name="plus" />}
              <span>{importing ? "Importing…" : selectedCount ? `Import ${Math.min(selectedCount, Math.max(room, 0))} ${selectedCount === 1 ? "block" : "blocks"}` : "Import profile"}</span>
            </button>
            <button type="button" className="btn btn-ghost" onClick={onClose} disabled={importing}>
              Cancel
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
