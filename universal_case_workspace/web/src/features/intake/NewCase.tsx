import { useCallback, useEffect, useRef, useState, type DragEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { ApiError, asApiError, newIdempotencyKey } from "../../api/client";
import {
  createCaseFromIntake, createIntakeSession, getIntakeSession, intakeTiming, removeIntakeDocument, saveIntakeDraft, sendIntakeMessage,
  uploadIntakeDocument, type IntakeDocument, type IntakeDraft, type IntakeField, type IntakeMessage, type IntakeSession, type IntakeUploads, type KBStatus,
} from "../../api/intake";
import type { CitedSource } from "../../api/types";
import { useResource } from "../../api/useResource";
import { listPacks } from "../../api/workspace";
import { Icon } from "../../components/Icon";
import { humanize } from "../../components/format";
import { CitedText } from "../../components/SourcePills";
import { indexSources, type SourceIndex } from "../../components/sources";
import { ErrorState, Loading, PageHead } from "../../components/States";
import { useToast } from "../../components/toastContext";
import { PackList } from "../cases/PackPicker";
import { startablePacks } from "../cases/startable";
import "./intake.css";

// Chat-first case intake, ported from Flowcase's NewCase page. UCW owns the session; the brief form, greeting and
// starters come from the pack's intake section via the session, so the same page serves every startable pack.

const kbPill: Record<KBStatus, [string, string]> = {
  covered: ["pill green", "KB: covered"],
  partial: ["pill amber", "KB: partial match"],
  not_covered: ["pill red", "KB: not covered"],
  unknown: ["pill grey", "KB: not checked yet"],
};

const maxUserTurns = 30;

type BriefForm = Record<string, string>;

function formFromDraft(fields: IntakeField[], d: IntakeDraft): BriefForm {
  const out: BriefForm = {};
  for (const f of fields) {
    const v = d[f.key];
    out[f.key] = Array.isArray(v) ? v.join("\n") : v ?? "";
  }
  return out;
}

function draftFromForm(fields: IntakeField[], f: BriefForm): IntakeDraft {
  const out: IntakeDraft = {};
  for (const field of fields) {
    const raw = f[field.key] ?? "";
    if (field.widget === "list") {
      const list = raw.split("\n").map((c) => c.trim()).filter(Boolean);
      if (list.length) out[field.key] = list;
    } else if (raw.trim()) {
      out[field.key] = raw.trim();
    }
  }
  return out;
}

/** Labels still missing once unsaved form edits are taken into account. */
function stillMissing(missing: string[], fields: IntakeField[], form: BriefForm | null): string[] {
  if (!form) return missing;
  return missing.filter((label) => {
    const key = fields.find((f) => f.label === label)?.key;
    return !key || !(form[key] ?? "").trim();
  });
}

function withMessageSources(base: SourceIndex, sources: CitedSource[] = []): SourceIndex {
  const m = new Map(base);
  for (const s of sources) if (!m.has(s.source_id.toLowerCase())) { m.set(s.source_id, s); m.set(s.source_id.toLowerCase(), s); }
  return m;
}

function MessageItem({ m, index, labels }: { m: IntakeMessage; index: SourceIndex; labels: Map<string, string> }) {
  if (m.role === "user") {
    return (
      <li className="intake-msg user">
        <span className="sr-only">You: </span>
        {m.document && <div className="intake-attached"><Icon name="paperclip" size="sm" /> {m.document.filename}</div>}
        <div className="intake-text">{m.content}</div>
      </li>
    );
  }
  const idx = withMessageSources(index, m.sources);
  return (
    <li className="intake-msg assistant">
      <span className="sr-only">Assistant: </span>
      <div className="intake-text"><CitedText text={m.content} index={idx} /></div>
      {m.updated_fields && m.updated_fields.length > 0 && (
        <div className="intake-meta">
          <span className="intake-meta-label">Draft updated:</span>
          {m.updated_fields.map((f) => <span key={f} className="intake-tag">{labels.get(f) ?? humanize(f)}</span>)}
        </div>
      )}
      {m.unresolved && m.unresolved.length > 0 && (
        <div className="intake-meta intake-unresolved">
          <span className="intake-meta-label">Not confirmed by the knowledge base:</span>
          <ul>{m.unresolved.map((u, i) => <li key={i}><strong>{u.topic}</strong>{u.reason && ` — ${u.reason}`}</li>)}</ul>
        </div>
      )}
      {m.sources && m.sources.length > 0 && (
        <div className="intake-meta intake-sources">
          <span className="intake-meta-label">Sources:</span>
          <ul>{m.sources.map((s) => <li key={s.source_id} title={s.citation || undefined}><Icon name="file" size="sm" /> {idx.get(s.source_id)?.title ?? s.title}</li>)}</ul>
        </div>
      )}
    </li>
  );
}

function formatSize(bytes: number): string {
  return bytes >= 1 << 20 ? `${(bytes / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

/** Client-side check before uploading; the server validates again by content. */
function uploadProblem(file: File, uploads: IntakeUploads): string | null {
  const ext = file.name.includes(".") ? file.name.slice(file.name.lastIndexOf(".")).toLowerCase() : "";
  if (!uploads.accept.includes(ext)) return `${file.name}: upload a ${uploads.accept.map((a) => a.slice(1).toUpperCase()).join(", ")} file.`;
  if (file.size === 0) return `${file.name} is empty.`;
  if (file.size > uploads.max_bytes) return `${file.name} is larger than ${formatSize(uploads.max_bytes)}.`;
  return null;
}

function DocumentList({ docs, onRemove, removing, locked }: {
  docs: IntakeDocument[]; onRemove: (d: IntakeDocument) => void; removing: string | null; locked: boolean;
}) {
  return (
    <ul className="intake-docs" aria-label="Uploaded documents">
      {docs.map((d) => (
        <li key={d.id} className={`intake-doc ${d.status}`}>
          <Icon name="file" size="sm" />
          <span className="intake-doc-name" title={d.filename}>{d.filename}</span>
          <span className="intake-doc-status" role={d.status === "processing" ? "status" : undefined}>
            {d.status === "processing" && <><span className="intake-dots" aria-hidden="true"><i /><i /><i /></span> Reading{d.media_type === "application/pdf" ? " (OCR for scans)" : ""}…</>}
            {d.status === "ready" && `${formatSize(d.size_bytes)} · ${(d.chars ?? 0).toLocaleString("en")} characters read`}
            {d.status === "failed" && (d.error ?? "Could not be read.")}
          </span>
          <button type="button" className="intake-doc-remove" aria-label={`Remove ${d.filename}`}
            disabled={d.status === "processing" || removing === d.id || (locked && d.status === "ready")} onClick={() => onRemove(d)}>
            <Icon name="x" size="sm" />
          </button>
        </li>
      ))}
    </ul>
  );
}

function BriefFields({ fields, form, onChange, disabled }: {
  fields: IntakeField[]; form: BriefForm; onChange: (k: string, v: string) => void; disabled: boolean;
}) {
  return (
    <>
      {fields.map((f) => {
        const id = `brief-${f.key}`;
        const helpId = `${id}-help`;
        const help = f.widget === "list" ? [f.help, "One item per line."].filter(Boolean).join(" ") : f.help;
        const common = {
          id, className: f.widget === "textarea" || f.widget === "list" ? "textarea" : "input", value: form[f.key] ?? "", disabled,
          "aria-describedby": help ? helpId : undefined, "aria-required": f.required || undefined,
        };
        return (
          <div className="field" key={f.key}>
            <label className="label" htmlFor={id}>{f.label}{f.required && <span className="req" aria-hidden="true"> *</span>}</label>
            {f.widget === "textarea" || f.widget === "list" ? (
              <textarea {...common} maxLength={f.widget === "list" ? 10000 : 20000} onChange={(e) => onChange(f.key, e.target.value)} />
            ) : (
              <input {...common} type={f.widget === "date" ? "date" : "text"} maxLength={f.widget === "date" ? 10 : 300} onChange={(e) => onChange(f.key, e.target.value)} />
            )}
            {help && <p className="hint" id={helpId}>{help}</p>}
          </div>
        );
      })}
    </>
  );
}

function PackChooser() {
  const { res, reload } = useResource("packs", listPacks);
  const navigate = useNavigate();
  const packs = res.state === "ready" ? startablePacks(res.data.items) : [];
  const only = packs.length === 1 ? packs[0] : undefined;
  useEffect(() => {
    if (only) navigate(`/cases/new?pack=${encodeURIComponent(only.pack_key)}`, { replace: true });
  }, [only, navigate]);
  return (
    <>
      <PageHead eyebrow="NEW CASE" title="Start a new case" subtitle="Pick the domain pack. The intake assistant prepares the case brief with you." />
      {res.state === "loading" && <Loading label="Loading domain packs…" />}
      {res.state === "error" && <ErrorState error={res.error} onRetry={reload} />}
      {res.state === "ready" && !only && <div className="card pad narrow"><PackList packs={packs} /></div>}
    </>
  );
}

export function NewCasePage() {
  const [params] = useSearchParams();
  if (!params.get("pack") && !params.get("session")) return <PackChooser />;
  return <IntakeChat />;
}

function IntakeChat() {
  const [params, setParams] = useSearchParams();
  const sessionParam = params.get("session");
  const packParam = params.get("pack") ?? "";
  const navigate = useNavigate();
  const toast = useToast();
  const [session, setSession] = useState<IntakeSession | null>(null);
  const [fatal, setFatal] = useState<ApiError | null>(null);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState<string | null>(null);
  const [edits, setEdits] = useState<BriefForm | null>(null);
  const [saving, setSaving] = useState(false);
  const [briefError, setBriefError] = useState<ApiError | null>(null);
  const [creating, setCreating] = useState(false);
  const [confirmUnready, setConfirmUnready] = useState(false);
  const [createError, setCreateError] = useState<ApiError | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const [dragging, setDragging] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const opening = useRef(false);
  const held = useRef<string | null>(null);
  const listRef = useRef<HTMLOListElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const createKey = useRef<string | undefined>(undefined);

  // The URL names the conversation. `held` is the session shown (or being opened); router updates can land a
  // render after setSession and must not open a second session (StrictMode runs effects twice in development).
  const adopt = useCallback((s: IntakeSession) => {
    held.current = s.id;
    setSession(s);
    setParams({ pack: s.pack_key, session: s.id }, { replace: true });
  }, [setParams]);

  useEffect(() => {
    if (opening.current || (sessionParam && sessionParam === held.current)) return;
    if (!sessionParam && held.current) {
      setParams({ pack: packParam, session: held.current }, { replace: true });
      return;
    }
    opening.current = true;
    const load = sessionParam ? getIntakeSession(sessionParam) : createIntakeSession(packParam);
    void load.then(adopt, (err) => setFatal(asApiError(err))).finally(() => { opening.current = false; });
  }, [sessionParam, packParam, setParams, adopt]);

  // Poll while the assistant answers or a document is read: every fetched session schedules the next poll.
  useEffect(() => {
    if (!session?.pending && !session?.documents?.some((d) => d.status === "processing")) return;
    const id = session.id;
    const ctrl = new AbortController();
    const timer = window.setTimeout(() => {
      getIntakeSession(id, ctrl.signal).then(setSession, (err) => {
        if (!(err instanceof DOMException)) setSendError(asApiError(err).message);
      });
    }, intakeTiming.pollMs);
    return () => { window.clearTimeout(timer); ctrl.abort(); };
  }, [session]);

  const messageCount = session?.messages.length ?? 0;
  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messageCount, session?.pending]);

  const send = async (text: string) => {
    const content = text.trim();
    if (!session || !content || sending || session.pending) return;
    setSending(true);
    setSendError(null);
    try {
      setSession(await sendIntakeMessage(session.id, content));
      setInput("");
    } catch (err) {
      const e = asApiError(err);
      setSendError(e.message);
      // The message is saved even when the assistant could not start; show it with the server's error.
      getIntakeSession(session.id).then((s) => { setSession(s); if (s.last_error) setSendError(null); }, () => undefined);
    } finally {
      setSending(false);
      inputRef.current?.focus({ preventScroll: true });
    }
  };

  const upload = async (file: File) => {
    if (!session?.uploads) return;
    const problem = uploadProblem(file, session.uploads);
    setUploadError(problem);
    if (problem) return;
    setUploading(true);
    try {
      setSession(await uploadIntakeDocument(session.id, file));
    } catch (err) {
      setUploadError(asApiError(err).message);
    } finally {
      setUploading(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  };

  const removeDocument = async (d: IntakeDocument) => {
    if (!session) return;
    setRemoving(d.id);
    setUploadError(null);
    try {
      setSession(await removeIntakeDocument(session.id, d.id));
      setEdits(null);
    } catch (err) {
      setUploadError(asApiError(err).message);
    } finally {
      setRemoving(null);
    }
  };

  const newConversation = async () => {
    const pack = session?.pack_key ?? packParam;
    setSession(null);
    setEdits(null);
    setInput("");
    setSendError(null);
    setBriefError(null);
    setCreateError(null);
    setUploadError(null);
    setConfirmUnready(false);
    createKey.current = undefined;
    opening.current = true;
    await createIntakeSession(pack).then(adopt, (err) => setFatal(asApiError(err))).finally(() => { opening.current = false; });
  };

  const saveBrief = async (): Promise<IntakeSession | null> => {
    if (!session || !edits) return session;
    setSaving(true);
    setBriefError(null);
    try {
      const s = await saveIntakeDraft(session.id, session.version_num, draftFromForm(session.fields, edits));
      setSession(s);
      setEdits(null);
      return s;
    } catch (err) {
      const e = asApiError(err);
      if (e.status === 412) {
        getIntakeSession(session.id).then(setSession, () => undefined);
        setBriefError(new ApiError(412, e.code, "The assistant updated the brief meanwhile. Your edits are kept: review them and save again."));
      } else {
        setBriefError(e);
      }
      return null;
    } finally {
      setSaving(false);
    }
  };

  const create = async (confirmed: boolean) => {
    if (!session) return;
    setCreateError(null);
    let current: IntakeSession | null = session;
    if (edits) current = await saveBrief();
    if (!current) return;
    if (!current.ready && !confirmed) {
      setConfirmUnready(true);
      return;
    }
    setConfirmUnready(false);
    setCreating(true);
    createKey.current ??= newIdempotencyKey();
    try {
      const c = await createCaseFromIntake(current.id, current.version_num, createKey.current);
      toast(`${c.reference} created from the intake chat`);
      navigate(`/cases/${encodeURIComponent(c.case_id)}`);
    } catch (err) {
      const e = asApiError(err);
      if (e.status === 412) {
        getIntakeSession(current.id).then(setSession, () => undefined);
        setCreateError(new ApiError(412, e.code, "The brief changed meanwhile. Review the latest version and click Create case again."));
        createKey.current = undefined;
      } else {
        setCreateError(e);
      }
      setCreating(false);
    }
  };

  if (fatal) {
    return (
      <>
        <PageHead eyebrow="NEW CASE" title="New case" subtitle="Chat with the case intake assistant to prepare the case brief." />
        <ErrorState error={fatal} onRetry={() => { setFatal(null); void newConversation(); }} />
        <p className="hint"><Link to="/">Back to My Cases</Link></p>
      </>
    );
  }
  if (!session) return <Loading label="Opening the case intake assistant…" />;

  const closed = session.status !== "open";
  const fields = session.fields ?? [];
  const labels = new Map(fields.map((f) => [f.key, f.label]));
  const sourceIndex = indexSources(session.sources ?? []);
  const userTurns = session.messages.filter((m) => m.role === "user").length;
  const last = session.messages[session.messages.length - 1];
  const showGreeting = !!session.greeting && session.messages[0]?.content !== session.greeting;
  const quickReplies = !session.pending && !closed && last?.role === "assistant" ? last.quick_replies ?? [] : [];
  const failedText = session.last_error && last?.role === "user" ? last.content : null;
  const form = edits ?? formFromDraft(fields, session.draft ?? {});
  const missing = stillMissing(session.missing ?? [], fields, edits);
  const [kbClass, kbText] = kbPill[session.kb_status] ?? kbPill.unknown;
  const docs = session.documents ?? [];
  const reading = docs.some((d) => d.status === "processing");
  const busy = sending || session.pending || reading || uploading;
  const uploads = session.uploads ?? null;
  const activeDocs = docs.filter((d) => d.status !== "failed").length;
  const canUpload = !!uploads && !closed && !busy && activeDocs < uploads.max_files && userTurns < maxUserTurns;
  const dropProps = uploads && !closed ? {
    onDragOver: (e: DragEvent<HTMLElement>) => { if (e.dataTransfer.types.includes("Files")) { e.preventDefault(); setDragging(true); } },
    onDragLeave: (e: DragEvent<HTMLElement>) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false); },
    onDrop: (e: DragEvent<HTMLElement>) => {
      e.preventDefault();
      setDragging(false);
      const file = e.dataTransfer.files[0];
      if (!file) return;
      if (!canUpload) {
        setUploadError(reading || uploading ? "Wait until the current document has been read." : busy ? "Wait for the assistant to finish." : "No more documents can be added to this conversation.");
        return;
      }
      void upload(file);
    },
  } : {};
  const editField = (k: string, v: string) => setEdits({ ...form, [k]: v });

  return (
    <>
      <PageHead eyebrow="NEW CASE" title="Prepare the case brief"
        subtitle={<>Describe the need; the assistant checks the knowledge base and fills the brief for <strong>{session.pack_key}</strong>{session.pack_version ? ` v${session.pack_version}` : ""}. Nothing is created until you click Create case.</>}
        actions={<>
          <Link className="btn" to="/"><Icon name="arrow-left" size="sm" /> My Cases</Link>
          <button className="btn" type="button" onClick={() => void newConversation()} disabled={sending}><Icon name="refresh" size="sm" /> New conversation</button>
        </>} />
      <ol className="intake-steps" aria-label="Case creation steps">
        <li className={userTurns > 0 ? "done" : "current"}><span>1</span> Describe the need</li>
        <li className={session.ready ? "done" : userTurns > 0 ? "current" : ""}><span>2</span> Complete the brief</li>
        <li className={closed ? "done" : session.ready ? "current" : ""}><span>3</span> Create the case</li>
      </ol>
      <div className="intake-layout">
        <section className={`card intake-chat${dragging ? " dragging" : ""}`} aria-label="Chat with the case intake assistant" {...dropProps}>
          {dragging && <div className="intake-drop" aria-hidden="true"><Icon name="paperclip" /> Drop the document to upload it</div>}
          <ol className="intake-messages" ref={listRef} role="log" aria-live="polite" aria-relevant="additions" aria-label="Conversation">
            {showGreeting && (
              <li className="intake-msg assistant"><span className="sr-only">Assistant: </span><div className="intake-text">{session.greeting}</div></li>
            )}
            {session.messages.map((m, i) => <MessageItem key={i} m={m} index={sourceIndex} labels={labels} />)}
            {session.pending && (
              <li className="intake-msg assistant intake-thinking" role="status">
                <span className="intake-dots" aria-hidden="true"><i /><i /><i /></span> Assistant is thinking…
              </li>
            )}
          </ol>
          <div className="intake-composer-wrap">
            {userTurns === 0 && !busy && !closed && (session.starters?.length ?? 0) > 0 && (
              <div className="intake-chips" role="group" aria-label="Conversation starters">
                {session.starters?.map((s) => <button key={s} type="button" className="intake-chip" onClick={() => void send(s)}>{s}</button>)}
              </div>
            )}
            {quickReplies.length > 0 && (
              <div className="intake-chips" role="group" aria-label="Suggested replies">
                {quickReplies.map((q) => <button key={q} type="button" className="intake-chip" onClick={() => void send(q)}>{q}</button>)}
              </div>
            )}
            {docs.length > 0 && <DocumentList docs={docs} onRemove={(d) => void removeDocument(d)} removing={removing} locked={session.pending} />}
            {uploads?.truncated && <p className="hint intake-hint">The documents are longer than the brief allows; only their beginning is used.</p>}
            {uploadError && <div className="notice error intake-error" role="alert">{uploadError}</div>}
            {(session.last_error || sendError) && !session.pending && (
              <div className="notice error intake-error" role="alert">
                {session.last_error ?? sendError}
                {failedText && <button type="button" className="btn sm" onClick={() => void send(failedText)} disabled={sending}><Icon name="refresh" size="sm" /> Retry</button>}
              </div>
            )}
            {closed ? (
              <div className="notice good">
                <strong>Case created.</strong> {session.case_id && <Link to={`/cases/${encodeURIComponent(session.case_id)}`}>Open {session.case_reference ?? "the case"}</Link>}
              </div>
            ) : (
              <form className="intake-composer" onSubmit={(e) => { e.preventDefault(); void send(input); }}>
                {uploads && (
                  <>
                    <input ref={fileRef} type="file" className="sr-only" tabIndex={-1} aria-hidden="true" accept={uploads.accept.join(",")}
                      data-testid="intake-file" onChange={(e) => { const f = e.target.files?.[0]; if (f) void upload(f); }} />
                    <button type="button" className="btn intake-attach" disabled={!canUpload} onClick={() => fileRef.current?.click()}
                      title={`Attach a KAK/RFP (${uploads.accept.map((a) => a.slice(1).toUpperCase()).join(", ")}, max ${formatSize(uploads.max_bytes)})`}
                      aria-label="Attach a document">
                      <Icon name="paperclip" size="sm" />{uploading ? " Uploading…" : ""}
                    </button>
                  </>
                )}
                <label className="sr-only" htmlFor="intake-input">Message to the assistant</label>
                <textarea id="intake-input" ref={inputRef} className="textarea" rows={2} maxLength={4000} value={input}
                  placeholder={uploads ? "Describe the need, or attach the KAK/RFP document…" : "Describe the customer, the need, scope and target date…"}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                      e.preventDefault();
                      void send(input);
                    }
                  }} />
                <button className="btn primary" type="submit" disabled={busy || !input.trim() || userTurns >= maxUserTurns}>
                  <Icon name="send" size="sm" /> {sending ? "Sending…" : "Send"}
                </button>
              </form>
            )}
            {!closed && <p className="hint intake-hint">Enter sends, Shift+Enter adds a line.{uploads && ` Attach or drop a KAK/RFP (${uploads.accept.map((a) => a.slice(1).toUpperCase()).join(", ")}, up to ${uploads.max_files} files).`} {userTurns}/{maxUserTurns} messages.</p>}
          </div>
        </section>

        <aside className="intake-aside" aria-label="Case brief status">
          <section className="card pad">
            <div className="intake-status" aria-live="polite">
              <span className={session.ready ? "pill green" : "pill amber"}>{session.ready ? "Ready to create" : "Needs more info"}</span>
              <span className={kbClass}>{kbText}</span>
            </div>
            <h2 className="intake-h">Matched in the knowledge base</h2>
            {(session.kb_items ?? []).length === 0 ? <p className="hint">No knowledge-base match yet.</p> : (
              <ul className="intake-kb">
                {session.kb_items.map((k, i) => {
                  const titles = (k.evidence_refs ?? []).map((r) => sourceIndex.get(r)?.title).filter(Boolean);
                  return (
                    <li key={i}>
                      <strong>{k.name}</strong> {k.kind && <span className="intake-kind">{humanize(k.kind)}</span>}
                      {titles.length > 0 && <div className="tiny muted">{titles.join(" · ")}</div>}
                    </li>
                  );
                })}
              </ul>
            )}
            <h2 className="intake-h">Still needed</h2>
            {missing.length === 0 ? <p className="hint">Every required field is filled.</p> : (
              <ul className="intake-missing" aria-label="Still needed fields">{missing.map((m) => <li key={m}>{m}</li>)}</ul>
            )}
            {confirmUnready && (
              <div className="notice warning intake-confirm" role="alert">
                The assistant has not confirmed this brief as ready. Create the case anyway?
                <div className="actions">
                  <button className="btn sm primary" type="button" onClick={() => void create(true)} disabled={creating}>Create anyway</button>
                  <button className="btn sm" type="button" onClick={() => setConfirmUnready(false)}>Keep chatting</button>
                </div>
              </div>
            )}
            {createError && (
              <div className="notice error intake-confirm" role="alert">
                {createError.status === 403 ? `${createError.message} (${createError.code})` : createError.message}
                {createError.details.length > 0 && <ul>{createError.details.map((d, i) => <li key={i}>{d.path && <code>{d.path}</code>} {d.message}</li>)}</ul>}
              </div>
            )}
            <button className="btn primary intake-create" type="button" disabled={closed || creating || saving || busy || missing.length > 0}
              onClick={() => void create(false)}>
              <Icon name="briefcase" size="sm" /> {creating ? "Creating…" : "Create case"}
            </button>
            {busy && !closed && <p className="hint">{reading || uploading ? "Wait until the document has been read before creating the case." : "Wait for the assistant to finish before creating the case."}</p>}
          </section>

          <details className="card intake-brief" open>
            <summary><Icon name="file" size="sm" /> Edit brief {edits && <span className="pill amber">unsaved</span>}</summary>
            <form className="intake-brief-body" onSubmit={(e) => { e.preventDefault(); void saveBrief(); }}>
              {fields.length === 0 ? <p className="hint">This pack declares no brief fields.</p> : <BriefFields fields={fields} form={form} onChange={editField} disabled={closed} />}
              {briefError && (
                <div className="notice error intake-confirm" role="alert">
                  {briefError.message}
                  {briefError.details.length > 0 && <ul>{briefError.details.map((d, i) => <li key={i}>{d.message}</li>)}</ul>}
                </div>
              )}
              {!closed && fields.length > 0 && (
                <div className="actions intake-brief-actions">
                  <button className="btn" type="submit" disabled={!edits || saving}><Icon name="save" size="sm" /> {saving ? "Saving…" : "Save brief"}</button>
                  {edits && <button className="btn sm" type="button" onClick={() => { setEdits(null); setBriefError(null); }}>Discard edits</button>}
                </div>
              )}
              <p className="hint">The chat and this form edit the same draft. Tell the assistant about scope changes so it can re-check the knowledge base.</p>
            </form>
          </details>
        </aside>
      </div>
    </>
  );
}
