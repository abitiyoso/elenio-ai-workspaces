import { FormEvent, ReactNode, useCallback, useEffect, useState } from 'react';
import { Agent, AgentVersion, EvidenceRef, FeedbackClassification, Run, RunEvent, RunValidation, Session, api } from '../api';

export type LoadState = 'loading' | 'ready' | 'error';

/** Read-only wide tables need a keyboard scroll target, including Safari. */
export function TableScroll({label, children}: {label: string; children: ReactNode}) {
  return <div className="table-scroll" role="region" aria-label={label} tabIndex={0}>{children}</div>;
}

export function errorText(error: unknown) {
  return error instanceof Error ? error.message : 'Permintaan gagal. Coba lagi.';
}

export function statusLabel(value: string) {
  return value.replaceAll('_', ' ');
}

export function formatDate(value?: string | null) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

export function runDuration(run: Pick<Run, 'started_at' | 'completed_at'>) {
  if (!run.started_at || !run.completed_at) return '—';
  const ms = new Date(run.completed_at).getTime() - new Date(run.started_at).getTime();
  if (!Number.isFinite(ms) || ms < 0) return '—';
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
}

export const activeRunStatuses = ['queued', 'running'];

/** Session permissions are a UI hint only; the API stays the authority. */
export function can(session: Session | null, permission: string) {
  if (!session?.permissions) return true;
  return session.permissions.includes(permission);
}

export function PermissionNote({ session, permission, action }: { session: Session | null; permission: string; action: string }) {
  if (can(session, permission)) return null;
  return <p className="form-hint permission-note" role="note">{action} tidak tersedia: sesi ini tidak memiliki izin <code>{permission}</code>.</p>;
}

export function parseJsonObject(text: string, label: string): Record<string, unknown> {
  let parsed: unknown;
  try { parsed = JSON.parse(text || '{}'); } catch { throw new Error(`${label}: JSON tidak valid.`); }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(`${label}: harus berupa JSON object.`);
  return parsed as Record<string, unknown>;
}

export function parseJsonArray(text: string, label: string): unknown[] {
  let parsed: unknown;
  try { parsed = JSON.parse(text || '[]'); } catch { throw new Error(`${label}: JSON tidak valid.`); }
  if (!Array.isArray(parsed)) throw new Error(`${label}: harus berupa JSON array.`);
  return parsed;
}

export function newIdempotencyKey(prefix: string) {
  const random = typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID().slice(0, 8) : Math.random().toString(36).slice(2, 10);
  return `${prefix}:${Date.now().toString(36)}-${random}`;
}

export function RecordsState({ state, message, empty, count = 0 }: { state: LoadState | 'idle'; message?: string; empty: string; count?: number }) {
  if (state === 'ready' && count > 0) return null;
  if (state === 'loading') return <div className="records-state" role="status">Memuat dari API…</div>;
  if (state === 'error') return <div className="records-state error-state" role="alert">{message || 'Data tidak tersedia.'}</div>;
  return <div className="records-state">{empty}</div>;
}

export function StatusBadge({ value, label }: { value: string; label?: string }) {
  return <span className={`status-badge status-${value.replaceAll('_', '-')}`}>{label ?? statusLabel(value)}</span>;
}

export function ModelModeBadge({ mode }: { mode: string }) {
  if (mode === 'dev_mock') return <span className="status-badge status-dev-mock" title="Output dihasilkan oleh model tiruan untuk pengembangan">Dev mock model — bukan model nyata</span>;
  return <span className="status-badge">{mode === 'provider' ? 'Provider model' : statusLabel(mode || 'unknown')}</span>;
}

export function JsonBlock({ value, label }: { value: unknown; label?: string }) {
  const text = value === undefined || value === null ? '—' : typeof value === 'string' ? value : JSON.stringify(value, null, 2);
  return <pre className="json-block" aria-label={label} tabIndex={0}>{text}</pre>;
}

