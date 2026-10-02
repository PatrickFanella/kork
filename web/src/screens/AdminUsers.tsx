import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, errorMessage, type AdminUser } from "../api";
import { useUser } from "../auth";
import { Icon } from "../components/Icon";
import { ProBadge } from "../components/Pro";
import { Notice, TextField, formatDate, formatRelative } from "../components/ui";
import { usePlan } from "../plan";

/** Admin list of accounts with their plan, and manual Pro grants. */
export function AdminUsers() {
  const me = useUser();
  const { refresh: refreshMyPlan } = usePlan();
  const [query, setQuery] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [users, setUsers] = useState<AdminUser[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  // Per-user expiry date input (YYYY-MM-DD); empty means no expiry.
  const [expiry, setExpiry] = useState<Record<string, string>>({});

  const load = useCallback((q: string) => {
    setError(null);
    api
      .adminUsers(q)
      .then(setUsers)
      .catch((err: unknown) => setError(errorMessage(err)));
  }, []);
  useEffect(() => load(submitted), [load, submitted]);

  function onSearch(e: FormEvent) {
    e.preventDefault();
    setSubmitted(query.trim());
  }

  async function setPlan(u: AdminUser, plan: "free" | "pro") {
    setBusy(u.id);
    setError(null);
    const day = expiry[u.id];
    // End of the chosen day in UTC.
    const expiresAt = plan === "pro" && day ? `${day}T23:59:59Z` : null;
    try {
      await api.setUserPlan(u.id, plan, expiresAt);
      load(submitted);
      if (u.id === me.id) void refreshMyPlan();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="container">
      <div className="page-header">
        <div>
          <h2>Users</h2>
          <p className="muted">Search by email. Grant Pro by hand, with an optional end date, or switch an account back to Free.</p>
        </div>
      </div>
      <form className="inline-form" onSubmit={onSearch} role="search">
        <TextField label="Email contains" value={query} onChange={setQuery} placeholder="name@example.com" autoComplete="off" />
        <button type="submit" className="btn">
          <Icon name="search" />
          <span>Search</span>
        </button>
      </form>
      {error && <Notice tone="danger">{error}</Notice>}
      {users && users.length === 0 && <p className="muted">No accounts match.</p>}
      {users && users.length > 0 && (
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col">Plan</th>
                <th scope="col">Pages</th>
                <th scope="col">Signed up</th>
                <th scope="col">
                  <span className="visually-hidden">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id}>
                  <td>
                    {u.email}
                    {!u.email_verified && <span className="muted small"> (unverified)</span>}
                    {u.suspended && <span className="badge badge-danger">Suspended</span>}
                  </td>
                  <td>
                    <PlanCell user={u} />
                  </td>
                  <td>{u.pages}</td>
                  <td className="nowrap">{formatRelative(u.created_at)}</td>
                  <td>
                    <div className="row-actions">
                      <label className="visually-hidden" htmlFor={`expiry-${u.id}`}>
                        Pro end date for {u.email} (optional)
                      </label>
                      <input
                        id={`expiry-${u.id}`}
                        type="date"
                        className="admin-date"
                        value={expiry[u.id] ?? ""}
                        onChange={(e) => setExpiry((x) => ({ ...x, [u.id]: e.target.value }))}
                        title="Pro end date (optional)"
                      />
                      <button type="button" className="btn btn-sm btn-primary" disabled={busy !== null} onClick={() => setPlan(u, "pro")}>
                        <span>{u.effective_plan === "pro" ? "Update Pro" : "Grant Pro"}</span>
                      </button>
                      {u.plan === "pro" && (
                        <button type="button" className="btn btn-sm" disabled={busy !== null} onClick={() => setPlan(u, "free")}>
                          <span>Revoke</span>
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function PlanCell({ user: u }: { user: AdminUser }) {
  if (u.effective_plan === "pro") {
    return (
      <>
        <ProBadge /> <span className="small muted">{u.plan_expires_at ? `until ${formatDate(u.plan_expires_at)}` : "no end date"}</span>
      </>
    );
  }
  if (u.plan === "pro" && u.plan_expires_at) {
    return (
      <span className="small">
        Free <span className="muted">(Pro ended {formatDate(u.plan_expires_at)})</span>
      </span>
    );
  }
  return <span className="small">Free</span>;
}
