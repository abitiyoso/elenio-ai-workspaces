import { FormEvent, useCallback, useEffect, useState } from 'react';
import { EvalRun, Release, ReleaseEnvironment, ReleaseRequest, Session, api } from '../api';
import {
  AgentSelect, LoadState, PermissionNote, RecordsState, StatusBadge, VersionSelect,
  can, errorText, formatDate, statusLabel, useAgentVersions, useAgents,
} from './shared';
import { GateBadge } from './Evaluations';

const requestStatuses = ['pending_review', 'pending_ops', 'published', 'rejected', 'withdrawn'];

type Notice = { ok: boolean; text: string } | null;

export default function Releases({ session }: { session: Session | null }) {
  const { agents, state: agentState } = useAgents();
  const [agentKey, setAgentKey] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [releases, setReleases] = useState<Release[]>([]);
  const [releaseState, setReleaseState] = useState<LoadState>('loading');
  const [releaseError, setReleaseError] = useState('');
  const [requests, setRequests] = useState<ReleaseRequest[]>([]);
  const [requestState, setRequestState] = useState<LoadState>('loading');
  const [requestError, setRequestError] = useState('');
  const [notice, setNotice] = useState<Notice>(null);
  const [busy, setBusy] = useState(false);
  const canRead = can(session, 'release.read');

  const loadReleases = useCallback(async () => {
    if (!canRead) return;
    setReleaseState('loading');
    try {
      setReleases((await api.releases(agentKey)).items);
      setReleaseState('ready');
    } catch (err) {
      setReleaseError(errorText(err));
      setReleaseState('error');
    }
  }, [agentKey, canRead]);

  const loadRequests = useCallback(async () => {
    if (!canRead) return;
    setRequestState('loading');
    try {
      setRequests((await api.releaseRequests(statusFilter)).items);
      setRequestState('ready');
    } catch (err) {
      setRequestError(errorText(err));
      setRequestState('error');
    }
  }, [statusFilter, canRead]);

  useEffect(() => { void loadReleases(); }, [loadReleases]);
  useEffect(() => { void loadRequests(); }, [loadRequests]);

  async function act(action: () => Promise<string>) {
    setBusy(true);
    setNotice(null);
    try {
      const result = { ok: true, text: await action() };
      setNotice(result);
      await Promise.all([loadReleases(), loadRequests()]);
      return result;
    } catch (err) {
      const result = { ok: false, text: errorText(err) };
      setNotice(result);
      return result;
    } finally {
      setBusy(false);
    }
  }

  const visibleRequests = agentKey ? requests.filter((item) => item.agent_key === agentKey) : requests;

  return (
    <>
      <div className="page-heading">
        <div><p className="eyebrow">GOVERNANCE / RELEASE CONTROL</p><h1>Releases</h1><p className="page-subtitle">Ajukan release dari evaluation yang lulus, lalu lewati review domain dan keputusan AI Ops oleh orang yang berbeda.</p></div>
        <button className="button secondary" type="button" onClick={() => { void loadReleases(); void loadRequests(); }} disabled={busy}>Muat ulang</button>
      </div>
      <div className="scope-banner" role="note"><span className="scope-mark" aria-hidden="true">i</span><div><strong>Alur release bergerbang</strong><p>Syarat: gate evaluation <code>pass</code> dan tidak stale · domain reviewer ≠ pengaju · AI Ops approver ≠ pengaju dan ≠ reviewer. API memeriksa ulang setiap syarat.</p></div></div>
      {!canRead && <PermissionNote session={session} permission="release.read" action="Data release" />}
      {notice && <div className={`alert ${notice.ok ? 'success' : 'error'}`} role={notice.ok ? 'status' : 'alert'}>{notice.ok ? notice.text : <><strong>Permintaan ditolak API:</strong> {notice.text}</>}</div>}

      <section className="panel catalog-controls filter-controls" aria-label="Filter release">
        <AgentSelect agents={agents} value={agentKey} onChange={setAgentKey} disabled={agentState !== 'ready'} allowAll />
        <label className="field"><span>Status permintaan</span><select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}><option value="">Semua status</option>{requestStatuses.map((item) => <option key={item} value={item}>{statusLabel(item)}</option>)}</select></label>
      </section>

      <section className="provider-layout" aria-label="Permintaan release">
        <div className="panel records-panel">
          <div className="panel-heading"><div><h2>Release requests</h2><p>Permintaan menunggu review domain lalu keputusan AI Ops.</p></div><span className="count">{requestState === 'ready' ? `${visibleRequests.length} permintaan` : 'Belum dimuat'}</span></div>
          {canRead && <RecordsState state={requestState} message={requestError} empty="Belum ada release request." count={visibleRequests.length} />}
          {canRead && requestState === 'ready' && visibleRequests.length > 0 && <div className="provider-list">{visibleRequests.map((item) => <RequestRow key={item.id} item={item} session={session} busy={busy} act={act} />)}</div>}
        </div>
        <RequestForm agentKey={agentKey} agents={agents} onAgentChange={setAgentKey} session={session} busy={busy} act={act} />
      </section>

      <section className="panel deployments-panel" aria-label="Releases">
        <div className="panel-heading"><div><h2>Releases</h2><p>Versi aktif per environment. Rollback hanya memengaruhi run baru; run yang sudah berjalan tetap memakai versi yang dipin.</p></div><span className="count">{releaseState === 'ready' ? `${releases.length} release` : 'Belum dimuat'}</span></div>
        {canRead && <RecordsState state={releaseState} message={releaseError} empty="Belum ada release untuk filter ini." count={releases.length} />}
        {canRead && releaseState === 'ready' && releases.length > 0 && (
          <div className="table-scroll"><table>
            <caption className="sr-only">Releases</caption>
            <thead><tr><th>Agent / versi</th><th>Environment</th><th>Status</th><th>Dibuat</th><th>Berakhir</th><th>Rollback</th></tr></thead>
            <tbody>{releases.map((release) => (
              <tr key={release.id}>
                <td><strong>{release.agent_key}</strong><small>v{release.agent_semver}{release.rolled_back_from ? ' · hasil rollback' : ''}</small></td>
                <td>{release.environment}</td>
                <td><StatusBadge value={release.status} /></td>
                <td>{formatDate(release.created_at)}<small>{release.created_by}</small></td>
                <td>{formatDate(release.ended_at)}</td>
                <td>{release.status === 'active' ? <RollbackControl release={release} session={session} busy={busy} act={act} /> : '—'}</td>
              </tr>
            ))}</tbody>
          </table></div>
        )}
      </section>
    </>
  );
}