export function ChipList({ items, tone = 'neutral', empty = 'Tidak ada' }: { items?: string[] | null; tone?: 'neutral' | 'warn' | 'bad' | 'good'; empty?: string }) {
  if (!items?.length) return <span className="muted-text">{empty}</span>;
  return <span className="chip-list">{items.map((item) => <span className={`chip chip-${tone}`} key={item}>{item}</span>)}</span>;
}

export function useAgents() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState('');
  const refresh = useCallback(async () => {
    setState('loading');
    setError('');
    try {
      const result = await api.agents(100, 0);
      setAgents(result.items.filter((agent) => agent.lifecycle_status !== 'archived'));
      setState('ready');
    } catch (err) {
      setError(errorText(err));
      setState('error');
    }
  }, []);
  useEffect(() => { void refresh(); }, [refresh]);
  return { agents, state, error, refresh };
}

export function useAgentVersions(agentKey: string) {
  const [versions, setVersions] = useState<AgentVersion[]>([]);
  const [state, setState] = useState<LoadState | 'idle'>('idle');
  const [error, setError] = useState('');
  useEffect(() => {
    if (!agentKey) { setVersions([]); setState('idle'); return; }
    let cancelled = false;
    setState('loading');
    setError('');
    api.agentVersions(agentKey, 100, 0).then((result) => {
      if (cancelled) return;
      setVersions(result.items);
      setState('ready');
    }, (err) => {
      if (cancelled) return;
      setVersions([]);
      setError(errorText(err));
      setState('error');
    });
    return () => { cancelled = true; };
  }, [agentKey]);
  return { versions, state, error };
}

export function AgentSelect({ agents, value, onChange, disabled, label = 'Agent', allowAll = false }: { agents: Agent[]; value: string; onChange: (key: string) => void; disabled?: boolean; label?: string; allowAll?: boolean }) {
  return (
    <label className="field"><span>{label}</span>
      <select value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled}>
        <option value="">{allowAll ? 'Semua agent' : 'Pilih agent'}</option>
        {agents.map((agent) => <option key={agent.id} value={agent.key}>{agent.display_name} · {agent.key}</option>)}
      </select>
    </label>
  );
}

export function VersionSelect({ versions, value, onChange, disabled, state, error }: { versions: AgentVersion[]; value: string; onChange: (id: string) => void; disabled?: boolean; state: LoadState | 'idle'; error?: string }) {
  return (
    <label className="field"><span>Agent version</span>
      <select value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled || state !== 'ready' || versions.length === 0}>
        <option value="">{state === 'loading' ? 'Memuat versi…' : state === 'error' ? 'Versi tidak dapat dimuat' : 'Pilih versi'}</option>
        {versions.map((version) => <option key={version.id} value={version.id}>{version.semver} · {version.state}</option>)}
      </select>
      {state === 'error' && <small className="error-text">{error}</small>}
    </label>
  );
}

export function ValidationSummary({ validation }: { validation: RunValidation | null | undefined }) {
  if (!validation) return <div className="records-state">Validasi belum tersedia.</div>;
  return (
    <dl className="detail-grid">
      <div><dt>Schema valid</dt><dd>{validation.schema_valid === undefined ? '—' : validation.schema_valid ? <StatusBadge value="pass" label="valid" /> : <StatusBadge value="failed" label="tidak valid" />}</dd></div>
      <div><dt>Risk flags</dt><dd><ChipList items={validation.risk_flags} tone="warn" /></dd></div>
      <div><dt>Cited sources</dt><dd><ChipList items={validation.cited_sources} /></dd></div>
      <div><dt>Unknown citations</dt><dd><ChipList items={validation.unknown_citations} tone="bad" /></dd></div>
      <div><dt>Unsupported claim paths</dt><dd><ChipList items={validation.unsupported_claim_paths} tone="bad" /></dd></div>
      {validation.schema_errors?.length ? <div className="span-all"><dt>Schema errors</dt><dd><ul className="plain-list">{validation.schema_errors.map((item) => <li key={item}><code>{item}</code></li>)}</ul></dd></div> : null}
      {validation.raw_output_preview ? <div className="span-all"><dt>Raw output preview</dt><dd><JsonBlock value={validation.raw_output_preview} /></dd></div> : null}
    </dl>
  );
}

