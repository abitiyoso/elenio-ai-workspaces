import { useCallback, useEffect, useState } from 'react';
import { AuditEvent, FeedbackItem, Run, Session, api } from '../api';
import {
  TableScroll,  AgentSelect, LoadState, ModelModeBadge, PermissionNote, RecordsState, RunDetailView, StatusBadge,
  can, errorText, formatDate, runDuration, statusLabel, useAgents,
} from './shared';

const pageSize = 25;
const auditPageSize = 50;
const runStatuses = ['queued', 'running', 'succeeded', 'needs_review', 'failed', 'cancelled'];
const runKinds = ['production', 'sandbox', 'evaluation'];

export default function Runs({ session }: { session: Session | null }) {
  const { agents } = useAgents();
  const [agentKey, setAgentKey] = useState('');
  const [kind, setKind] = useState('');
  const [status, setStatus] = useState('');
  const [offset, setOffset] = useState(0);
  const [runs, setRuns] = useState<Run[]>([]);
  const [total, setTotal] = useState(0);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState('');
  const [selectedId, setSelectedId] = useState('');

  const [feedback, setFeedback] = useState<FeedbackItem[]>([]);
  const [feedbackState, setFeedbackState] = useState<LoadState>('loading');
  const [feedbackError, setFeedbackError] = useState('');

  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [auditTotal, setAuditTotal] = useState(0);
  const [auditOffset, setAuditOffset] = useState(0);
  const [auditState, setAuditState] = useState<LoadState>('loading');
  const [auditError, setAuditError] = useState('');

  const canRuns = can(session, 'run.read');
  const canFeedback = can(session, 'feedback.read');
  const canAudit = can(session, 'audit.read');

  const loadRuns = useCallback(async () => {
    if (!canRuns) return;
    setState('loading');
    setError('');
    try {
      const result = await api.runs({ agent_key: agentKey, kind, status, limit: pageSize, offset });
      setRuns(result.items);
      setTotal(result.total);
      setState('ready');
    } catch (err) {
      setError(errorText(err));
      setState('error');
    }
  }, [agentKey, kind, status, offset, canRuns]);

  const loadFeedback = useCallback(async () => {
    if (!canFeedback) return;
    setFeedbackState('loading');
    try {
      setFeedback((await api.feedback(agentKey)).items);
      setFeedbackState('ready');
    } catch (err) {
      setFeedbackError(errorText(err));
      setFeedbackState('error');
    }
  }, [agentKey, canFeedback]);

  const loadAudit = useCallback(async () => {
    if (!canAudit) return;
    setAuditState('loading');
    try {
      const result = await api.auditEvents(auditPageSize, auditOffset);
      setAudit(result.items);
      setAuditTotal(result.total);
      setAuditState('ready');
    } catch (err) {
      setAuditError(errorText(err));
      setAuditState('error');
    }
  }, [auditOffset, canAudit]);

  useEffect(() => { void loadRuns(); }, [loadRuns]);
  useEffect(() => { void loadFeedback(); }, [loadFeedback]);
  useEffect(() => { void loadAudit(); }, [loadAudit]);

  function resetPage<T>(setter: (value: T) => void) {
    return (value: T) => { setter(value); setOffset(0); };
  }

  return (
    <>
      <div className="page-heading">
        <div><p className="eyebrow">OPERATE / OBSERVABILITY</p><h1>Runs &amp; Audit</h1><p className="page-subtitle">Riwayat run, trace, evidence, feedback reviewer, dan jejak audit tenant. Semua output agent adalah draft.</p></div>
        <button className="button secondary" type="button" onClick={() => { void loadRuns(); void loadFeedback(); void loadAudit(); }} disabled={state === 'loading'}>Muat ulang</button>
      </div>

      <section className="panel catalog-controls filter-controls" aria-label="Filter run">
        <AgentSelect agents={agents} value={agentKey} onChange={resetPage(setAgentKey)} allowAll />
        <label className="field"><span>Jenis</span><select value={kind} onChange={(event) => resetPage(setKind)(event.target.value)}><option value="">Semua jenis</option>{runKinds.map((item) => <option key={item} value={item}>{item}</option>)}</select></label>
        <label className="field"><span>Status</span><select value={status} onChange={(event) => resetPage(setStatus)(event.target.value)}><option value="">Semua status</option>{runStatuses.map((item) => <option key={item} value={item}>{statusLabel(item)}</option>)}</select></label>
      </section>

      <section className="panel deployments-panel" style={{ marginTop: 0 }} aria-label="Daftar run">
        <div className="panel-heading"><div><h2>Runs</h2><p>Pilih run untuk melihat validasi, evidence, dan trace.</p></div><span className="count">{state === 'ready' ? `${total} run` : 'Belum dimuat'}</span></div>
        {!canRuns ? <PermissionNote session={session} permission="run.read" action="Daftar run" /> : <RecordsState state={state} message={error} empty="Tidak ada run untuk filter ini." count={runs.length} />}
        {canRuns && state === 'ready' && runs.length > 0 && (
          <TableScroll label="Runs"><table>
            <caption className="sr-only">Runs</caption>
            <thead><tr><th>Dibuat</th><th>Agent</th><th>Jenis</th><th>Status</th><th>Model</th><th>Validasi</th><th>Durasi</th><th><span className="sr-only">Aksi</span></th></tr></thead>
            <tbody>{runs.map((run) => (
              <tr key={run.id} className={run.id === selectedId ? 'selected-row' : undefined}>
                <td><strong>{formatDate(run.created_at)}</strong><small>{run.caller_subject}</small></td>
                <td><strong>{run.agent_key}</strong><small>v{run.agent_semver}</small></td>
                <td>{run.kind}</td>
                <td><StatusBadge value={run.status} />{run.error_code && <small className="error-text">{run.error_code}</small>}</td>
                <td><ModelModeBadge mode={run.model_mode} /></td>
                <td>{run.validation?.schema_valid === false ? 'Schema tidak valid' : run.validation?.schema_valid ? 'Schema valid' : '—'}<small>{run.validation?.risk_flags?.length ? `${run.validation.risk_flags.length} risk flag` : 'Tanpa risk flag'}</small></td>
                <td>{runDuration(run)}</td>
                <td><button className="button" type="button" onClick={() => setSelectedId(run.id === selectedId ? '' : run.id)} aria-expanded={run.id === selectedId}>{run.id === selectedId ? 'Tutup' : 'Detail'}</button></td>
              </tr>
            ))}</tbody>
          </table></TableScroll>
        )}
        {canRuns && state === 'ready' && total > pageSize && <div className="pagination"><p className="footer-note">Menampilkan {offset + 1} sampai {offset + runs.length} dari {total} run.</p><div><button className="button" type="button" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}>Sebelumnya</button><button className="button" type="button" disabled={offset + runs.length >= total} onClick={() => setOffset(offset + pageSize)}>Berikutnya</button></div></div>}
      </section>

      {selectedId && (
        <section className="panel deployments-panel" aria-label="Detail run">
          <div className="panel-heading"><div><h2>Detail run</h2><p>Validasi, draft output, evidence, dan trace dari API.</p></div><button className="button" type="button" onClick={() => setSelectedId('')}>Tutup</button></div>
          <RunDetailView runId={selectedId} session={session} onChanged={(run) => setRuns((items) => items.map((item) => item.id === run.id ? run : item))} />
        </section>
      )}

      <section className="lower-grid">
        <section className="panel records-panel" aria-label="Audit events">
          <div className="panel-heading"><div><h2>Audit events</h2><p>Jejak aksi tenant terbaru.</p></div><span className="count">{auditState === 'ready' ? `${auditTotal} event` : 'Belum dimuat'}</span></div>
          {!canAudit ? <PermissionNote session={session} permission="audit.read" action="Audit log" /> : <RecordsState state={auditState} message={auditError} empty="Belum ada audit event." count={audit.length} />}
          {canAudit && auditState === 'ready' && audit.length > 0 && (
            <TableScroll label="Audit events"><table className="compact-table">
              <caption className="sr-only">Audit events</caption>
              <thead><tr><th>Waktu</th><th>Actor</th><th>Action</th><th>Resource</th></tr></thead>
              <tbody>{audit.map((item, index) => <tr key={`${item.created_at}-${item.resource}-${index}`}><td>{formatDate(item.created_at)}</td><td>{item.actor}</td><td><code>{item.action}</code></td><td><code>{item.resource}</code></td></tr>)}</tbody>
            </table></TableScroll>
          )}
          {canAudit && auditState === 'ready' && auditTotal > auditPageSize && <div className="pagination"><p className="footer-note">{auditOffset + 1}–{auditOffset + audit.length} dari {auditTotal}</p><div><button className="button" type="button" disabled={auditOffset === 0} onClick={() => setAuditOffset(Math.max(0, auditOffset - auditPageSize))}>Sebelumnya</button><button className="button" type="button" disabled={auditOffset + audit.length >= auditTotal} onClick={() => setAuditOffset(auditOffset + auditPageSize)}>Berikutnya</button></div></div>}
        </section>

        <section className="panel records-panel" aria-label="Feedback">
          <div className="panel-heading"><div><h2>Feedback reviewer</h2><p>{agentKey ? `Untuk ${agentKey}.` : 'Semua agent.'} Kirim feedback dari detail run.</p></div><span className="count">{feedbackState === 'ready' ? `${feedback.length} item` : 'Belum dimuat'}</span></div>
          {!canFeedback ? <PermissionNote session={session} permission="feedback.read" action="Daftar feedback" /> : <RecordsState state={feedbackState} message={feedbackError} empty="Belum ada feedback." count={feedback.length} />}
          {canFeedback && feedbackState === 'ready' && feedback.length > 0 && <div className="provider-list">{feedback.map((item) => (
            <article className="provider-row wrap-row" key={item.id}>
              <div className="record-main"><strong>{statusLabel(item.classification)} · {item.agent_key}</strong><span>{item.note}</span><small>{item.created_by} · {formatDate(item.created_at)}{item.run_id ? ` · run ${item.run_id.slice(0, 8)}` : ''}{item.reusable_candidate ? ' · kandidat kasus evaluasi' : ''}</small></div>
              <StatusBadge value={item.status} />
            </article>
          ))}</div>}
        </section>
      </section>
    </>
  );
}
