import { useCallback, useEffect, useRef, useState } from "react";
import { asApiError, type ApiError } from "./client";

export type Resource<T> =
  | { state: "loading" }
  | { state: "error"; error: ApiError }
  | { state: "ready"; data: T; refreshing: boolean };

/**
 * Loads a resource and reloads it when `key` changes. `reload()` keeps the current data on screen while
 * refreshing (silent polling), `set()` replaces it with a server response from a mutation.
 */
export function useResource<T>(key: string, load: (signal: AbortSignal) => Promise<T>) {
  const [res, setRes] = useState<Resource<T>>({ state: "loading" });
  const [tick, setTick] = useState(0);
  const loader = useRef(load);
  const lastKey = useRef(key);
  useEffect(() => {
    loader.current = load;
  });

  useEffect(() => {
    const ctrl = new AbortController();
    const keyChanged = lastKey.current !== key;
    lastKey.current = key;
    setRes((r) => (r.state === "ready" && !keyChanged ? { ...r, refreshing: true } : { state: "loading" }));
    loader.current(ctrl.signal).then(
      (data) => setRes({ state: "ready", data, refreshing: false }),
      (err: unknown) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setRes((r) => (r.state === "ready" && !keyChanged && asApiError(err).status !== 401
          ? { ...r, refreshing: false }
          : { state: "error", error: asApiError(err) }));
      },
    );
    return () => ctrl.abort();
  }, [key, tick]);

  const reload = useCallback(() => setTick((t) => t + 1), []);
  const set = useCallback((data: T) => setRes({ state: "ready", data, refreshing: false }), []);
  return { res, reload, set };
}
