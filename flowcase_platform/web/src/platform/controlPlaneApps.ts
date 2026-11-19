export interface AppSummary {
  app_key: string;
  display_name: string;
  description: string;
  route_base: string;
  status: string;
  integration: string;
}
export function safeAppRoute(key: string, route: unknown): route is string {
  return /^[a-z][a-z0-9-]*$/.test(key) && route === `/apps/${key}/`;
}
/** CP cookie session only. Do not forward a Flowcase bearer token to a different audience. */
export async function controlPlaneGet<T>(
  path: string,
  signal?: AbortSignal,
): Promise<T> {
  if (!/^\/api\/v1\/apps(?:\/[a-z][a-z0-9-]*\/launch)?$/.test(path))
    throw new Error("Unsupported Control Plane route.");
  const response = await fetch(path, {
    credentials: "same-origin",
    cache: "no-store",
    headers: { Accept: "application/json" },
    ...(signal ? { signal } : {}),
  });
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const message = (body as { error?: { message?: string } } | null)?.error
      ?.message;
    throw new Error(
      message ?? `Control Plane request failed (${response.status}).`,
    );
  }
  return body as T;
}
