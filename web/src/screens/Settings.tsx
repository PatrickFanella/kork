import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { ApiError, EXPORT_URL, api, errorMessage, type Account } from "../api";
import { useAuth } from "../auth";
import { Icon } from "../components/Icon";
import { Logo } from "../components/Logo";
import { Notice, PageLoading, Spinner, TextField, formatDate, useDocumentTitle } from "../components/ui";
import { PASSWORD_MIN, passwordProblem } from "../lib/validate";

type Errors = Record<string, string>;

/** Map an API error to a field message when the server names a known field. */
function fieldErrors(err: unknown, known: string[]): { fields: Errors; general: string | null } {
  if (err instanceof ApiError && err.field && known.includes(err.field)) {
    return { fields: { [err.field]: err.message }, general: null };
  }
  return { fields: {}, general: errorMessage(err) };
}

export function SettingsScreen() {
  useDocumentTitle("Settings");
  const [account, setAccount] = useState<Account | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    setError(null);
    api
      .account()
      .then(setAccount)
      .catch((err: unknown) => setError(errorMessage(err)));
  }, []);
  useEffect(load, [load]);

  if (error) {
    return (
      <div className="container narrow">
        <Notice
          tone="danger"
          title="Couldn't load your settings"
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
  if (!account) return <PageLoading />;

  return (
    <div className="container narrow">
      <div className="page-header">
        <div>
          <h1>Settings</h1>
          <p className="muted">
            Signed in as <strong>{account.user.email}</strong> since {formatDate(account.user.created_at)}.
          </p>
        </div>
      </div>
      <EmailSection account={account} onChange={setAccount} />
      <PasswordSection />
      <ExportSection />
      <DeleteSection />
      <p className="muted small">
        <a href="/terms" target="_blank" rel="noopener">
          Terms
        </a>{" "}
        ·{" "}
        <a href="/privacy" target="_blank" rel="noopener">
          Privacy Policy
        </a>
        {account.terms_version ? ` · You accepted version ${account.terms_version}.` : null}
      </p>
    </div>
  );
}

function EmailSection({ account, onChange }: { account: Account; onChange: (a: Account) => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [fields, setFields] = useState<Errors>({});
  const [general, setGeneral] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setFields({});
    setGeneral(null);
    try {
      onChange(await api.changeEmail(email.trim(), password));
      setEmail("");
      setPassword("");
    } catch (err) {
      const s = fieldErrors(err, ["email", "password"]);
      setFields(s.fields);
      setGeneral(s.general);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="card section" aria-labelledby="email-heading">
      <div className="section-intro">
        <h2 id="email-heading">Email</h2>
        <p className="muted">
          We'll send a link to the new address. Your account keeps using <strong>{account.user.email}</strong> until you open
          it.
        </p>
      </div>
      {account.pending_email && (
        <Notice tone="info" title="Waiting for confirmation">
          Open the link we sent to <strong>{account.pending_email}</strong> to switch. The link expires after 24 hours.
        </Notice>
      )}
      <form onSubmit={onSubmit} className="stack" noValidate>
        {general && <Notice tone="danger">{general}</Notice>}
        <TextField label="New email" type="email" value={email} onChange={setEmail} autoComplete="email" required error={fields.email} />
        <TextField
          label="Current password"
          type="password"
          value={password}
          onChange={setPassword}
          autoComplete="current-password"
          required
          error={fields.password}
        />
        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={busy || !email || !password}>
            {busy ? <Spinner label="Sending" /> : null}
            Send confirmation link
          </button>
        </div>
      </form>
    </section>
  );
}

function PasswordSection() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [touched, setTouched] = useState(false);
  const [done, setDone] = useState(false);
  const [fields, setFields] = useState<Errors>({});
  const [general, setGeneral] = useState<string | null>(null);
  const problem = passwordProblem(next);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setTouched(true);
    setDone(false);
    if (problem) return;
    setBusy(true);
    setFields({});
    setGeneral(null);
    try {
      await api.changePassword(current, next);
      setDone(true);
      setCurrent("");
      setNext("");
      setTouched(false);
    } catch (err) {
      const s = fieldErrors(err, ["current_password", "new_password"]);
      setFields(s.fields);
      setGeneral(s.general);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="card section" aria-labelledby="password-heading">
      <div className="section-intro">
        <h2 id="password-heading">Password</h2>
        <p className="muted">Changing it signs you out on every other device.</p>
      </div>
      <form onSubmit={onSubmit} className="stack" noValidate>
        {general && <Notice tone="danger">{general}</Notice>}
        {done && <Notice tone="success">Password changed. Other sessions were signed out.</Notice>}
        <TextField
          label="Current password"
          type="password"
          value={current}
          onChange={setCurrent}
          autoComplete="current-password"
          required
          error={fields.current_password}
        />
        <TextField
          label="New password"
          type="password"
          value={next}
          onChange={setNext}
          autoComplete="new-password"
          required
          hint={`At least ${PASSWORD_MIN} characters.`}
          error={fields.new_password ?? (touched ? problem : null)}
        />
        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={busy || !current || !next}>
            {busy ? <Spinner label="Saving" /> : null}
            Change password
          </button>
        </div>
      </form>
    </section>
  );
}

function ExportSection() {
  return (
    <section className="card section" aria-labelledby="export-heading">
      <div className="section-row">
        <div className="section-intro">
          <h2 id="export-heading">Your data</h2>
          <p className="muted">
            A ZIP with <code>account.json</code> (your email, pages, blocks, themes, domains and daily stats) and every image you
            uploaded.
          </p>
        </div>
        <a className="btn" href={EXPORT_URL} download>
          <Icon name="down" />
          <span>Download my data</span>
        </a>
      </div>
    </section>
  );
}

function DeleteSection() {
  const { setUser } = useAuth();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.deleteAccount(password);
      setUser(null);
      navigate("/login", { replace: true });
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  }

  return (
    <section className="card section" aria-labelledby="delete-heading">
      <div className="section-row">
        <div className="section-intro">
          <h2 id="delete-heading">Delete account</h2>
          <p className="muted">
            Removes your account, pages, domains, stats and uploaded images right away. Your page links stop working. Backups
            keep a copy for about two weeks.
          </p>
        </div>
        {!open && (
          <button type="button" className="btn btn-danger" onClick={() => setOpen(true)}>
            <Icon name="trash" />
            <span>Delete account</span>
          </button>
        )}
      </div>
      {open && (
        <form onSubmit={onSubmit} className="stack" noValidate>
          {error && <Notice tone="danger">{error}</Notice>}
          <TextField
            label="Enter your password to confirm"
            type="password"
            value={password}
            onChange={setPassword}
            autoComplete="current-password"
            required
            autoFocus
          />
          <div className="form-actions">
            <button type="submit" className="btn btn-danger" disabled={busy || !password}>
              {busy ? <Spinner label="Deleting" /> : null}
              Delete my account
            </button>
            <button type="button" className="btn btn-ghost" onClick={() => setOpen(false)} disabled={busy}>
              Cancel
            </button>
          </div>
        </form>
      )}
    </section>
  );
}

