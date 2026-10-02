import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { ApiError, api, errorMessage } from "../api";
import { useAuth } from "../auth";
import { Logo } from "../components/Logo";
import { Notice, Spinner, TextField, useDocumentTitle } from "../components/ui";
import { PASSWORD_MIN, passwordProblem } from "../lib/validate";

function AuthLayout({ title, subtitle, children, footer }: { title: string; subtitle?: ReactNode; children: ReactNode; footer?: ReactNode }) {
  useDocumentTitle(title);
  return (
    <main className="auth-page">
      <div className="auth-card">
        <Link to="/" className="auth-brand" aria-label="Kork home">
          <Logo size={34} />
          <span>kork</span>
        </Link>
        <h1>{title}</h1>
        {subtitle && <p className="auth-subtitle">{subtitle}</p>}
        {children}
      </div>
      {footer && <p className="auth-footer">{footer}</p>}
    </main>
  );
}

type FieldErrors = Partial<Record<"email" | "password" | "confirm", string>>;

function splitError(err: unknown): { fields: FieldErrors; general: string | null } {
  if (err instanceof ApiError && (err.field === "email" || err.field === "password")) {
    return { fields: { [err.field]: err.message }, general: null };
  }
  return { fields: {}, general: errorMessage(err) };
}

export function LoginScreen() {
  const { setUser } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from ?? "/";
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [fields, setFields] = useState<FieldErrors>({});
  const [general, setGeneral] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setFields({});
    setGeneral(null);
    try {
      const user = await api.login(email.trim(), password);
      setUser(user);
      navigate(from, { replace: true });
    } catch (err) {
      const s = splitError(err);
      setFields(s.fields);
      setGeneral(s.general);
      setBusy(false);
    }
  }

  return (
    <AuthLayout
      title="Sign in"
      subtitle="Edit your link pages."
      footer={
        <>
          New here? <Link to="/signup">Create an account</Link>
        </>
      }
    >
      <form onSubmit={onSubmit} className="stack" noValidate>
        {general && <Notice tone="danger">{general}</Notice>}
        <TextField label="Email" type="email" value={email} onChange={setEmail} autoComplete="email" required autoFocus error={fields.email} />
        <TextField
          label="Password"
          type="password"
          value={password}
          onChange={setPassword}
          autoComplete="current-password"
          required
          error={fields.password}
        />
        <div className="auth-row">
          <Link to="/forgot" className="muted-link">
            Forgot password?
          </Link>
        </div>
        <button type="submit" className="btn btn-primary btn-block" disabled={busy || !email || !password}>
          {busy ? <Spinner label="Signing in" /> : null}
          Sign in
        </button>
      </form>
    </AuthLayout>
  );
}

export function SignupScreen() {
  const { setUser } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [touched, setTouched] = useState(false);
  const [fields, setFields] = useState<FieldErrors>({});
  const [general, setGeneral] = useState<string | null>(null);
  const pwProblem = passwordProblem(password);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (pwProblem) return;
    setBusy(true);
    setFields({});
    setGeneral(null);
    try {
      const user = await api.signup(email.trim(), password);
      setUser(user);
      navigate("/", { replace: true });
    } catch (err) {
      const s = splitError(err);
      setFields(s.fields);
      setGeneral(s.general);
      setBusy(false);
    }
  }

  return (
    <AuthLayout
      title="Create your account"
      subtitle="One link for everything you make. Free, up to five pages."
      footer={
        <>
          Already have an account? <Link to="/login">Sign in</Link>
        </>
      }
    >
      <form onSubmit={onSubmit} className="stack" noValidate>
        {general && <Notice tone="danger">{general}</Notice>}
        <TextField label="Email" type="email" value={email} onChange={setEmail} autoComplete="email" required autoFocus error={fields.email} />
        <TextField
          label="Password"
          type="password"
          value={password}
          onChange={setPassword}
          autoComplete="new-password"
          required
          hint={`At least ${PASSWORD_MIN} characters.`}
          error={fields.password ?? (touched ? pwProblem : null)}
        />
        <button type="submit" className="btn btn-primary btn-block" disabled={busy || !email}>
          {busy ? <Spinner label="Creating account" /> : null}
          Create account
        </button>
      </form>
    </AuthLayout>
  );
}

