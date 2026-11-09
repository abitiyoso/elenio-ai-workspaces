import { getPersona, getToken } from "./hostBridge";

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

// crypto.randomUUID exists only in secure contexts; getRandomValues also works over plain HTTP (tailnet dev VM).
function randomHex(bytes = 16): string {
  const b = crypto.getRandomValues(new Uint8Array(bytes));
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

/** A fresh Idempotency-Key for one user action (reuse it only to retry that same action). */
export function newIdempotencyKey(): string {
  return `ui-${randomHex()}`;
}

export interface RequestOptions {
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}

export interface ApiResponse<T> {
  data: T;
  etag: string | null;
}

/** Mount path of the app ("" standalone, "/apps/flowcase" behind the Control Plane). */
export const basePath = import.meta.env.BASE_URL.replace(/\/$/, "");

/**
 * Standalone, the bearer token lives in memory and no cookies are sent. Behind the Control Plane the browser
 * must send the shell's same-origin session cookie; the Control Plane strips it and injects the token.
 */
export function requestCredentials(base: string = basePath): RequestCredentials {
  return base ? "same-origin" : "omit";
}

/** Calls a Flowcase API route. The bearer token is sent only as a header, never in the URL. */
export async function apiRequest<T>(method: string, path: string, opts: RequestOptions = {}): Promise<ApiResponse<T>> {
  if (!path.startsWith("/api/v1/")) throw new Error(`refusing non-API path ${path}`);
  const headers: Record<string, string> = { Accept: "application/json", "X-Request-ID": randomHex(), ...opts.headers };
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  const persona = getPersona();
  if (persona !== "default") headers["X-Flowcase-Dev-Persona"] = persona;
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";

  let res: Response;
  try {
    res = await fetch(basePath + path, {
      method,
      headers,
      credentials: requestCredentials(),
      cache: "no-store",
      ...(opts.body !== undefined ? { body: JSON.stringify(opts.body) } : {}),
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") throw err;
    throw new ApiError(0, "network_error", "The Flowcase API could not be reached.");
  }
  const traceId = res.headers.get("X-Request-ID") ?? undefined;
  const body: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const e = (body as { error?: { code?: string; message?: string; trace_id?: string; details?: ErrorDetail[] } } | null)?.error;
    throw new ApiError(res.status, e?.code ?? "http_error", e?.message ?? `Request failed with status ${res.status}.`, e?.trace_id ?? traceId, e?.details ?? []);
  }
  return { data: body as T, etag: res.headers.get("ETag") };
}

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  return (await apiRequest<T>("GET", path, signal ? { signal } : {})).data;
}
