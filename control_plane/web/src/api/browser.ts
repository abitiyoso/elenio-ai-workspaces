// Thin wrapper over full-page navigation so tests can observe it without touching jsdom's
// non-configurable window.location. Every navigation the shell performs goes through here.
export const browser = {
  assign(url: string): void {
    window.location.assign(url);
  },
  currentPath(): string {
    return window.location.pathname + window.location.search;
  },
};

/** Same-origin relative path guard for `return_to` (the server re-validates). */
export function safeReturnTo(path: string): string {
  return path.startsWith("/") && !path.startsWith("//") && !path.startsWith("/\\") ? path : "/";
}

export function loginUrl(returnTo: string = browser.currentPath()): string {
  return `/auth/login?return_to=${encodeURIComponent(safeReturnTo(returnTo))}`;
}

export function redirectToLogin(): void {
  browser.assign(loginUrl());
}
