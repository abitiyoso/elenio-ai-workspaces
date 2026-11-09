// Same-origin client for the UCW BFF. The browser never holds a token: behind the Control Plane the
// shell's session cookie travels same-origin and the proxy injects the bearer token for the BFF.

export interface ErrorDetail {
  code: string;
  message: string;
  path?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly traceId: string | undefined;
  readonly details: ErrorDetail[];

  constructor(status: number, code: string, message: string, traceId?: string, details: ErrorDetail[] = []) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.traceId = traceId;
    this.details = details;
  }
}

export function asApiError(err: unknown): ApiError {
  return err instanceof ApiError ? err : new ApiError(0, "unknown", "Unexpected error.");
}

// crypto.randomUUID exists only in secure contexts; getRandomValues also works over plain HTTP (dev VM).
function randomHex(bytes = 16): string {
  const b = crypto.getRandomValues(new Uint8Array(bytes));
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

/** A fresh Idempotency-Key for one user action (reuse it only to retry that same action). */
export function newIdempotencyKey(): string {
  return `ucw-${randomHex()}`;
}

/** Mount path of the SPA: "" at the dev root, "/apps/case-workspace" behind the Control Plane. */
export const basePath = import.meta.env.BASE_URL.replace(/\/$/, "");

/** Root of the BFF API as the browser sees it (the Control Plane strips the mount path before the BFF). */
export const apiBase = `${basePath}/api/v1/workspace`;

export const ifMatch = (n: number | string) => ({ "If-Match": `"${n}"` });

export interface RequestOptions {
  body?: unknown;
  /** Multipart upload; the browser sets the Content-Type boundary. */
  form?: FormData;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}

export interface ApiResponse<T> {
  data: T;
  etag: string | null;
}

// 401 means the Control Plane session is gone; the shell listens and replaces the page with a reload hint.
type Listener = () => void;
const sessionListeners = new Set<Listener>();
export function onSessionEnded(l: Listener): () => void {
  sessionListeners.add(l);
  return () => {
    sessionListeners.delete(l);
  };
}

/** Calls a BFF route. `path` is relative to the workspace API root, e.g. "/cases". */
export async function apiRequest<T>(method: string, path: string, opts: RequestOptions = {}): Promise<ApiResponse<T>> {
  if (!path.startsWith("/") || path.includes("://") || path.includes("..")) throw new Error(`refusing unsafe API path ${path}`);
  const headers: Record<string, string> = { Accept: "application/json", "X-Request-ID": randomHex(), ...opts.headers };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";

  let res: Response;
  try {
    res = await fetch(apiBase + path, {
      method,
      headers,
      credentials: "same-origin",
      cache: "no-store",
      ...(opts.form ? { body: opts.form } : opts.body !== undefined ? { body: JSON.stringify(opts.body) } : {}),
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") throw err;
    throw new ApiError(0, "network_error", "The Case Workspace service could not be reached.");
  }
  const traceId = res.headers.get("X-Request-ID") ?? undefined;
  const body: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const e = (body as { error?: { code?: string; message?: string; trace_id?: string; details?: ErrorDetail[] } } | null)?.error;
    if (res.status === 401) sessionListeners.forEach((l) => l());
    throw new ApiError(res.status, e?.code ?? "http_error", e?.message ?? `Request failed with status ${res.status}.`, e?.trace_id ?? traceId, e?.details ?? []);
  }
  return { data: body as T, etag: res.headers.get("ETag") };
}

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  return (await apiRequest<T>("GET", path, signal ? { signal } : {})).data;
}

const serviceNames: Record<string, string> = {
  flowcase_unavailable: "Flowcase",
  agent_studio_unavailable: "Agent Studio",
  kms_unavailable: "KMS",
  registry_unavailable: "the pack registry",
};

/** A reader-facing explanation of an API error, by status (the server message stays for 403/422). */
export function describeError(e: ApiError): { title: string; message: string; reload: boolean } {
  switch (e.status) {
    case 0:
      return { title: "Connection problem", message: e.message, reload: true };
    case 401:
      return { title: "Your session ended", message: "Your session ended — reload from the Control Plane.", reload: false };
    case 403:
      return { title: "Not permitted", message: `${e.message} (${e.code})`, reload: false };
    case 404:
      return { title: "Not found", message: "This item does not exist or you do not have access to it.", reload: false };
    case 409:
      return { title: "Conflict", message: `${e.message} Reload to see the latest state.`, reload: true };
    case 412:
      return { title: "Changed meanwhile", message: "Someone changed this since you loaded it. Reload to see the latest version, then try again.", reload: true };
    case 503: {
      const svc = serviceNames[e.code];
      return { title: svc ? `${svc} unavailable` : "Service unavailable", message: svc ? `${svc} is unavailable right now. ${e.message}` : e.message, reload: true };
    }
    default:
      return { title: "Something went wrong", message: e.message, reload: true };
  }
}
