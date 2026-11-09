import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost, apiRequest, ApiRequestError, resetAuthRedirect, setCsrfToken } from "../api/client";
import { browser } from "../api/browser";
import { jsonResponse } from "./fixtures";

describe("api client", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    setCsrfToken("csrf-123");
    resetAuthRedirect();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    setCsrfToken("");
  });

  function lastInit(): RequestInit & { headers: Record<string, string> } {
    return fetchMock.mock.calls.at(-1)![1] as RequestInit & { headers: Record<string, string> };
  }

  it("sends CSRF token, JSON content type and same-origin credentials on non-GET", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { ok: true }));
    await apiPost("/api/v1/session/workspace", { workspace_id: "ws-1" });
    const init = lastInit();
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("same-origin");
    expect(init.headers["X-CSRF-Token"]).toBe("csrf-123");
    expect(init.headers["Content-Type"]).toBe("application/json");
    expect(init.body).toBe(JSON.stringify({ workspace_id: "ws-1" }));
    expect(init.headers["If-Match"]).toBeUndefined();
  });

  it("does not send the CSRF token on GET but still uses same-origin credentials", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { items: [] }));
    await apiGet("/api/v1/apps");
    const init = lastInit();
    expect(init.method).toBe("GET");
    expect(init.credentials).toBe("same-origin");
    expect(init.headers["X-CSRF-Token"]).toBeUndefined();
  });

  it("sends a quoted If-Match revision for registry lifecycle calls", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, {}));
    await apiPost("/api/v1/admin/apps/kms/publish", { version: "1.0.0", reason: "ship it" }, 7);
    const init = lastInit();
    expect(init.headers["If-Match"]).toBe('"7"');
    expect(init.headers["X-CSRF-Token"]).toBe("csrf-123");
  });

  it("navigates to /auth/login with the current path on 401", async () => {
    window.history.pushState({}, "", "/people?q=ada lovelace");
    const assign = vi.spyOn(browser, "assign").mockImplementation(() => {});
    fetchMock.mockResolvedValue(jsonResponse(401, { code: "UNAUTHENTICATED", message: "no session", trace_id: "t1" }));
    await expect(apiGet("/api/v1/me")).rejects.toMatchObject({ status: 401 });
    expect(assign).toHaveBeenCalledTimes(1);
    expect(assign).toHaveBeenCalledWith(`/auth/login?return_to=${encodeURIComponent("/people?q=ada%20lovelace")}`);
    window.history.pushState({}, "", "/");
  });

  it("surfaces ApiError code, message and trace id (falling back to X-Request-ID)", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(403, { code: "NOT_MEMBER", message: "not a member", trace_id: "tr-9" }));
    const err = await apiRequest("/api/v1/session/workspace", { method: "POST", body: {} }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiRequestError);
    expect(err).toMatchObject({ status: 403, code: "NOT_MEMBER", message: "not a member", trace_id: "tr-9" });

    fetchMock.mockResolvedValueOnce(new Response("<html>bad gateway</html>", { status: 502, headers: { "X-Request-ID": "rid-1" } }));
    const err2 = await apiGet("/api/v1/apps").catch((e: unknown) => e);
    expect(err2).toMatchObject({ status: 502, code: "HTTP_502", trace_id: "rid-1" });
  });

  it("parses 422 validation issues", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(422, {
        code: "MANIFEST_INVALID",
        message: "invalid",
        trace_id: "t",
        issues: [{ field: "manifest.app_key", code: "pattern", message: "bad key" }],
      }),
    );
    const err = (await apiPost("/api/v1/admin/apps", {}).catch((e: unknown) => e)) as ApiRequestError;
    expect(err.issues).toHaveLength(1);
    expect(err.issues[0]!.field).toBe("manifest.app_key");
  });

  it("returns undefined for 204", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    await expect(apiPost("/api/v1/session/workspace", { workspace_id: "x" })).resolves.toBeUndefined();
  });
});