export function ForgotScreen() {
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.forgot(email.trim());
      setSent(true);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthLayout
      title="Reset your password"
      subtitle={sent ? undefined : "We'll email you a link to choose a new one."}
      footer={<Link to="/login">Back to sign in</Link>}
    >
      {sent ? (
        <Notice tone="success" title="Check your email">
          If an account exists for <strong>{email.trim()}</strong>, a reset link is on its way. The link expires, so use it soon.
        </Notice>
      ) : (
        <form onSubmit={onSubmit} className="stack" noValidate>
          {error && <Notice tone="danger">{error}</Notice>}
          <TextField label="Email" type="email" value={email} onChange={setEmail} autoComplete="email" required autoFocus />
          <button type="submit" className="btn btn-primary btn-block" disabled={busy || !email}>
            {busy ? <Spinner label="Sending" /> : null}
            Send reset link
          </button>
        </form>
      )}
    </AuthLayout>
  );
}

export function ResetScreen() {
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  const { user } = useAuth();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pwProblem = passwordProblem(password);
  const mismatch = confirm && confirm !== password ? "Passwords don't match." : null;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (pwProblem || confirm !== password) return;
    setBusy(true);
    setError(null);
    try {
      await api.reset(token, password);
      setDone(true);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  if (!token) {
    return (
      <AuthLayout title="Reset link missing" footer={<Link to="/forgot">Request a new link</Link>}>
        <Notice tone="warning">This page needs the link from your reset email. Open the email and click the link again.</Notice>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title={done ? "Password updated" : "Choose a new password"}>
      {done ? (
        <div className="stack">
          <Notice tone="success">Your password has been changed.</Notice>
          <Link className="btn btn-primary btn-block" to={user ? "/" : "/login"}>
            {user ? "Go to your pages" : "Sign in"}
          </Link>
        </div>
      ) : (
        <form onSubmit={onSubmit} className="stack" noValidate>
          {error && (
            <Notice tone="danger" action={<Link to="/forgot">Request a new link</Link>}>
              {error}
            </Notice>
          )}
          <TextField
            label="New password"
            type="password"
            value={password}
            onChange={setPassword}
            autoComplete="new-password"
            required
            autoFocus
            hint={`At least ${PASSWORD_MIN} characters.`}
            error={touched ? pwProblem : null}
          />
          <TextField
            label="Confirm new password"
            type="password"
            value={confirm}
            onChange={setConfirm}
            autoComplete="new-password"
            required
            error={touched || confirm.length >= password.length ? mismatch : null}
          />
          <button type="submit" className="btn btn-primary btn-block" disabled={busy}>
            {busy ? <Spinner label="Saving" /> : null}
            Set password
          </button>
        </form>
      )}
    </AuthLayout>
  );
}

export function VerifyScreen() {
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  const { user, setUser } = useAuth();
  const [state, setState] = useState<"working" | "done" | { error: string }>(token ? "working" : { error: "This link is missing its token." });
  const started = useRef(false);

  useEffect(() => {
    if (!token || started.current) return;
    started.current = true;
    api
      .verify(token)
      .then((u) => {
        setUser(u);
        setState("done");
      })
      .catch((err: unknown) => setState({ error: errorMessage(err) }));
  }, [token, setUser]);

  return (
    <AuthLayout title={state === "done" ? "Email confirmed" : state === "working" ? "Confirming your email" : "We couldn't confirm your email"}>
      {state === "working" && (
        <div className="center-block">
          <Spinner label="Confirming" />
        </div>
      )}
      {state === "done" && (
        <div className="stack">
          <Notice tone="success">Thanks. You can now publish your pages.</Notice>
          <Link className="btn btn-primary btn-block" to="/">
            Go to your pages
          </Link>
        </div>
      )}
      {typeof state === "object" && (
        <div className="stack">
          <Notice tone="danger">{state.error}</Notice>
          {user && !user.email_verified ? <ResendButton /> : null}
          <Link className="btn btn-block" to={user ? "/" : "/login"}>
            {user ? "Go to your pages" : "Sign in"}
          </Link>
        </div>
      )}
    </AuthLayout>
  );
}

function ResendButton() {
  const [state, setState] = useState<"idle" | "sending" | "sent">("idle");
  const [error, setError] = useState<string | null>(null);
  return (
    <>
      <button
        type="button"
        className="btn btn-primary btn-block"
        disabled={state !== "idle"}
        onClick={async () => {
          setState("sending");
          try {
            await api.resendVerification();
            setState("sent");
          } catch (err) {
            setError(errorMessage(err));
            setState("idle");
          }
        }}
      >
        {state === "sent" ? "New link sent" : state === "sending" ? "Sending…" : "Send a new link"}
      </button>
      {error && <p className="field-error">{error}</p>}
    </>
  );
}
