import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { ApiError, api, type User } from "./api";

type AuthState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; user: User | null };

interface AuthContextValue {
  state: AuthState;
  user: User | null;
  setUser: (user: User | null) => void;
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: "loading" });

  const refresh = useCallback(async () => {
    try {
      const user = await api.me();
      setState({ status: "ready", user });
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setState({ status: "ready", user: null });
      } else {
        setState({
          status: "error",
          message: err instanceof Error ? err.message : "Can't reach the server.",
        });
      }
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const setUser = useCallback((user: User | null) => setState({ status: "ready", user }), []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setState({ status: "ready", user: null });
    }
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      state,
      user: state.status === "ready" ? state.user : null,
      setUser,
      refresh,
      logout,
    }),
    [state, setUser, refresh, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside AuthProvider");
  return ctx;
}

/** Use only below the auth guard, where a user is guaranteed. */
export function useUser(): User {
  const { user } = useAuth();
  if (!user) throw new Error("useUser called without a signed-in user");
  return user;
}