export function EvidenceList({ evidence }: { evidence: EvidenceRef[] }) {
  if (!evidence.length) return <div className="records-state">Tidak ada evidence yang tercatat untuk run ini.</div>;
  return (
    <div className="provider-list">
      {evidence.map((item, index) => (
        <article className="provider-row wrap-row" key={`${item.source_id}-${item.source_version}-${index}`}>
          <div className="record-main">
            <strong>{item.title || item.source_id}</strong>
            <span>{item.source_id} · versi {item.source_version || '—'}{item.anchor ? ` · ${item.anchor}` : ''}</span>
            <small>Trust: {statusLabel(item.trust_status)} · Publikasi: {statusLabel(item.publication_status || '—')}</small>
          </div>
          {item.origin === 'fixture' ? <span className="status-badge status-fixture" title="Evidence fixture dari form uji, bukan sumber KMS">Fixture · data uji</span> : <span className="status-badge">{item.origin}</span>}
          {item.cited ? <span className="status-badge status-succeeded">dikutip</span> : <span className="status-badge">tidak dikutip</span>}
        </article>
      ))}
    </div>
  );
}

export function EventTimeline({ events }: { events: RunEvent[] }) {
  if (!events.length) return <div className="records-state">Belum ada event.</div>;
  return (
    <ol className="event-list">
      {events.map((event) => (
        <li key={event.seq}>
          <span className="event-seq">{event.seq}</span>
          <div><strong>{event.type}</strong><small>{formatDate(event.created_at)}</small><code>{JSON.stringify(event.payload)}</code></div>
        </li>
      ))}
    </ol>
  );
}

const feedbackClasses: FeedbackClassification[] = ['factual_error', 'missing_evidence', 'format', 'policy', 'style', 'other'];

