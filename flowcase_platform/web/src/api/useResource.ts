import { useCallback, useEffect, useState } from "react";
import { apiGet, ApiError } from "./client";
import { onTokenChange } from "./hostBridge";

export type Resource<T> =
  | { state: "loading" }
  | { state: "error"; error: ApiError }
  | { state: "ready"; data: T };

const loading = { state: "loading" } as const;

/** Loads a GET resource, re-fetching when the host token changes. */
export function useResource<T>(path: string): [Resource<T>, () => void] {
  const [nonce, setNonce] = useState(0);
  const reload = useCallback(() => setNonce((n) => n + 1), []);
  // Results are tagged with the request they answer, so a new path or reload reads as loading
  // without resetting state inside the effect.
  const requestKey = `${path}#${nonce}`;
  const [result, setResult] = useState<{ key: string; value: Resource<T> } | null>(null);

  useEffect(() => onTokenChange(reload), [reload]);

  useEffect(() => {
    const ctrl = new AbortController();
    apiGet<T>(path, ctrl.signal).then(
      (data) => setResult({ key: requestKey, value: { state: "ready", data } }),
      (err: unknown) => {
        if (ctrl.signal.aborted) return;
        const error = err instanceof ApiError ? err : new ApiError(0, "unknown", "Unexpected error.");
        setResult({ key: requestKey, value: { state: "error", error } });
      },
    );
    return () => ctrl.abort();
  }, [path, requestKey]);

  return [result?.key === requestKey ? result.value : loading, reload];
}
