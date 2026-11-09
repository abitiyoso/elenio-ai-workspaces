import { useCallback, useContext, useEffect, useState } from "react";
import { apiGet, toApiError, type ApiRequestError } from "../api/client";
import { EpochContext } from "../session/context";

export interface ApiState<T> {
  data: T | undefined;
  error: ApiRequestError | undefined;
  loading: boolean;
  reload: () => void;
}

interface Stored<T> {
  key: string | null;
  path: string | null;
  data?: T;
  error?: ApiRequestError;
}

/**
 * GET a JSON resource. Pass `null` to skip (e.g. when the caller lacks a capability).
 * Refetches when the path, the session epoch (workspace switch) or `reload()` changes.
 * Keeps showing previous data for the same path while revalidating.
 */
export function useApi<T>(path: string | null): ApiState<T> {
  const epoch = useContext(EpochContext);
  const [nonce, setNonce] = useState(0);
  const key = path === null ? null : `${epoch}:${nonce}:${path}`;
  const [stored, setStored] = useState<Stored<T>>({ key: null, path: null });

  useEffect(() => {
    if (path === null || key === null) return;
    const ctrl = new AbortController();
    apiGet<T>(path, ctrl.signal).then(
      (data) => {
        if (!ctrl.signal.aborted) setStored({ key, path, data });
      },
      (err: unknown) => {
        if (!ctrl.signal.aborted) setStored({ key, path, error: toApiError(err) });
      },
    );
    return () => ctrl.abort();
  }, [key, path]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  const samePath = stored.path === path;
  const fresh = stored.key === key;
  return {
    data: samePath ? stored.data : undefined,
    error: fresh ? stored.error : undefined,
    loading: path !== null && !fresh,
    reload,
  };
}
