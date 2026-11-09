import type { ApiError, ValidationIssue } from "./types";
import { redirectToLogin } from "./browser";

/**
 * Error thrown for any non-2xx response (or network failure). Always carries the server's
 * `ApiError` fields so the UI can show code + message + trace id.
 */
export class ApiRequestError extends Error implements ApiError {
  readonly status: number;
  readonly code: string;
  readonly trace_id: string;
  readonly issues: ValidationIssue[];

  constructor(status: number, body: ApiError & { issues?: ValidationIssue[] }) {
    super(body.message);
    this.name = "ApiRequestError";
    this.status = status;
    this.code = body.code;
    this.trace_id = body.trace_id;
    this.issues = Array.isArray(body.issues) ? body.issues : [];
  }
}

export function toApiError(err: unknown): ApiRequestError {
  if (err instanceof ApiRequestError) return err;
  const message = err instanceof Error ? err.message : "Unexpected error";
  return new ApiRequestError(0, { code: "NETWORK_ERROR", message, trace_id: "" });
}

// The CSRF token lives in memory only (from GET /api/v1/me); never persisted.
let csrfToken = "";
export function setCsrfToken(token: string): void {
  csrfToken = token;
}
export function getCsrfToken(): string {
  return csrfToken;
}

export type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";

export interface RequestOptions {
  method?: HttpMethod;
  body?: unknown;
  /** Registry lifecycle optimistic concurrency: sent as `If-Match: "<revision>"`. */
  ifMatch?: number;
  signal?: AbortSignal;
}

export interface ApiResponse<T> {
  status: number;
  data: T;
}

let redirecting = false;
/** Test hook: reset the one-shot 401 redirect latch. */
export function resetAuthRedirect(): void {
  redirecting = false;
}

export async function apiRequest<T>(path: string, opts: RequestOptions = {}): Promise<ApiResponse<T>> {
  const method = opts.method ?? "GET";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (method !== "GET") {
    headers["X-CSRF-Token"] = csrfToken;
    headers["Content-Type"] = "application/json";
  }
  if (opts.ifMatch !== undefined) {
    headers["If-Match"] = `"${opts.ifMatch}"`;
  }

  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      credentials: "same-origin",
      cache: "no-store",
      redirect: "error",
      body: method !== "GET" && opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      signal: opts.signal,
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") throw err;
    throw new ApiRequestError(0, {
      code: "NETWORK_ERROR",
      message: "Could not reach the control plane. Check your connection and try again.",
      trace_id: "",
    });
  }

  const traceHeader = res.headers.get("X-Request-ID") ?? "";

  if (res.status === 401) {
    if (!redirecting) {
      redirecting = true;
      redirectToLogin();
    }
    throw new ApiRequestError(401, {
      code: "UNAUTHENTICATED",
      message: "Your session has ended. Redirecting to sign in…",
      trace_id: traceHeader,
    });
  }

  if (res.status === 204) {
    return { status: 204, data: undefined as T };
  }

  const text = await res.text();
  let parsed: unknown = undefined;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = undefined;
    }
  }

  if (!res.ok) {
    const body = (parsed ?? {}) as Partial<ApiError> & { issues?: ValidationIssue[] };
    throw new ApiRequestError(res.status, {
      code: typeof body.code === "string" ? body.code : `HTTP_${res.status}`,
      message: typeof body.message === "string" ? body.message : res.statusText || "Request failed",
      trace_id: typeof body.trace_id === "string" && body.trace_id ? body.trace_id : traceHeader,
      issues: body.issues,
    });
  }

  return { status: res.status, data: parsed as T };
}

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  return (await apiRequest<T>(path, { signal })).data;
}

export async function apiPost<T>(path: string, body?: unknown, ifMatch?: number): Promise<T> {
  return (await apiRequest<T>(path, { method: "POST", body: body ?? {}, ifMatch })).data;
}

export async function apiPut<T>(path: string, body: unknown): Promise<ApiResponse<T>> {
  return apiRequest<T>(path, { method: "PUT", body });
}

export async function apiDelete(path: string, body: unknown): Promise<void> {
  await apiRequest<void>(path, { method: "DELETE", body });
}

/** Encode a single path segment (ids, keys) — never interpolate raw values into URLs. */
export const seg = (v: string): string => encodeURIComponent(v);