function RequestForm({ agentKey, agents, onAgentChange, session, busy, act }: {
  agentKey: string;
  agents: ReturnType<typeof useAgents>['agents'];
  onAgentChange: (key: string) => void;
  session: Session | null;
  busy: boolean;
  act: (action: () => Promise<string>) => Promise<Notice>;
}) {
  const { versions, state: versionState, error: versionError } = useAgentVersions(agentKey);
  const [versionId, setVersionId] = useState('');
  const [evalRuns, setEvalRuns] = useState<EvalRun[]>([]);
  const [evalState, setEvalState] = useState<LoadState | 'idle'>('idle');
  const [evalError, setEvalError] = useState('');
  const [evalRunId, setEvalRunId] = useState('');
  const [environment, setEnvironment] = useState<ReleaseEnvironment>('production');
  const [note, setNote] = useState('');
  const [result, setResult] = useState<Notice>(null);
  const allowed = can(session, 'release.request');

  useEffect(() => {
    if (versionState !== 'ready') { setVersionId(''); return; }
    setVersionId((versions.find((item) => item.state === 'draft') ?? versions[0])?.id ?? '');
  }, [versions, versionState]);

  useEffect(() => {
    if (!agentKey || !can(session, 'eval.read')) { setEvalRuns([]); setEvalState('idle'); return; }
    let cancelled = false;
    setEvalState('loading');
    api.evalRuns(agentKey, 100, 0).then((result) => {
      if (cancelled) return;
      setEvalRuns(result.items);
      setEvalState('ready');
    }, (err) => {
      if (cancelled) return;
      setEvalError(errorText(err));
      setEvalState('error');
    });
    return () => { cancelled = true; };
  }, [agentKey, session]);

  const candidates = evalRuns.filter((run) => run.agent_version_id === versionId);
  useEffect(() => {
    const matching = evalRuns.filter((run) => run.agent_version_id === versionId);
    setEvalRunId(matching.find((run) => run.status === 'completed')?.id ?? matching[0]?.id ?? '');
  }, [versionId, evalRuns]);
  const selectedEval = candidates.find((run) => run.id === evalRunId);
  const gateProblem = selectedEval && (selectedEval.status !== 'completed' ? 'Evaluation belum selesai.' : selectedEval.summary?.gate !== 'pass' ? `Gate evaluation ${selectedEval.summary?.gate ?? 'tidak diketahui'}${selectedEval.summary?.reasons?.length ? ` (${selectedEval.summary.reasons.join(', ')})` : ''}.` : selectedEval.stale ? 'Evaluation stale; versi berubah setelah dievaluasi.' : '');

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setResult(null);
    void act(async () => {
      const created = await api.createReleaseRequest({ agent_version_id: versionId, eval_run_id: evalRunId, environment, note: note.trim() });
      setNote('');
      return `Release request ${created.agent_key} v${created.agent_semver} (${created.environment}) diajukan dan menunggu review domain.`;
    }).then(setResult);
  }

  return (
    <form className="panel form-panel" onSubmit={submit}>
      <div className="panel-heading"><div><h2>Ajukan release</h2><p>Pilih versi draft dan evaluation run untuk versi tersebut.</p></div></div>
      <AgentSelect agents={agents} value={agentKey} onChange={onAgentChange} disabled={busy} />
      <VersionSelect versions={versions} value={versionId} onChange={setVersionId} disabled={!agentKey || busy} state={versionState} error={versionError} />
      <label className="field"><span>Evaluation run</span>
        <select value={evalRunId} onChange={(event) => setEvalRunId(event.target.value)} disabled={busy || evalState !== 'ready' || candidates.length === 0}>
          <option value="">{evalState === 'loading' ? 'Memuat evaluation…' : candidates.length ? 'Pilih evaluation run' : 'Belum ada evaluation untuk versi ini'}</option>
          {candidates.map((run) => <option key={run.id} value={run.id}>{formatDate(run.created_at)} · {run.suite_key} · {run.status === 'completed' ? `gate ${run.summary?.gate ?? '?'}` : run.status}{run.stale ? ' · stale' : ''}</option>)}
        </select>
        {evalState === 'error' && <small className="error-text">{evalError}</small>}
      </label>
      <PermissionNote session={session} permission="eval.read" action="Daftar evaluation" />
      {selectedEval && <div className="gate-preview"><GateBadge run={selectedEval} />{gateProblem ? <span className="error-text">{gateProblem} API kemungkinan akan menolak permintaan ini.</span> : <span>Gate lulus dan belum stale.</span>}</div>}
      <label className="field"><span>Environment</span><select value={environment} onChange={(event) => setEnvironment(event.target.value as ReleaseEnvironment)} disabled={busy}><option value="production">production</option><option value="staging">staging</option></select></label>
      <label className="field"><span>Catatan untuk reviewer</span><textarea value={note} onChange={(event) => setNote(event.target.value)} rows={3} maxLength={2000} disabled={busy} style={{ fontFamily: 'inherit' }} /></label>
      <button className="button primary full" type="submit" disabled={!allowed || busy || !versionId || !evalRunId}>{busy ? 'Memproses…' : 'Ajukan release request'}</button>
      <PermissionNote session={session} permission="release.request" action="Pengajuan release" />
      {result && <div className={`alert ${result.ok ? 'success' : 'error'}`} role={result.ok ? 'status' : 'alert'} style={{ margin: '12px 0 0' }}>{result.ok ? result.text : <><strong>Ditolak API:</strong> {result.text}</>}</div>}
    </form>
  );
}

