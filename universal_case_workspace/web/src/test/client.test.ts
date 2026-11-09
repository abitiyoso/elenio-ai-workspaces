import { describe, expect, it, vi } from "vitest";
import { ApiError, apiBase, apiRequest, describeError, onSessionEnded } from "../api/client";

describe("api client", () => {
  it("derives the API root from the Vite base path", () => {
    expect(import.meta.env.BASE_URL).toBe("/");
    expect(apiBase).toBe("/api/v1/workspace");
  });

  it("sends same-origin credentials, never an Authorization header, and parses errors", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ error: { code: "draft_not_pending", message: "Already decided.", trace_id: "tr-1", details: [] } }), { status: 409 }));
    const err = await apiRequest("POST", "/cases/x/drafts/y/submit", { body: {} }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).code).toBe("draft_not_pending");
    expect((err as ApiError).traceId).toBe("tr-1");
    const init = spy.mock.calls[0]?.[1] as RequestInit;
    expect(init.credentials).toBe("same-origin");
    expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
    expect(describeError(err as ApiError).message).toMatch(/Reload to see the latest state/);
  });

  it("announces an ended session on 401", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", { status: 401 }));
    const ended = vi.fn();
    const off = onSessionEnded(ended);
    await apiRequest("GET", "/cases").catch(() => undefined);
    off();
    expect(ended).toHaveBeenCalledOnce();
  });

  it("names the unavailable service on 503", () => {
    expect(describeError(new ApiError(503, "kms_unavailable", "down")).title).toBe("KMS unavailable");
    expect(describeError(new ApiError(403, "not_case_owner", "Only the case owner can review drafts.")).message).toBe("Only the case owner can review drafts. (not_case_owner)");
  });

  it("refuses unsafe paths", async () => {
    await expect(apiRequest("GET", "https://evil.example/x")).rejects.toThrow(/unsafe/);
  });
});
