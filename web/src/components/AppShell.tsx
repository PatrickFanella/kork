import { useState } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { api, errorMessage } from "../api";
import { useAuth, useUser } from "../auth";
import { Icon } from "./Icon";
import { Logo } from "./Logo";

export function AppShell() {
  const user = useUser();
  const { logout } = useAuth();
  const navigate = useNavigate();

  return (
    <div className="app">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="topbar">
        <Link to="/" className="brand" aria-label="Zelda, your pages">
          <Logo />
          <span>Zelda</span>
        </Link>
        <nav className="topnav" aria-label="Main">
          <NavLink to="/" end>
            Pages
          </NavLink>
          {user.is_admin && <NavLink to="/admin">Admin</NavLink>}
        </nav>
        <div className="topbar-user">
          <span className="topbar-email" title={user.email}>
            {user.email}
          </span>
          <button
            type="button"
            className="btn btn-sm btn-ghost"
            onClick={async () => {
              await logout().catch(() => undefined);
              navigate("/login", { replace: true });
            }}
          >
            <Icon name="logout" />
            <span>Sign out</span>
          </button>
        </div>
      </header>
      {!user.email_verified && <VerifyBanner email={user.email} />}
      <main id="main" tabIndex={-1}>
        <Outlet />
      </main>
    </div>
  );
}

function VerifyBanner({ email }: { email: string }) {
  const [state, setState] = useState<"idle" | "sending" | "sent" | { error: string }>("idle");
  return (
    <div className="verify-banner" role="status">
      <Icon name="at" />
      <p>
        Confirm <strong>{email}</strong> to publish your pages. Check your inbox for the link.
      </p>
      {state === "sent" ? (
        <span className="verify-banner-done">
          <Icon name="check" /> Sent. It can take a minute to arrive.
        </span>
      ) : (
        <button
          type="button"
          className="btn btn-sm"
          disabled={state === "sending"}
          onClick={async () => {
            setState("sending");
            try {
              await api.resendVerification();
              setState("sent");
            } catch (err) {
              setState({ error: errorMessage(err) });
            }
          }}
        >
          {state === "sending" ? "Sending…" : "Resend email"}
        </button>
      )}
      {typeof state === "object" && <span className="field-error">{state.error}</span>}
    </div>
  );
}
