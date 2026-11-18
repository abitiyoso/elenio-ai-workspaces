import { describe, it, expect, vi, afterEach } from "vitest";
import { ApiError } from "../api/client";
import { setToken } from "../api/hostBridge";
import {
  canEditVersion,
  createDefinition,
  isSession,
  isRuntimeCapabilities,
  unsupportedNodes,
  isValidationReport,
  assertSaveResult,
} from "./connectedApi";
import { definitionDiff } from "./definitionDiff";
import { vendorPayment } from "./fixtures";
import { safeAppRoute, controlPlaneGet } from "./controlPlaneApps";

afterEach(() => {
  setToken(null);
  vi.restoreAllMocks();
});
describe("connected contract boundaries", () => {
  it("rejects malformed validation and version responses before replacing the editor document", () => {
    expect(isValidationReport({ valid: true, diagnostics: [] })).toBe(true);
    expect(isValidationReport({ valid: false, diagnostics: [null] })).toBe(
      false,
    );
    expect(
      isValidationReport({
        valid: false,
        diagnostics: [{ severity: "error" }],
      }),
    ).toBe(false);
    expect(() =>
      assertSaveResult({
        version: { status: "draft" },
        validation: { valid: true, diagnostics: [] },
      }),
    ).toThrow("invalid draft response");
  });
  it("creates an imported definition through the actual API with a stable idempotency key and no browser tenant/role claims", async () => {
    setToken("test-header-only-token");
    const fetch = vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(
          JSON.stringify({
            version: {
              id: "v1",
              workflow_key: "payments",
              version: 1,
              version_num: 1,
              status: "draft",
              definition: { ...vendorPayment, key: "payments" },
            },
            validation: { valid: true, diagnostics: [] },
          }),
          { status: 201 },
        ),
    );
    const input = {
      key: "payments",
      name: "Payments",
      definition: vendorPayment,
    };
    await createDefinition(input, "one-action-key");
    await createDefinition(input, "one-action-key");
    const opts = fetch.mock.calls[0]?.[1];
    expect(fetch.mock.calls[0]?.[0]).toBe("/api/v1/workflows");
    expect(opts?.method).toBe("POST");
    expect(JSON.parse(String(opts?.body))).toEqual(input);
    expect(opts?.headers).toMatchObject({
      "Idempotency-Key": "one-action-key",
      Authorization: "Bearer test-header-only-token",
    });
    expect(
      Object.keys(opts?.headers ?? {}).some((k) => /tenant|role/i.test(k)),
    ).toBe(false);
    expect(fetch.mock.calls[1]?.[1]?.headers).toMatchObject({
      "Idempotency-Key": "one-action-key",
    });
  });
  it("propagates forbidden service responses rather than manufacturing a local workflow", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: "forbidden", message: "Denied" } }),
        { status: 403 },
      ),
    );
    await expect(
      createDefinition({ key: "payments", name: "Payments" }),
    ).rejects.toMatchObject({
      status: 403,
      code: "forbidden",
    } satisfies Partial<ApiError>);
  });
  it("allows only designers to edit draft/validated versions and never published/deprecated data", () => {
    for (const state of ["published", "deprecated", "unknown"])
      expect(canEditVersion(state, true)).toBe(false);
    expect(canEditVersion("draft", false)).toBe(false);
    expect(canEditVersion("draft", true)).toBe(true);
    expect(canEditVersion("validated", true)).toBe(true);
  });
  it("fails closed for invalid sessions or malformed capabilities", () => {
    expect(
      isSession({
        user_id: "u",
        tenant_id: "t",
        display_name: "User",
        roles: ["workflow_designer"],
        auth_mode: "dev_mock",
        environment: "test",
      }),
    ).toBe(true);
    for (const data of [
      null,
      {},
      { roles: ["workflow_designer"] },
      { user_id: "u", tenant_id: "t", roles: "tenant_admin" },
    ])
      expect(isSession(data)).toBe(false);
    expect(
      isRuntimeCapabilities({ supported_node_types: ["start", "end"] }),
    ).toBe(true);
    expect(isRuntimeCapabilities({ supported_node_types: "all" })).toBe(false);
    expect(isRuntimeCapabilities(null)).toBe(false);
    expect(
      unsupportedNodes(vendorPayment, {
        supported_node_types: ["start", "end"],
      }),
    ).toContain("extract");
  });
  it("previews semantic removals, additions, policy and schema changes without mutating either definition", () => {
    const changed = structuredClone(vendorPayment);
    changed.nodes = changed.nodes.filter((n) => n.id !== "exception");
    changed.nodes.push({ id: "new", type: "end", name: "New end", next: [] });
    changed.policy_refs = ["new-policy@2"];
    changed.input_schema = { type: "object" };
    const before = JSON.stringify(vendorPayment);
    const diff = definitionDiff(vendorPayment, changed);
    expect(diff.added).toEqual(["new"]);
    expect(diff.removed).toEqual(["exception"]);
    expect(diff.fields).toEqual(
      expect.arrayContaining(["policy_refs", "input_schema"]),
    );
    expect(JSON.stringify(vendorPayment)).toBe(before);
  });
});

describe("Control Plane app boundary", () => {
  it("accepts only the exact registered same-origin app route", () => {
    expect(safeAppRoute("kms", "/apps/kms/")).toBe(true);
    for (const route of [
      "https://example.test/apps/kms/",
      "//example.test/apps/kms/",
      "javascript:alert(1)",
      "/apps/kms/../../",
      "/apps/other/",
      "/apps/kms/?token=secret",
    ])
      expect(safeAppRoute("kms", route)).toBe(false);
  });
  it("uses only the CP cookie session and never forwards the Flowcase bearer audience", async () => {
    setToken("flowcase-scope-token");
    const request = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(JSON.stringify({ items: [] })));
    await controlPlaneGet("/api/v1/apps");
    expect(request.mock.calls[0]?.[1]?.credentials).toBe("same-origin");
    expect(request.mock.calls[0]?.[1]?.headers).toEqual({
      Accept: "application/json",
    });
    await expect(controlPlaneGet("/api/v1/admin/users")).rejects.toThrow(
      "Unsupported Control Plane route",
    );
  });
});