function RequestRow({ item, session, busy, act }: { item: ReleaseRequest; session: Session | null; busy: boolean; act: (action: () => Promise<string>) => Promise<Notice> }) {
  const [reason, setReason] = useState('');
  const subject = session?.subject ?? '';
  const isRequester = subject !== '' && subject === item.requested_by;
  const isReviewer = subject !== '' && subject === item.reviewed_by;
  const review = item.status === 'pending_review';
  const decide = item.status === 'pending_ops';
  const permission = review ? 'release.review' : 'release.approve';
  const role = review ? 'Review domain' : 'Keputusan AI Ops';
  // Development walkthroughs may waive separation of duties for one account (AGENT_STUDIO_SELF_APPROVAL_EXEMPT_SUBJECTS).
  const exempt = Boolean(session?.self_approval_exempt);
  const sameActor = review ? isRequester : decide ? isRequester || isReviewer : false;
  const sodReason = exempt ? '' : review ? (isRequester ? 'Anda adalah pengaju; review domain harus dilakukan orang lain.' : '') : decide ? (isRequester ? 'Anda adalah pengaju; keputusan AI Ops harus dilakukan orang lain.' : isReviewer ? 'Anda sudah menjadi reviewer domain; keputusan AI Ops harus dilakukan orang lain.' : '') : '';
  const decisionBlocked = !can(session, permission) || Boolean(sodReason);

  function decideRequest(approve: boolean) {
    void act(async () => {
      const input = { approve, reason: reason.trim() };
      const result = review ? await api.reviewRelease(item.id, input) : await api.decideRelease(item.id, input);
      setReason('');
      return `Permintaan ${item.agent_key} v${item.agent_semver}: ${statusLabel(result.status)}.${result.status === 'published' ? ' Versi aktif untuk run baru.' : ''}`;
    });
  }

  return (
    <article className="provider-row wrap-row request-row">
      <div className="record-main">
        <strong>{item.agent_key} · v{item.agent_semver} → {item.environment}</strong>
        <span>Diajukan {item.requested_by} · {formatDate(item.created_at)}{item.request_note ? ` · “${item.request_note}”` : ''}</span>
        {item.reviewed_by && <small>Review domain: {item.reviewed_by} · {formatDate(item.reviewed_at)}{item.review_reason ? ` · ${item.review_reason}` : ''}</small>}
        {item.decided_by && <small>Keputusan AI Ops: {item.decided_by} · {formatDate(item.decided_at)}{item.decision_reason ? ` · ${item.decision_reason}` : ''}</small>}
        <small>Evaluation <code>{item.eval_run_id.slice(0, 8)}</code></small>
        {item.self_approval_exempt && <small>Tanpa review independen: pengecualian separation of duties (development).</small>}
      </div>
      <StatusBadge value={item.status} />
      {(review || decide) && (
        <div className="decision-box">
          <label className="field"><span>{role} — alasan (wajib)</span><textarea rows={2} maxLength={2000} value={reason} onChange={(event) => setReason(event.target.value)} disabled={decisionBlocked || busy} style={{ fontFamily: 'inherit', minHeight: 56 }} /></label>
          <div className="catalog-actions">
            <button className="button primary" type="button" disabled={decisionBlocked || busy || !reason.trim()} onClick={() => decideRequest(true)}>{review ? 'Setujui review' : 'Publish release'}</button>
            <button className="button" type="button" disabled={decisionBlocked || busy || !reason.trim()} onClick={() => decideRequest(false)}>Tolak</button>
            {isRequester && can(session, 'release.request') && <button className="button" type="button" disabled={busy} onClick={() => void act(async () => { await api.withdrawRelease(item.id); return 'Release request ditarik.'; })}>Tarik permintaan</button>}
          </div>
          {sodReason && <p className="form-hint permission-note" role="note">{sodReason}</p>}
          {exempt && sameActor && <p className="form-hint permission-note" role="note">Mode development: akun ini dikecualikan dari separation of duties, jadi Anda bisa melanjutkan permintaan Anda sendiri. Tercatat di audit.</p>}
          <PermissionNote session={session} permission={permission} action={role} />
        </div>
      )}
    </article>
  );
}

