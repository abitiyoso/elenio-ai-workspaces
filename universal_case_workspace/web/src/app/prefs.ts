// Harmless UI preferences only (e.g. card/table view). Never identity, tokens or case data.
export function readPref(key: string, fallback: string): string {
  try {
    return window.localStorage.getItem(`ucw.pref.${key}`) ?? fallback;
  } catch {
    return fallback;
  }
}

export function writePref(key: string, value: string): void {
  try {
    window.localStorage.setItem(`ucw.pref.${key}`, value);
  } catch {
    // Storage may be unavailable (private mode, blocked site data); the preference just won't persist.
  }
}
