import type { ReactNode } from "react";
import { BrowserRouter, Navigate, Outlet, Route, Routes, useLocation } from "react-router-dom";
import { AuthProvider, useAuth } from "./auth";
import { CatalogProvider } from "./catalog";
import { Notice, PageLoading } from "./components/ui";
import { AppShell } from "./components/AppShell";
import { ForgotScreen, LoginScreen, ResetScreen, SignupScreen, VerifyScreen } from "./screens/AuthScreens";
import { Dashboard } from "./screens/Dashboard";
import { PageEditor } from "./screens/editor/PageEditor";
import { AdminScreen } from "./screens/Admin";
import { NotFound } from "./screens/NotFound";
import { ConfirmEmailScreen, SettingsScreen } from "./screens/Settings";

export function App() {
  return (
    <BrowserRouter basename="/app">
      <AuthProvider>
        <CatalogProvider>
          <Routes>
            <Route element={<GuestOnly />}>
              <Route path="/login" element={<LoginScreen />} />
              <Route path="/signup" element={<SignupScreen />} />
              <Route path="/forgot" element={<ForgotScreen />} />
            </Route>
            <Route path="/reset" element={<ResetScreen />} />
            <Route path="/verify" element={<VerifyScreen />} />
            <Route path="/confirm-email" element={<ConfirmEmailScreen />} />
            <Route element={<RequireAuth />}>
              <Route element={<AppShell />}>
                <Route index element={<Dashboard />} />
                <Route path="/pages/:id" element={<PageEditor />} />
                <Route path="/admin" element={<AdminScreen />} />
                <Route path="/settings" element={<SettingsScreen />} />
                <Route path="*" element={<NotFound />} />
              </Route>
            </Route>
          </Routes>
        </CatalogProvider>
      </AuthProvider>
    </BrowserRouter>
  );
}

function AuthGate({ children }: { children: (signedIn: boolean) => ReactNode }) {
  const { state, refresh } = useAuth();
  if (state.status === "loading") return <PageLoading />;
  if (state.status === "error") {
    return (
      <main className="auth-page">
        <div className="auth-card">
          <Notice
            tone="danger"
            title="We couldn't load your account"
            action={
              <button type="button" className="btn btn-sm" onClick={() => void refresh()}>
                Try again
              </button>
            }
          >
            {state.message}
          </Notice>
        </div>
      </main>
    );
  }
  return <>{children(state.user !== null)}</>;
}

function RequireAuth() {
  const location = useLocation();
  return (
    <AuthGate>
      {(signedIn) =>
        signedIn ? (
          <Outlet />
        ) : (
          <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
        )
      }
    </AuthGate>
  );
}

function GuestOnly() {
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from;
  return <AuthGate>{(signedIn) => (signedIn ? <Navigate to={from || "/"} replace /> : <Outlet />)}</AuthGate>;
}