function FeedbackForm({ run, session }: { run: Run; session: Session | null }) {
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null);
  const allowed = can(session, 'feedback.create');

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setBusy(true);
    setNotice(null);
    try {
      await api.createFeedback({
        agent_key: run.agent_key,
        run_id: run.id,
        classification: String(data.get('classification')) as FeedbackClassification,
        note: String(data.get('note')).trim(),
        reusable_candidate: data.get('reusable') === 'on',
      });
      form.reset();
      setNotice({ ok: true, text: 'Feedback tercatat untuk triase.' });
    } catch (err) {
      setNotice({ ok: false, text: errorText(err) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="catalog-form" onSubmit={submit}>
      <h3>Feedback reviewer</h3>
      <div className="field-grid">
        <label className="field"><span>Klasifikasi</span><select name="classification" required disabled={!allowed || busy}>{feedbackClasses.map((item) => <option key={item} value={item}>{statusLabel(item)}</option>)}</select></label>
        <label className="field checkbox-field"><span><input name="reusable" type="checkbox" disabled={!allowed || busy} /> Kandidat kasus evaluasi</span></label>
      </div>
      <label className="field"><span>Catatan</span><textarea name="note" rows={3} maxLength={4000} required disabled={!allowed || busy} style={{ fontFamily: 'inherit' }} /></label>
      <button className="button" type="submit" disabled={!allowed || busy}>{busy ? 'Mengirim…' : 'Kirim feedback'}</button>
      <PermissionNote session={session} permission="feedback.create" action="Feedback" />
      {notice && <div className={`alert ${notice.ok ? 'success' : 'error'}`} role="status" style={{ marginTop: 12 }}>{notice.text}</div>}
    </form>
  );
}

/** Loads a run with its evidence and trace, polling while it is queued or running. */
export function RunDetailView({ runId, session, onChanged }: { runId: string; session: Session | null; onChanged?: (run: Run) => void }) {
  const [run, setRun] = useState<Run | null>(null);
  const [evidence, setEvidence] = useState<EvidenceRef[]>([]);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState('');
  const [actionError, setActionError] = useState('');
  const [tick, setTick] = useState(0);
  const [cancelling, setCancelling] = useState(false);

  useEffect(() => { setState('loading'); setRun(null); setEvents([]); setEvidence([]); setActionError(''); setTick(0); }, [runId]);

  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;
    (async () => {
      try {
        const detail = await api.run(runId);
        const trace = await api.runEvents(runId).catch(() => ({ items: [] as RunEvent[] }));
        if (cancelled) return;
        setRun(detail.run);
        setEvidence(detail.evidence ?? []);
        setEvents(trace.items ?? []);
        setState('ready');
        onChanged?.(detail.run);
        if (activeRunStatuses.includes(detail.run.status) && tick < 200) timer = window.setTimeout(() => setTick((value) => value + 1), 1500);
      } catch (err) {
        if (cancelled) return;
        setError(errorText(err));
        setState('error');
      }
    })();
    return () => { cancelled = true; if (timer) window.clearTimeout(timer); };
    // onChanged is an inline callback; polling is driven by id and tick only.
  }, [runId, tick]);

  async function cancel() {
    if (!run) return;
    setCancelling(true);
    setActionError('');
    try {
      await api.cancelRun(run.id);
      setTick((value) => value + 1);
    } catch (err) {
      setActionError(errorText(err));
    } finally {
      setCancelling(false);
    }
  }

  if (state !== 'ready' || !run) return <RecordsState state={state} message={error} empty="" />;
  const active = activeRunStatuses.includes(run.status);

  return (
    <div className="run-detail">
      <div className="run-header">
        <div className="record-main">
          <strong>{run.agent_key} · v{run.agent_semver}</strong>
          <span>Run <code>{run.id}</code> · {run.kind} · dibuat {formatDate(run.created_at)}</span>
          <small>Caller: {run.caller_subject} · Attempt {run.attempts}/{run.max_attempts} · Durasi {runDuration(run)} · Token {run.tokens_in}/{run.tokens_out} · Tool calls {run.tool_calls}</small>
        </div>
        <div className="badge-stack">
          <StatusBadge value={run.status} />
          <ModelModeBadge mode={run.model_mode} />
          {run.cancel_requested && <span className="status-badge status-cancelled">pembatalan diminta</span>}
        </div>
      </div>
      {run.model_mode === 'dev_mock' && <div className="alert mock-alert" role="note"><strong>Dev mock model — bukan model nyata.</strong> Output ini berasal dari model tiruan untuk pengujian alur, bukan dari provider LLM.</div>}
      <div className="alert draft-alert" role="note">Output agent selalu berstatus <strong>draft</strong> dan perlu ditinjau manusia sebelum dipakai. Status run tidak berarti persetujuan.</div>
      {run.error_code && <div className="alert error" role="alert">Error code: <code>{run.error_code}</code></div>}
      {active && (
        <div className="inline-actions">
          <span className="form-hint" role="status">Run masih {statusLabel(run.status)}; halaman memperbarui otomatis.</span>
          <button className="button" type="button" onClick={() => void cancel()} disabled={cancelling || run.cancel_requested || !can(session, 'run.cancel')}>{cancelling ? 'Membatalkan…' : 'Batalkan run'}</button>
          <PermissionNote session={session} permission="run.cancel" action="Pembatalan run" />
        </div>
      )}
      {actionError && <div className="alert error" role="alert">{actionError}</div>}

      <h3 className="section-title">Validasi</h3>
      <ValidationSummary validation={run.validation} />

      <h3 className="section-title">Draft output</h3>
      {run.output === undefined || run.output === null ? <div className="records-state">{active ? 'Menunggu output…' : 'Tidak ada output.'}</div> : <JsonBlock value={run.output} label="Draft output" />}

      <h3 className="section-title">Evidence</h3>
      <EvidenceList evidence={evidence} />

      <details className="detail-disclosure">
        <summary className="catalog-summary">Input run</summary>
        <JsonBlock value={run.input} label="Input" />
      </details>
      <details className="detail-disclosure">
        <summary className="catalog-summary">Trace events ({events.length})</summary>
        <EventTimeline events={events} />
      </details>
      <details className="detail-disclosure">
        <summary className="catalog-summary">Versi yang dipin</summary>
        <JsonBlock value={run.pinned} label="Pinned" />
      </details>

      {!active && <FeedbackForm run={run} session={session} />}
    </div>
  );
}