function RollbackControl({ release, session, busy, act }: { release: Release; session: Session | null; busy: boolean; act: (action: () => Promise<string>) => Promise<Notice> }) {
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState('');
  const allowed = can(session, 'release.rollback');
  if (!allowed) return <PermissionNote session={session} permission="release.rollback" action="Rollback" />;
  if (!open) return <div className="rollback-cell"><button className="button" type="button" onClick={() => setOpen(true)} disabled={busy}>Rollback…</button><small>Hanya memengaruhi run baru.</small></div>;
  return (
    <div className="rollback-cell">
      <label className="field"><span>Alasan rollback</span><input value={reason} onChange={(event) => setReason(event.target.value)} maxLength={2000} disabled={busy} /></label>
      <div className="catalog-actions">
        <button className="button primary" type="button" disabled={busy || !reason.trim()} onClick={() => void act(async () => { const result = await api.rollbackRelease(release.id, reason.trim()); setOpen(false); setReason(''); return `Rollback selesai: ${result.agent_key} kembali ke v${result.agent_semver} untuk run baru.`; })}>Konfirmasi rollback</button>
        <button className="button" type="button" disabled={busy} onClick={() => setOpen(false)}>Batal</button>
      </div>
      <small>Rollback hanya memengaruhi run baru; run yang sedang berjalan tetap memakai versi yang dipin.</small>
    </div>
  );
}
