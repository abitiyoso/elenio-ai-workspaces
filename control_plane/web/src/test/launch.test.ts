import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { isSafeRouteBase, openLaunchRoute, requestLaunch } from "../api/launch";
import { browser } from "../api/browser";
import { resetAuthRedirect } from "../api/client";
import { jsonResponse } from "./fixtures";

describe("app launch", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    resetAuthRedirect();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("accepts only /apps/<key>/ route bases", () => {
    expect(isSafeRouteBase("/apps/kms/")).toBe(true);
    expect(isSafeRouteBase("/apps/agent-studio/")).toBe(true);
    for (const bad of [
      "https://evil.example/apps/kms/",
      "//evil.example/apps/kms/",
      "/apps/kms",
      "/apps/../admin/",
      "/apps/KMS/",
      "/apps/ab/",
      "/apps/kms/extra/",
      "javascript:alert(1)",
      "/apps/kms/?x=1",
    ]) {
      expect(isSafeRouteBase(bad), bad).toBe(false);
    }
  });

  it("refuses to navigate when the server returns a non-matching route_base", async () => {
    const assign = vi.spyOn(browser, "assign").mockImplementation(() => {});
    fetchMock.mockResolvedValue(
      jsonResponse(200, { app_key: "kms", route_base: "https://evil.example/", integration: "proxy" }),
    );
    await expect(requestLaunch("kms")).rejects.toMatchObject({ code: "LAUNCH_ROUTE_INVALID" });
    expect(openLaunchRoute({ app_key: "kms", route_base: "//evil.example/", integration: "proxy" })).toBe(false);
    expect(assign).not.toHaveBeenCalled();
  });

  it("navigates to a valid route_base", async () => {
    const assign = vi.spyOn(browser, "assign").mockImplementation(() => {});
    fetchMock.mockResolvedValue(jsonResponse(200, { app_key: "kms", route_base: "/apps/kms/", integration: "proxy" }));
    const result = await requestLaunch("kms");
    expect(fetchMock.mock.calls[0]![0]).toBe("/api/v1/apps/kms/launch");
    expect(openLaunchRoute(result)).toBe(true);
    expect(assign).toHaveBeenCalledWith("/apps/kms/");
  });

  it("propagates a 403 reason code from the server", async () => {
    fetchMock.mockResolvedValue(jsonResponse(403, { code: "APP_DISABLED", message: "disabled", trace_id: "t-1" }));
    await expect(requestLaunch("kms")).rejects.toMatchObject({ status: 403, code: "APP_DISABLED", trace_id: "t-1" });
  });
});
