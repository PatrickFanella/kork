import { useCallback, useEffect, useState } from "react";
import { api, errorMessage, type PendingUser } from "../api";
import { Icon } from "../components/Icon";
import { Notice, formatRelative } from "../components/ui";

/** New accounts that can't publish yet, with a button to approve them early. */
export function PendingAccounts() {
  const [users, setUsers] = useState<PendingUser[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const load = useCallback(() => {
    setError(null);
    api
      .pendingUsers()
      .then(setUsers)
      .catch((err: unknown) => setError(errorMessage(err)));
  }, []);
  useEffect(load, [load]);

  async function approve(u: PendingUser) {
    setBusy(u.id);
    setError(null);
    try {
      await api.trustUser(u.id);
      setUsers((list) => list && list.filter((x) => x.id !== u.id));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  // Hide the section when the gate is off or nobody is waiting.
  if (!error && (!users || users.length === 0)) return null;

  return (
    <div className="container">
      <div className="page-header">
        <div>
          <h2>New accounts</h2>
          <p className="muted">These accounts can't publish until they are old enough or you approve them.</p>
        </div>
      </div>
      {error && <Notice tone="danger">{error}</Notice>}
      {users && users.length > 0 && (
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col">Pages</th>
                <th scope="col">Signed up</th>
                <th scope="col">Can publish</th>
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
                  </td>
                  <td>{u.pages.length === 0 ? <span className="muted">None</span> : u.pages.map((s) => `/${s}`).join(", ")}</td>
                  <td className="nowrap">{formatRelative(u.created_at)}</td>
                  <td className="nowrap">{new Date(u.publish_unlocks_at).toLocaleString()}</td>
                  <td>
                    <div className="row-actions">
                      <button type="button" className="btn btn-sm" disabled={busy !== null} onClick={() => approve(u)}>
                        <Icon name="check" />
                        <span>Approve</span>
                      </button>
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
