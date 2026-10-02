import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ApiError, api, errorMessage, type PageSummary } from "../api";
import { Icon } from "../components/Icon";
import { SlugStatusBadge, slugStatusError } from "../components/SlugStatus";
import { ConfirmButton, CopyButton, Notice, PageLoading, Spinner, TextField, formatRelative, useDocumentTitle } from "../components/ui";
import { useSlugCheck } from "../lib/useSlugCheck";
import { MAX_PAGES, normalizeSlug } from "../lib/validate";
import { UpgradeNote } from "../components/Pro";
import { usePlan } from "../plan";

/** The public base ("https://host/") inferred from an existing page URL. */
export function publicBase(pages: PageSummary[]): string {
  const first = pages[0];
  if (first?.url && first.url.endsWith(first.slug)) return first.url.slice(0, -first.slug.length);
  return `${window.location.origin}/`;
}

export function Dashboard() {
  useDocumentTitle("Your pages");
  const [pages, setPages] = useState<PageSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const { plan } = usePlan();
  const maxPages = plan?.pages ?? MAX_PAGES;

  const load = useCallback(() => {
    setError(null);
    api
      .listPages()
      .then((p) => {
        setPages(p);
        if (p.length === 0) setCreating(true);
      })
      .catch((err: unknown) => setError(errorMessage(err)));
  }, []);

  useEffect(load, [load]);

  if (error) {
    return (
      <div className="container">
        <Notice
          tone="danger"
          title="Couldn't load your pages"
          action={
            <button type="button" className="btn btn-sm" onClick={load}>
              Try again
            </button>
          }
        >
          {error}
        </Notice>
      </div>
    );
  }
  if (!pages) return <PageLoading />;

  const atLimit = pages.length >= maxPages;
  const proHasMore = plan !== null && plan.plan !== "pro" && plan.pro.pages > maxPages;
  const base = publicBase(pages);

  return (
    <div className="container">
      <div className="page-header">
        <div>
          <h1>Your pages</h1>
          <p className="muted">
            {pages.length} of {maxPages} pages used
          </p>
        </div>
        {!creating && (
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => setCreating(true)}
            disabled={atLimit}
            title={atLimit ? `You can have up to ${maxPages} pages.` : undefined}
          >
            <Icon name="plus" />
            <span>New page</span>
          </button>
        )}
      </div>

      {atLimit && !creating && !proHasMore && (
        <p className="muted small">You've reached the {maxPages}-page limit. Delete a page to make room for another.</p>
      )}
      {atLimit && !creating && proHasMore && plan && (
        <UpgradeNote>
          Free accounts can have {maxPages} pages. Pro allows {plan.pro.pages}.
        </UpgradeNote>
      )}

      {creating && !atLimit && (
        <CreatePageForm base={base} firstPage={pages.length === 0} onCancel={pages.length ? () => setCreating(false) : undefined} />
      )}

      {pages.length > 0 && (
        <ul className="page-list">
          {pages.map((p) => (
            <li key={p.id} className="page-card">
              <div className="page-card-main">
                <div className="page-card-title">
                  <Link to={`/pages/${p.id}`} className="page-card-link">
                    {p.title || p.slug}
                  </Link>
                  <span className={`badge ${p.published ? "badge-live" : "badge-draft"}`}>{p.published ? "Live" : "Draft"}</span>
                </div>
                <div className="page-card-url">
                  <code>{p.url.replace(/^https?:\/\//, "")}</code>
                </div>
                <p className="muted small">Edited {formatRelative(p.updated_at)}</p>
              </div>
              <div className="page-card-actions">
                <Link to={`/pages/${p.id}`} className="btn btn-sm btn-primary">
                  <Icon name="edit" />
                  <span>Edit</span>
                </Link>
                <CopyButton text={p.url} label="Copy link" />
                {p.published && (
                  <a className="btn btn-sm btn-ghost" href={p.url} target="_blank" rel="noreferrer">
                    <Icon name="external" />
                    <span>View</span>
                  </a>
                )}
                <ConfirmButton
                  label={`Delete`}
                  confirmLabel={`Delete ${p.slug}`}
                  onConfirm={async () => {
                    try {
                      await api.deletePage(p.id);
                      setPages((cur) => (cur ? cur.filter((x) => x.id !== p.id) : cur));
                    } catch (err) {
                      setError(errorMessage(err));
                    }
                  }}
                />
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function CreatePageForm({ base, firstPage, onCancel }: { base: string; firstPage: boolean; onCancel?: () => void }) {
  const navigate = useNavigate();
  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<{ slug?: string; title?: string }>({});
  const status = useSlugCheck(slug);
  const slugError = fieldErrors.slug ?? slugStatusError(status);
  const canSubmit = status.kind === "available" && !busy;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    setBusy(true);
    setError(null);
    setFieldErrors({});
    try {
      const page = await api.createPage(slug, title.trim() || slug);
      navigate(`/pages/${page.id}`);
    } catch (err) {
      if (err instanceof ApiError && (err.field === "slug" || err.field === "title")) {
        setFieldErrors({ [err.field]: err.message });
      } else {
        setError(errorMessage(err));
      }
      setBusy(false);
    }
  }

  return (
    <section className="card create-card" aria-labelledby="create-heading">
      <h2 id="create-heading">{firstPage ? "Create your first page" : "New page"}</h2>
      {firstPage && <p className="muted">Pick an address. You can change it later.</p>}
      <form onSubmit={onSubmit} className="create-form" noValidate>
        {error && <Notice tone="danger">{error}</Notice>}
        <div className="create-grid">
          <TextField
            label="Address"
            value={slug}
            onChange={(v) => setSlug(normalizeSlug(v))}
            prefix={base.replace(/^https?:\/\//, "")}
            required
            autoFocus
            spellCheck={false}
            maxLength={32}
            placeholder="yourname"
            error={slugError}
            status={<SlugStatusBadge status={status} />}
          />
          <TextField label="Title" value={title} onChange={setTitle} placeholder="Your name or brand" maxLength={120} error={fieldErrors.title} />
        </div>
        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={!canSubmit}>
            {busy ? <Spinner label="Creating" /> : null}
            Create page
          </button>
          {onCancel && (
            <button type="button" className="btn btn-ghost" onClick={onCancel}>
              Cancel
            </button>
          )}
        </div>
      </form>
    </section>
  );
}
