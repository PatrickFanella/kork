import { useCallback, useEffect, useState } from "react";
import { Navigate } from "react-router-dom";
import { api, errorMessage, type Report } from "../api";
import { useUser } from "../auth";
import { Icon } from "../components/Icon";
import { Notice, PageLoading, formatRelative, useDocumentTitle } from "../components/ui";

const REASONS: Record<string, string> = {
  spam: "Spam",
  scam: "Scam",
  impersonation: "Impersonation",
  illegal: "Illegal content",
  other: "Other",
};

export function AdminScreen() {
  const user = useUser();
  useDocumentTitle("Admin");
  if (!user.is_admin) return <Navigate to="/" replace />;
  return <Reports />;
}

function Reports() {
  const [reports, setReports] = useState<Report[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<"open" | "all">("open");
  const [suspended, setSuspended] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState<string | null>(null);

  const load = useCallback(() => {
    setError(null);
    api
      .reports()
      .then(setReports)
      .catch((err: unknown) => setError(errorMessage(err)));
  }, []);
  useEffect(load, [load]);

  async function resolve(r: Report) {
    setBusy(`resolve-${r.id}`);
    setError(null);
    try {
      await api.resolveReport(r.id);
      setReports((rs) => rs && rs.map((x) => (x.id === r.id ? { ...x, resolved: true } : x)));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  async function suspend(pageId: string, value: boolean) {
    setBusy(`suspend-${pageId}`);
    setError(null);
    try {
      await api.suspendPage(pageId, value);
      setSuspended((s) => ({ ...s, [pageId]: value }));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  if (!reports && !error) return <PageLoading />;

  const shown = (reports ?? []).filter((r) => filter === "all" || !r.resolved);
  const openCount = (reports ?? []).filter((r) => !r.resolved).length;

  return (
    <div className="container">
      <div className="page-header">
        <div>
          <h1>Reports</h1>
          <p className="muted">{openCount} open</p>
        </div>
        <div className="segmented" role="group" aria-label="Filter reports">
          <button type="button" className="segmented-option" aria-pressed={filter === "open"} onClick={() => setFilter("open")}>
            Open
          </button>
          <button type="button" className="segmented-option" aria-pressed={filter === "all"} onClick={() => setFilter("all")}>
            All
          </button>
        </div>
      </div>
      {error && (
        <Notice
          tone="danger"
          action={
            <button type="button" className="btn btn-sm" onClick={load}>
              Reload
            </button>
          }
        >
          {error}
        </Notice>
      )}
      {reports && shown.length === 0 ? (
        <div className="empty-state">
          <Icon name="shield" size={32} />
          <h2>{filter === "open" ? "No open reports" : "No reports yet"}</h2>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Page</th>
                <th scope="col">Reason</th>
                <th scope="col">Details</th>
                <th scope="col">Reported</th>
                <th scope="col">
                  <span className="visually-hidden">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {shown.map((r) => {
                const isSuspended = suspended[r.page_id];
                return (
                  <tr key={r.id} className={r.resolved ? "is-resolved" : undefined}>
                    <td>
                      <a href={`/${encodeURIComponent(r.slug)}`} target="_blank" rel="noreferrer">
                        /{r.slug}
                      </a>
                    </td>
                    <td>
                      <span className={`badge ${r.reason === "scam" || r.reason === "illegal" ? "badge-danger" : "badge-draft"}`}>
                        {REASONS[r.reason] ?? r.reason}
                      </span>
                    </td>
                    <td className="report-details">{r.details || <span className="muted">No details</span>}</td>
                    <td className="nowrap">{formatRelative(r.created_at)}</td>
                    <td>
                      <div className="row-actions">
                        {r.resolved ? (
                          <span className="muted small">Resolved</span>
                        ) : (
                          <button type="button" className="btn btn-sm" disabled={busy !== null} onClick={() => resolve(r)}>
                            <Icon name="check" />
                            <span>Resolve</span>
                          </button>
                        )}
                        <button
                          type="button"
                          className={`btn btn-sm ${isSuspended ? "" : "btn-danger"}`}
                          disabled={busy !== null}
                          onClick={() => suspend(r.page_id, !isSuspended)}
                        >
                          <Icon name="ban" />
                          <span>{isSuspended ? "Unsuspend page" : "Suspend page"}</span>
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
