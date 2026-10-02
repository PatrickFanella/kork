import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { ApiError, api, errorMessage, type Page } from "../../api";
import { useCatalogState } from "../../catalog";
import { Icon, type IconName } from "../../components/Icon";
import { Notice, PageLoading, useDocumentTitle } from "../../components/ui";
import { BlocksTab } from "./BlocksTab";
import { DomainsTab } from "./DomainsTab";
import { ProfileTab } from "./ProfileTab";
import { ShareTab } from "./ShareTab";
import { StatsTab } from "./StatsTab";
import { ThemeTab } from "./ThemeTab";
import type { EditorProps } from "./types";

const TABS: { key: string; label: string; icon: IconName }[] = [
  { key: "blocks", label: "Blocks", icon: "blocks" },
  { key: "profile", label: "Profile", icon: "user" },
  { key: "theme", label: "Theme", icon: "palette" },
  { key: "domains", label: "Domains", icon: "globe" },
  { key: "stats", label: "Stats", icon: "chart" },
  { key: "share", label: "Share", icon: "share" },
];

export function PageEditor() {
  const { id = "" } = useParams();
  const { state: catalogState, reload: reloadCatalog } = useCatalogState();
  const [page, setPageState] = useState<Page | null>(null);
  const [loadError, setLoadError] = useState<{ message: string; notFound: boolean } | null>(null);
  const [version, setVersion] = useState(0);
  const [params, setParams] = useSearchParams();
  const [previewOpen, setPreviewOpen] = useState(false);
  const tabRefs = useRef<Record<string, HTMLButtonElement | null>>({});

  const tab = TABS.some((t) => t.key === params.get("tab")) ? (params.get("tab") as string) : "blocks";

  useDocumentTitle(page ? page.title || page.slug : "Editor");

  const load = useCallback(() => {
    setLoadError(null);
    api
      .getPage(id)
      .then(setPageState)
      .catch((err: unknown) =>
        setLoadError({ message: errorMessage(err), notFound: err instanceof ApiError && err.status === 404 }),
      );
  }, [id]);

  useEffect(load, [load]);

  const setPage = useCallback((update: (p: Page) => Page) => {
    setPageState((p) => (p ? update(p) : p));
  }, []);
  const saved = useCallback(() => setVersion((v) => v + 1), []);

  function selectTab(key: string, focus = false) {
    const next = new URLSearchParams(params);
    if (key === "blocks") next.delete("tab");
    else next.set("tab", key);
    setParams(next, { replace: true });
    if (focus) tabRefs.current[key]?.focus();
  }

  function onTabKey(e: KeyboardEvent<HTMLButtonElement>, index: number) {
    let target = -1;
    if (e.key === "ArrowRight" || e.key === "ArrowDown") target = (index + 1) % TABS.length;
    else if (e.key === "ArrowLeft" || e.key === "ArrowUp") target = (index - 1 + TABS.length) % TABS.length;
    else if (e.key === "Home") target = 0;
    else if (e.key === "End") target = TABS.length - 1;
    if (target >= 0) {
      e.preventDefault();
      const t = TABS[target];
      if (t) selectTab(t.key, true);
    }
  }

  if (loadError) {
    return (
      <div className="container narrow">
        <Notice
          tone="danger"
          title={loadError.notFound ? "Page not found" : "Couldn't load this page"}
          action={
            loadError.notFound ? (
              <Link to="/" className="btn btn-sm">
                Back to pages
              </Link>
            ) : (
              <button type="button" className="btn btn-sm" onClick={load}>
                Try again
              </button>
            )
          }
        >
          {loadError.notFound ? "It may have been deleted." : loadError.message}
        </Notice>
      </div>
    );
  }
  if (catalogState.status === "error") {
    return (
      <div className="container narrow">
        <Notice
          tone="danger"
          title="Couldn't load the editor catalog"
          action={
            <button type="button" className="btn btn-sm" onClick={reloadCatalog}>
              Try again
            </button>
          }
        >
          {catalogState.message}
        </Notice>
      </div>
    );
  }
  if (!page || catalogState.status !== "ready") return <PageLoading label="Loading editor" />;

  const props: EditorProps = { page, setPage, saved };
  const previewSrc = `/preview/${encodeURIComponent(page.id)}?v=${version}`;

  return (
    <div className={`editor${previewOpen ? " preview-open" : ""}`}>
      <div className="editor-main">
        <div className="editor-header">
          <Link to="/" className="back-link">
            <Icon name="back" size={16} />
            <span>Pages</span>
          </Link>
          <div className="editor-title">
            <h1>{page.title || page.slug}</h1>
            <span className={`badge ${page.suspended ? "badge-danger" : page.published ? "badge-live" : "badge-draft"}`}>
              {page.suspended ? "Suspended" : page.published ? "Live" : "Draft"}
            </span>
          </div>
          {page.published && !page.suspended ? (
            <a className="editor-url" href={page.url} target="_blank" rel="noreferrer">
              {page.url.replace(/^https?:\/\//, "")}
              <Icon name="external" size={14} />
            </a>
          ) : (
            <span className="editor-url muted">{page.url.replace(/^https?:\/\//, "")}</span>
          )}
        </div>

        {page.suspended && (
          <Notice tone="danger" title="This page is suspended">
            It isn't visible to the public. You can keep editing; contact support if you think this is a mistake.
          </Notice>
        )}

        <div className="tabs" role="tablist" aria-label="Page settings">
          {TABS.map((t, i) => (
            <button
              key={t.key}
              ref={(el) => {
                tabRefs.current[t.key] = el;
              }}
              type="button"
              role="tab"
              id={`tab-${t.key}`}
              aria-selected={tab === t.key}
              aria-controls={`panel-${t.key}`}
              tabIndex={tab === t.key ? 0 : -1}
              className="tab"
              onClick={() => selectTab(t.key)}
              onKeyDown={(e) => onTabKey(e, i)}
            >
              <Icon name={t.icon} size={16} />
              <span>{t.label}</span>
            </button>
          ))}
        </div>

        <div className="tab-panel" role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`} tabIndex={0}>
          {tab === "blocks" && <BlocksTab {...props} />}
          {tab === "profile" && <ProfileTab {...props} />}
          {tab === "theme" && <ThemeTab {...props} />}
          {tab === "domains" && <DomainsTab {...props} />}
          {tab === "stats" && <StatsTab {...props} />}
          {tab === "share" && <ShareTab {...props} />}
        </div>
      </div>

      <aside className="editor-preview" aria-label="Live preview">
        <div className="preview-toolbar">
          <span className="preview-label">
            <span className="live-dot" aria-hidden="true" /> Preview
          </span>
          <div className="preview-tools">
            <button type="button" className="btn btn-sm btn-ghost" onClick={saved} title="Reload preview">
              <Icon name="refresh" size={16} />
              <span className="visually-hidden">Reload preview</span>
            </button>
            <a className="btn btn-sm btn-ghost" href={previewSrc} target="_blank" rel="noreferrer" title="Open preview in a new tab">
              <Icon name="external" size={16} />
              <span className="visually-hidden">Open preview in a new tab</span>
            </a>
          </div>
        </div>
        <div className="phone">
          <iframe key={version} src={previewSrc} title="Live preview of your page" className="phone-screen" />
        </div>
      </aside>

      <button
        type="button"
        className="btn btn-primary preview-toggle"
        aria-pressed={previewOpen}
        onClick={() => {
          setPreviewOpen((o) => !o);
          window.scrollTo({ top: 0 });
        }}
      >
        <Icon name={previewOpen ? "edit" : "eye"} />
        <span>{previewOpen ? "Back to editor" : "Preview"}</span>
      </button>
    </div>
  );
}
