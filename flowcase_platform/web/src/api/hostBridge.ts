// Receives the short-lived host token from the parent AI Workspace via postMessage.
// PENDING host contract (docs/CONTRACTS.md): message shape {type:"flowcase:token", token}.
// The token lives only in memory: never in localStorage, cookies or URLs.

type Listener = () => void;

let currentToken: string | null = null;
const listeners = new Set<Listener>();

export function getToken(): string | null {
  return currentToken;
}

export function setToken(token: string | null): void {
  currentToken = token;
  listeners.forEach((l) => l());
}

// Development persona (dev_mock only): the server maps the key to a configured identity.
let currentPersona = "default";

export function getPersona(): string {
  return currentPersona;
}

export function setPersona(key: string): void {
  currentPersona = key;
  listeners.forEach((l) => l());
}

/** Fires when the token or dev persona changes, so identity-dependent data reloads. */
export function onTokenChange(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

const compactJWS = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/;

export function parseAllowedOrigins(raw: string | undefined): string[] {
  return (raw ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter((s) => /^https:\/\/[^/]+$/.test(s) || /^http:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/.test(s));
}

/**
 * Starts listening for host tokens. Messages are accepted only from an allowlisted origin and only
 * from the parent window. Returns a stop function.
 */
export function startHostBridge(allowedOrigins: string[], win: Window = window): () => void {
  if (allowedOrigins.length === 0 || win.parent === win) return () => undefined;

  const handler = (event: MessageEvent) => {
    if (!allowedOrigins.includes(event.origin) || event.source !== win.parent) return;
    const data: unknown = event.data;
    if (typeof data !== "object" || data === null) return;
    const msg = data as { type?: unknown; token?: unknown };
    if (msg.type === "flowcase:token" && typeof msg.token === "string" && compactJWS.test(msg.token) && msg.token.length <= 8192) {
      setToken(msg.token);
    } else if (msg.type === "flowcase:logout") {
      setToken(null);
    }
  };
  win.addEventListener("message", handler);
  for (const origin of allowedOrigins) {
    win.parent.postMessage({ type: "flowcase:ready" }, origin);
  }
  return () => win.removeEventListener("message", handler);
}
