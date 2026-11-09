import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { intakeTiming, type IntakeSession } from "../api/intake";
import { bodyOf, headersOf, me, mockApi, renderAt } from "./fixtures";

const id = "55555555-5555-4555-8555-555555555555";
const greeting = "Hi! Tell me about the case you want to open.";
const fields = [
  { key: "title", label: "Case title", widget: "text", required: true },
  { key: "customer_name", label: "Customer name", widget: "text", required: true },
  { key: "deadline", label: "Target date", widget: "date", required: true },
  { key: "summary", label: "Case brief", widget: "textarea" },
  { key: "constraints", label: "Constraints", widget: "list", help: "Hard limits the solution must respect." },
];
const base = (over: Partial<IntakeSession> = {}): IntakeSession => ({
  id, pack_key: "demo.pack", pack_version: "1.1.0", workflow_key: "wf", status: "open", fields, greeting,
  starters: ["We need a customer service chatbot", "Summarise my RFP"], draft: {}, messages: [],
  kb_status: "unknown", kb_items: [], sources: [], ready: false, missing: ["Case title", "Customer name", "Target date"],
  pending: false, last_error: null, case_id: null, case_reference: null, turn: 0, version_num: 1, ...over,
});
const userMsg = { role: "user" as const, content: "We need a customer service chatbot", at: "2026-10-31T09:01:00Z" };
const sid = "9cea8321-b363-4d27-8b0d-fa967570c011";
const reply = {
  role: "assistant" as const, content: `Noted: a chatbot, matching the catalogue [${sid}].`, at: "2026-10-31T09:01:05Z",
  updated_fields: ["title", "customer_name"], quick_replies: ["Target is end of January", "Customer is PT Contoh"],
  sources: [{ source_id: sid, title: "Solution catalogue 2026" }],
  kb_items: [{ name: "Customer Service Bot", kind: "solution", evidence_refs: [sid] }],
};
const pending = base({ messages: [userMsg], pending: true, turn: 1, version_num: 2 });
const answered = base({
  messages: [userMsg, reply], draft: { title: "Chatbot", customer_name: "PT Contoh" }, kb_status: "partial",
  kb_items: reply.kb_items, sources: reply.sources, missing: ["Target date"], turn: 1, version_num: 3,
});

beforeEach(() => { intakeTiming.pollMs = 10; });
afterEach(() => { intakeTiming.pollMs = 2000; });

