import { apiGet, ApiRequestError, seg } from "./client";
import { browser } from "./browser";
import type { LaunchResult } from "./types";

export const ROUTE_BASE_PATTERN = /^\/apps\/[a-z][a-z0-9-]{2,63}\/$/;

export function isSafeRouteBase(routeBase: string): boolean {
  return ROUTE_BASE_PATTERN.test(routeBase);
}

/**
 * Ask the server whether the caller may open the app, and validate the returned route.
 * Throws ApiRequestError (403 with reason code, or LAUNCH_ROUTE_INVALID) on refusal.
 */
export async function requestLaunch(appKey: string): Promise<LaunchResult> {
  const result = await apiGet<LaunchResult>(`/api/v1/apps/${seg(appKey)}/launch`);
  if (!result || typeof result.route_base !== "string" || !isSafeRouteBase(result.route_base)) {
    throw new ApiRequestError(0, {
      code: "LAUNCH_ROUTE_INVALID",
      message: "The server returned a launch route outside /apps/<key>/. The launch was blocked.",
      trace_id: "",
    });
  }
  return result;
}

/** Navigate to a launch route. Re-validates so no caller can bypass the pattern check. */
export function openLaunchRoute(result: LaunchResult): boolean {
  if (!isSafeRouteBase(result.route_base)) return false;
  browser.assign(result.route_base);
  return true;
}
