import { afterEach, describe, expect, it, vi } from "vitest";
import { apiGet, ApiError, requestCredentials } from "./client";
import { getToken, parseAllowedOrigins, setToken, startHostBridge } from "./hostBridge";

const JWT = "aaa.bbb.ccc";

function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });
}

afterEach(() => setToken(null));

describe("apiGet", () => {
  it("sends the token only as a bearer header, without cookies, and never in the URL", async () => {
    setToken(JWT);
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { ok: true }));
    await apiGet("/api/v1/session");
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/session");
    expect(url).not.toContain(JWT);
    expect((init.headers as Record<string, string>).Authorization).toBe(`Bearer ${JWT}`);
    expect((init.headers as Record<string, string>)["X-Request-ID"]).toMatch(/^[0-9a-f]{32}$/);
    expect(init.credentials).toBe("omit");
  });

  it("sends the shell session cookie only when mounted behind the Control Plane", () => {
    expect(requestCredentials("")).toBe("omit");
    expect(requestCredentials("/apps/flowcase")).toBe("same-origin");
  });

  it("works in insecure contexts where crypto.randomUUID is missing", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { ok: true }));
    const original = crypto.randomUUID;
    Object.defineProperty(crypto, "randomUUID", { value: undefined, configurable: true });
    try {
      await expect(apiGet("/api/v1/session")).resolves.toEqual({ ok: true });
    } finally {
      Object.defineProperty(crypto, "randomUUID", { value: original, configurable: true });
    }
  });

  it("parses the structured error envelope", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(401, { error: { code: "unauthenticated", message: "A verified host session is required.", trace_id: "trace-123" } }),
    );
    const err = await apiGet("/api/v1/session").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 401, code: "unauthenticated", traceId: "trace-123" });
  });

  it("maps network failures to a typed error", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("Failed to fetch"));
    await expect(apiGet("/api/v1/session")).rejects.toMatchObject({ status: 0, code: "network_error" });
  });

  it("refuses non-API paths", async () => {
    await expect(apiGet("https://evil.example/api/v1/session")).rejects.toThrow(/non-API/);
  });
});

describe("host bridge", () => {
  function fakeWindow() {
    const parent = { postMessage: vi.fn() };
    const handlers: ((e: MessageEvent) => void)[] = [];
    const win = {
      parent,
      addEventListener: (_: string, h: (e: MessageEvent) => void) => handlers.push(h),
      removeEventListener: vi.fn(),
    } as unknown as Window;
    const dispatch = (origin: string, data: unknown, source: unknown = parent) =>
      handlers.forEach((h) => h({ origin, data, source } as MessageEvent));
    return { win, parent, dispatch };
  }

  it("accepts tokens only from allowlisted parent origins", () => {
    const { win, parent, dispatch } = fakeWindow();
    startHostBridge(["https://workspace.example"], win);
    expect(parent.postMessage).toHaveBeenCalledWith({ type: "flowcase:ready" }, "https://workspace.example");

    dispatch("https://evil.example", { type: "flowcase:token", token: JWT });
    expect(getToken()).toBeNull();
    dispatch("https://workspace.example", { type: "flowcase:token", token: JWT }, {});
    expect(getToken()).toBeNull();
    dispatch("https://workspace.example", { type: "flowcase:token", token: "not a jwt" });
    expect(getToken()).toBeNull();

    dispatch("https://workspace.example", { type: "flowcase:token", token: JWT });
    expect(getToken()).toBe(JWT);
    dispatch("https://workspace.example", { type: "flowcase:logout" });
    expect(getToken()).toBeNull();
  });

  it("does nothing when not embedded or without an allowlist", () => {
    const { win, parent } = fakeWindow();
    startHostBridge([], win);
    expect(parent.postMessage).not.toHaveBeenCalled();
  });

  it("only allows https origins (http just for localhost)", () => {
    expect(parseAllowedOrigins("https://a.example, http://evil.example, http://localhost:3000, *, https://b.example/path")).toEqual([
      "https://a.example",
      "http://localhost:3000",
    ]);
  });
});