/** Landing screen for the link sent to a new email address. */
export function ConfirmEmailScreen() {
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  const { user, refresh } = useAuth();
  const [state, setState] = useState<"working" | "done" | { error: string }>(
    token ? "working" : { error: "This link is missing its token." },
  );
  const started = useRef(false);
  useDocumentTitle("Confirm your new email");

  useEffect(() => {
    if (!token || started.current) return;
    started.current = true;
    api
      .confirmEmail(token)
      .then(() => {
        setState("done");
        void refresh();
      })
      .catch((err: unknown) => setState({ error: errorMessage(err) }));
  }, [token, refresh]);

  return (
    <main className="auth-page">
      <div className="auth-card">
        <Link to="/" className="auth-brand" aria-label="Kork home">
          <Logo size={34} />
          <span>kork</span>
        </Link>
        <h1>{state === "done" ? "Email changed" : state === "working" ? "Confirming your new email" : "We couldn't change your email"}</h1>
        {state === "working" && (
          <div className="center-block">
            <Spinner label="Confirming" />
          </div>
        )}
        {state === "done" && (
          <div className="stack">
            <Notice tone="success">Your account now uses this address. Sign in with it from now on.</Notice>
            <Link className="btn btn-primary btn-block" to={user ? "/settings" : "/login"}>
              {user ? "Back to settings" : "Sign in"}
            </Link>
          </div>
        )}
        {typeof state === "object" && (
          <div className="stack">
            <Notice tone="danger">{state.error}</Notice>
            <Link className="btn btn-block" to={user ? "/settings" : "/login"}>
              {user ? "Back to settings" : "Sign in"}
            </Link>
          </div>
        )}
      </div>
    </main>
  );
}