describe("Intake chat", () => {
  it("opens one session under StrictMode, shows greeting, form fields from the session, and polls a pending turn to its reply", async () => {
    let polls = 0;
    const calls = mockApi({
      "/me/context": [200, me],
      "POST /intake-sessions": [201, base()],
      [`GET /intake-sessions/${id}`]: () => [200, ++polls < 2 ? pending : answered],
      [`POST /intake-sessions/${id}/messages`]: [202, pending],
    });
    const user = userEvent.setup();
    renderAt("/cases/new?pack=demo.pack");
    expect(await screen.findByText(greeting)).toBeInTheDocument();
    expect(calls.filter((c) => c.method === "POST" && c.url.endsWith("/intake-sessions"))).toHaveLength(1);
    expect(bodyOf(calls.find((c) => c.method === "POST"))).toEqual({ pack_key: "demo.pack" });
    // Brief form fields come from session.fields.
    expect(screen.getByLabelText(/Case title/)).toHaveAttribute("type", "text");
    expect(screen.getByLabelText(/Target date/)).toHaveAttribute("type", "date");
    expect(screen.getByLabelText(/Constraints/).tagName).toBe("TEXTAREA");
    expect(screen.getByText(/One item per line/)).toBeInTheDocument();

    await user.type(screen.getByLabelText("Message to the assistant"), "We need a customer service chatbot");
    await user.click(screen.getByRole("button", { name: /Send/ }));
    expect(bodyOf(calls.find((c) => c.url.endsWith("/messages")))).toEqual({ content: "We need a customer service chatbot" });
    expect(await screen.findByText(/Assistant is thinking/)).toBeInTheDocument();

    const text = await screen.findByText(/Noted: a chatbot/);
    expect(text.textContent).not.toContain(sid);
    expect(text.querySelector(".source-pill")?.textContent).toBe("Solution catalogue 2026");
    expect(screen.queryByText(/Assistant is thinking/)).not.toBeInTheDocument();
    expect(screen.getByRole("log", { name: "Conversation" })).toHaveAttribute("aria-live", "polite");
    expect(screen.getByText("Draft updated:")).toBeInTheDocument();
    expect(screen.getAllByText("Case title").length).toBeGreaterThan(1);
    expect(screen.getByText("KB: partial match")).toBeInTheDocument();
    expect(screen.getByText("Customer Service Bot")).toBeInTheDocument();
    expect(within(screen.getByRole("list", { name: "Still needed fields" })).getByText("Target date")).toBeInTheDocument();
    expect(screen.getByLabelText(/Case title/)).toHaveValue("Chatbot");
  });

  it("sends a quick reply as the next message", async () => {
    const calls = mockApi({
      "/me/context": [200, me],
      [`GET /intake-sessions/${id}`]: [200, answered],
      [`POST /intake-sessions/${id}/messages`]: [202, base({ ...answered, messages: [...answered.messages, { role: "user", content: "Target is end of January" }], pending: true })],
    });
    const user = userEvent.setup();
    renderAt(`/cases/new?pack=demo.pack&session=${id}`);
    const group = await screen.findByRole("group", { name: "Suggested replies" });
    await user.click(within(group).getByRole("button", { name: "Target is end of January" }));
    await waitFor(() => expect(bodyOf(calls.find((c) => c.url.endsWith("/messages")))).toEqual({ content: "Target is end of January" }));
    expect(calls.some((c) => c.method === "POST" && c.url.endsWith("/intake-sessions"))).toBe(false);
  });

  it("creates the case with If-Match and Idempotency-Key and opens it", async () => {
    const ready = base({ ...answered, ready: true, missing: [], draft: { title: "Chatbot", customer_name: "PT Contoh", deadline: "2027-01-31" }, version_num: 7 });
    const calls = mockApi({
      "/me/context": [200, me],
      [`GET /intake-sessions/${id}`]: [200, ready],
      [`POST /intake-sessions/${id}/case`]: [201, { case_id: "case-9", reference: "CASE-2026-0009" }],
      "/cases/case-9": [503, { error: { code: "flowcase_unavailable", message: "later", trace_id: "t" } }],
    });
    const user = userEvent.setup();
    renderAt(`/cases/new?session=${id}`);
    await user.click(await screen.findByRole("button", { name: /Create case/ }));
    await waitFor(() => expect(calls.some((c) => c.url.endsWith("/cases/case-9"))).toBe(true));
    const create = calls.find((c) => c.url.endsWith("/case"));
    expect(headersOf(create)["If-Match"]).toBe('"7"');
    expect(headersOf(create)["Idempotency-Key"]).toMatch(/^ucw-[0-9a-f]{32}$/);
    expect(create?.init.credentials).toBe("same-origin");
    expect(await screen.findByText("Flowcase unavailable")).toBeInTheDocument();
  });

  it("asks before creating a case the assistant has not marked ready", async () => {
    const notReady = base({ ...answered, missing: [], ready: false });
    const calls = mockApi({
      "/me/context": [200, me],
      [`GET /intake-sessions/${id}`]: [200, notReady],
      [`POST /intake-sessions/${id}/case`]: [201, { case_id: "case-9", reference: "CASE-2026-0009" }],
    });
    const user = userEvent.setup();
    renderAt(`/cases/new?session=${id}`);
    await user.click(await screen.findByRole("button", { name: /Create case/ }));
    expect(await screen.findByText(/Create the case anyway/)).toBeInTheDocument();
    expect(calls.some((c) => c.url.endsWith("/case"))).toBe(false);
    await user.click(screen.getByRole("button", { name: "Create anyway" }));
    await waitFor(() => expect(calls.some((c) => c.url.endsWith("/case"))).toBe(true));
  });

  it("keeps edits and explains when the brief changed meanwhile (412)", async () => {
    mockApi({
      "/me/context": [200, me],
      [`GET /intake-sessions/${id}`]: [200, answered],
      [`PUT /intake-sessions/${id}/draft`]: [412, { error: { code: "version_mismatch", message: "stale", trace_id: "t" } }],
    });
    const user = userEvent.setup();
    renderAt(`/cases/new?session=${id}`);
    const title = await screen.findByLabelText(/Case title/);
    await user.clear(title);
    await user.type(title, "Edited title");
    await user.click(screen.getByRole("button", { name: /Save brief/ }));
    expect(await screen.findByText(/updated the brief meanwhile/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Case title/)).toHaveValue("Edited title");
  });

  it("offers retry when the assistant could not answer", async () => {
    const failed = base({ messages: [userMsg], last_error: "Agent Studio is unavailable.", turn: 1 });
    const calls = mockApi({
      "/me/context": [200, me],
      [`GET /intake-sessions/${id}`]: [200, failed],
      [`POST /intake-sessions/${id}/messages`]: [202, pending],
    });
    const user = userEvent.setup();
    renderAt(`/cases/new?session=${id}`);
    expect(await screen.findByText("Agent Studio is unavailable.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Retry/ }));
    await waitFor(() => expect(bodyOf(calls.find((c) => c.url.endsWith("/messages")))).toEqual({ content: userMsg.content }));
  });
  it("uploads a KAK document, shows it being read and sends it to the assistant", async () => {
    const uploads = { max_files: 3, max_bytes: 15 * 1024 * 1024, accept: [".pdf", ".docx", ".txt", ".md"], truncated: false };
    const doc = { id: "d1", filename: "RKST Server CCTV.pdf", media_type: "application/pdf", size_bytes: 4_300_000, chars: null, error: null };
    const open = base({ uploads, documents: [] });
    const reading = base({ uploads, documents: [{ ...doc, status: "processing" as const }], version_num: 2 });
    const sent = { role: "user" as const, content: "Saya mengunggah dokumen \"RKST Server CCTV.pdf\".", document: { id: "d1", filename: doc.filename } };
    const read = base({
      uploads, documents: [{ ...doc, status: "ready" as const, chars: 18_900 }], messages: [sent], pending: true, turn: 1, version_num: 4,
      draft: { summary: "## RKST" },
    });
    let polls = 0;
    const calls = mockApi({
      "/me/context": [200, me],
      [`GET /intake-sessions/${id}`]: () => [200, ++polls === 1 ? open : polls === 2 ? reading : read],
      [`POST /intake-sessions/${id}/documents`]: [202, reading],
    });
    const user = userEvent.setup();
    renderAt(`/cases/new?session=${id}`);
    const attach = await screen.findByRole("button", { name: "Attach a document" });
    expect(attach).toBeEnabled();
    const file = new File(["%PDF-1.7"], "RKST Server CCTV.pdf", { type: "application/pdf" });
    await user.upload(screen.getByTestId("intake-file"), file);
    const up = calls.find((c) => c.url.endsWith("/documents"));
    expect(up?.method).toBe("POST");
    expect((up?.init.body as FormData).get("file")).toBeInstanceOf(File);
    expect(headersOf(up)["Content-Type"]).toBeUndefined();
    expect(await screen.findByText(/Reading \(OCR for scans\)/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Send/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Remove RKST Server CCTV.pdf" })).toBeDisabled();
    // polling picks up the read document and the message sent for it
    expect(await screen.findByText(/18,900 characters read/)).toBeInTheDocument();
    const log = screen.getByRole("log", { name: "Conversation" });
    expect(within(log).getByText("RKST Server CCTV.pdf")).toBeInTheDocument();
    expect(within(log).getByText(/Saya mengunggah dokumen/)).toBeInTheDocument();
  });

  it("rejects unsupported files before uploading", async () => {
    const uploads = { max_files: 3, max_bytes: 1024, accept: [".pdf", ".docx", ".txt", ".md"], truncated: false };
    const calls = mockApi({ "/me/context": [200, me], [`GET /intake-sessions/${id}`]: [200, base({ uploads, documents: [] })] });
    const user = userEvent.setup({ applyAccept: false });
    renderAt(`/cases/new?session=${id}`);
    await screen.findByRole("button", { name: "Attach a document" });
    await user.upload(screen.getByTestId("intake-file"), new File(["MZ"], "setup.exe"));
    expect(await screen.findByText(/setup.exe: upload a PDF, DOCX, TXT, MD file/)).toBeInTheDocument();
    await user.upload(screen.getByTestId("intake-file"), new File(["x".repeat(2048)], "big.pdf"));
    expect(await screen.findByText(/big.pdf is larger than 1 KB/)).toBeInTheDocument();
    expect(calls.some((c) => c.url.endsWith("/documents"))).toBe(false);
  });

  it("has no attach button when the pack does not accept documents", async () => {
    mockApi({ "/me/context": [200, me], [`GET /intake-sessions/${id}`]: [200, base()] });
    renderAt(`/cases/new?session=${id}`);
    await screen.findByText(greeting);
    expect(screen.queryByRole("button", { name: "Attach a document" })).not.toBeInTheDocument();
  });
});
