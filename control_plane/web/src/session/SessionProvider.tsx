import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { apiGet, apiPost, setCsrfToken, toApiError, type ApiRequestError } from "../api/client";
import { browser } from "../api/browser";
import { api } from "../api/endpoints";
import type { Me } from "../api/types";
import { EpochContext, SessionContext, type SessionValue } from "./context";
import { ErrorState } from "../components/ui";
import { Icon } from "../components/Icons";

type State = { status: "loading" } | { status: "error"; error: ApiRequestError } | { status: "ready"; me: Me };

export function SessionProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<State>({ status: "loading" });
  const [epoch, setEpoch] = useState(0);
  const [attempt, setAttempt] = useState(0);

  const fetchMe = useCallback(
    () =>
      apiGet<Me>(api.me).then((me) => {
        setCsrfToken(me.csrf_token);
        return me;
      }),
    [],
  );

  useEffect(() => {
    let cancelled = false;
    fetchMe().then(
      (me) => {
        if (!cancelled) setState({ status: "ready", me });
      },
      (err: unknown) => {
        if (!cancelled) setState({ status: "error", error: toApiError(err) });
      },
    );
    return () => {
      cancelled = true;
    };
  }, [fetchMe, attempt]);

  const refresh = useCallback(async () => {
    const me = await fetchMe();
    setState({ status: "ready", me });
    setEpoch((e) => e + 1);
  }, [fetchMe]);

  const switchWorkspace = useCallback(
    async (workspaceId: string) => {
      await apiPost<void>(api.sessionWorkspace, { workspace_id: workspaceId });
      await refresh();
    },
    [refresh],
  );

  const signOut = useCallback(async () => {
    const res = await apiPost<{ redirect: string }>(api.logout);
    setCsrfToken("");
    const target = typeof res?.redirect === "string" && res.redirect ? res.redirect : "/signed-out";
    browser.assign(target);
  }, []);

  const value = useMemo<SessionValue | null>(
    () => (state.status === "ready" ? { me: state.me, epoch, refresh, switchWorkspace, signOut } : null),
    [state, epoch, refresh, switchWorkspace, signOut],
  );

  if (state.status === "loading") {
    return (
      <div className="boot" role="status" aria-live="polite">
        <div className="brandmark" aria-hidden="true">
          <Icon name="layers" size="lg" />
        </div>
        <p>Loading your workspace…</p>
      </div>
    );
  }
  if (state.status === "error") {
    if (state.error.status === 401) {
      return (
        <div className="boot" role="status">
          <p>Redirecting to sign in…</p>
        </div>
      );
    }
    return (
      <div className="boot">
        <ErrorState
          title="Could not load your session"
          error={state.error}
          onRetry={() => {
            setState({ status: "loading" });
            setAttempt((a) => a + 1);
          }}
        />
      </div>
    );
  }

  return (
    <SessionContext.Provider value={value}>
      <EpochContext.Provider value={epoch}>{children}</EpochContext.Provider>
    </SessionContext.Provider>
  );
}
