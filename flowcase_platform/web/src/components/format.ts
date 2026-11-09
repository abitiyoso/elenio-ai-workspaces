export function formatDate(iso: string | null): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function shortHash(hash: string | null): string {
  return hash ? `${hash.slice(0, 10)}…` : "—";
}
