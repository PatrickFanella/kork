import { useCallback, useEffect, useState } from "react";
import { api, errorMessage, type SignupContacts as Contacts } from "../../api";
import { ConfirmButton, Notice, Spinner } from "../../components/ui";

/** The confirmed list of an email sign-up block, shown while editing it. */
export function SignupContacts({ pageId, blockId, brevo }: { pageId: string; blockId: string; brevo: boolean }) {
  const [list, setList] = useState<Contacts | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    api
      .signupContacts(pageId, blockId)
      .then((c) => {
        setList(c);
        setError(null);
      })
      .catch((err: unknown) => setError(errorMessage(err)));
  }, [pageId, blockId]);
  useEffect(load, [load]);

  async function remove(id: string) {
    try {
      await api.deleteSignupContact(pageId, blockId, id);
      setList((l) => (l ? { ...l, contacts: l.contacts.filter((c) => c.id !== id) } : l));
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  if (error) return <Notice tone="danger">{error}</Notice>;
  if (!list) return <Spinner label="Loading sign-ups" />;
  const n = list.contacts.length;

  return (
    <section className="signup-contacts" aria-labelledby={`contacts-${blockId}`}>
      <div className="signup-contacts-head">
        <h3 id={`contacts-${blockId}`}>
          {n} confirmed {n === 1 ? "address" : "addresses"}
        </h3>
        {n > 0 && (
          <a className="btn btn-sm" href={api.signupContactsCsvUrl(pageId, blockId)} download>
            Download CSV
          </a>
        )}
      </div>
      <p className="muted small">
        {list.pending > 0 && `${list.pending} waiting for visitors to confirm. `}
        {list.awaiting_push > 0 && `${list.awaiting_push} confirmed but not yet accepted by Brevo; retried automatically. `}
        {brevo
          ? n > 0
            ? "New confirmed addresses go to Brevo. The ones listed here were collected before you switched."
            : "Confirmed addresses go to Brevo and aren't kept here."
          : "Deleting this block deletes this list. Download the CSV first."}
      </p>
      {n > 0 && (
        <ul className="signup-contact-list">
          {list.contacts.map((c) => (
            <li key={c.id}>
              <span className="signup-contact-email">{c.email}</span>
              <time className="muted small" dateTime={c.confirmed_at}>
                {new Date(c.confirmed_at).toLocaleDateString()}
              </time>
              <ConfirmButton label={`Remove ${c.email}`} confirmLabel="Remove" hideLabel onConfirm={() => remove(c.id)} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
