import { FormEvent, useCallback, useEffect, useState } from 'react';
import { EvalCase, EvalClassification, EvalRun, EvalSuite, Session, api } from '../api';
import {
  AgentSelect, ChipList, JsonBlock, LoadState, PermissionNote, RecordsState, RunDetailView, StatusBadge, VersionSelect,
  can, errorText, formatDate, parseJsonArray, parseJsonObject, statusLabel, useAgentVersions, useAgents,
} from './shared';

const classifications: EvalClassification[] = ['positive', 'negative', 'adversarial', 'evidence_conflict', 'missing_evidence'];

export function GateBadge({ run }: { run: Pick<EvalRun, 'status' | 'summary' | 'stale'> }) {
  if (run.status !== 'completed') return <StatusBadge value={run.status} />;
  const gate = run.summary?.gate ?? 'unknown';
  return <span className="badge-stack inline"><StatusBadge value={gate} label={`gate ${gate}`} />{run.stale && <StatusBadge value="stale" label="stale" />}</span>;
}

export default function Evaluations({ session }: { session: Session | null }) {
  const { agents, state: agentState, error: agentError } = useAgents();
  const [agentKey, setAgentKey] = useState('');
  const { versions, state: versionState, error: versionError } = useAgentVersions(agentKey);
  const [versionId, setVersionId] = useState('');

  const [suites, setSuites] = useState<EvalSuite[]>([]);
  const [suiteState, setSuiteState] = useState<LoadState | 'idle'>('idle');
  const [suiteError, setSuiteError] = useState('');
  const [suiteId, setSuiteId] = useState('');

  const [cases, setCases] = useState<EvalCase[]>([]);
  const [caseState, setCaseState] = useState<LoadState | 'idle'>('idle');
  const [caseError, setCaseError] = useState('');

  const [evalRuns, setEvalRuns] = useState<EvalRun[]>([]);
  const [evalRunTotal, setEvalRunTotal] = useState(0);
  const [evalRunState, setEvalRunState] = useState<LoadState>('loading');
  const [evalRunError, setEvalRunError] = useState('');
  const [evalRunId, setEvalRunId] = useState('');

  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null);
  const canRead = can(session, 'eval.read');
  const canWrite = can(session, 'eval.write');
  const canRun = can(session, 'eval.run');
  const suite = suites.find((item) => item.id === suiteId);

  useEffect(() => {
    if (versionState !== 'ready') { setVersionId(''); return; }
    setVersionId((versions.find((item) => item.state === 'draft') ?? versions[0])?.id ?? '');
  }, [versions, versionState]);

  const loadSuites = useCallback(async () => {
    if (!agentKey || !canRead) { setSuites([]); setSuiteState('idle'); return; }
    setSuiteState('loading');
    try {
      const items = (await api.evalSuites(agentKey)).items;
      setSuites(items);
      setSuiteId((current) => items.some((item) => item.id === current) ? current : items[0]?.id ?? '');
      setSuiteState('ready');
    } catch (err) {
      setSuiteError(errorText(err));
      setSuiteState('error');
    }
  }, [agentKey, canRead]);

  const loadCases = useCallback(async () => {
    if (!suiteId || !canRead) { setCases([]); setCaseState('idle'); return; }
    setCaseState('loading');
    try {
      setCases((await api.evalCases(suiteId)).items);
      setCaseState('ready');
    } catch (err) {
      setCaseError(errorText(err));
      setCaseState('error');
    }
  }, [suiteId, canRead]);

  const loadEvalRuns = useCallback(async () => {
    if (!canRead) return;
    setEvalRunState('loading');
    try {
      const result = await api.evalRuns(agentKey, 25, 0);
      setEvalRuns(result.items);
      setEvalRunTotal(result.total);
      setEvalRunState('ready');
    } catch (err) {
      setEvalRunError(errorText(err));
      setEvalRunState('error');
    }
  }, [agentKey, canRead]);

  useEffect(() => { void loadSuites(); }, [loadSuites]);
  useEffect(() => { void loadCases(); }, [loadCases]);
  useEffect(() => { void loadEvalRuns(); }, [loadEvalRuns]);

  async function act(action: () => Promise<string>) {
    setBusy(true);
    setNotice(null);
    try {
      setNotice({ ok: true, text: await action() });
    } catch (err) {
      setNotice({ ok: false, text: errorText(err) });
    } finally {
      setBusy(false);
    }
  }

  function submitSuite(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    void act(async () => {
      const created = await api.createEvalSuite({ agent_key: agentKey, key: String(data.get('suiteKey')).trim(), description: String(data.get('suiteDescription')).trim() });
      form.reset();
      await loadSuites();
      setSuiteId(created.id);
      return `Suite ${created.key} dibuat.`;
    });
  }

  function submitCase(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    void act(async () => {
      const created = await api.createEvalCase(suiteId, {
        key: String(data.get('caseKey')).trim(),
        classification: String(data.get('classification')) as EvalClassification,
        critical: data.get('critical') === 'on',
        input: parseJsonObject(String(data.get('caseInput')), 'Input'),
        evidence_fixture: parseJsonArray(String(data.get('caseFixture')), 'Evidence fixture'),
        expectations: parseJsonObject(String(data.get('caseExpectations')), 'Expectations'),
      });
      form.reset();
      await Promise.all([loadCases(), loadSuites()]);
      return `Kasus ${created.key} ditambahkan.`;
    });
  }

  function deleteCase(item: EvalCase) {
    if (!window.confirm(`Hapus kasus evaluasi ${item.key}?`)) return;
    void act(async () => {
      await api.deleteEvalCase(item.id);
      await Promise.all([loadCases(), loadSuites()]);
      return `Kasus ${item.key} dihapus.`;
    });
  }

  function startRun() {
    void act(async () => {
      const run = await api.startEvalRun({ suite_id: suiteId, agent_version_id: versionId });
      setEvalRunId(run.id);
      await loadEvalRuns();
      return `Evaluation run dimulai (${run.suite_key} · v${run.agent_semver}).`;
    });
  }

  return (
    <>
      <div className="page-heading">
        <div><p className="eyebrow">QUALITY / EVALUATION GATE</p><h1>Evaluations</h1><p className="page-subtitle">Kelola suite dan kasus uji, jalankan evaluation terhadap versi agent, dan periksa gate sebelum release.</p></div>
        <button className="button secondary" type="button" onClick={() => { void loadSuites(); void loadEvalRuns(); }} disabled={busy}>Muat ulang</button>
      </div>
      <div className="scope-banner" role="note"><span className="scope-mark" aria-hidden="true">i</span><div><strong>Gate release</strong><p>Release hanya dapat diajukan dari evaluation dengan gate <code>pass</code> yang belum stale. Evaluation dengan dev mock model selalu diblokir.</p></div></div>
      {!canRead && <PermissionNote session={session} permission="eval.read" action="Data evaluation" />}
      {notice && <div className={`alert ${notice.ok ? 'success' : 'error'}`} role="status">{notice.text}</div>}

      <section className="panel catalog-controls filter-controls" aria-label="Pilih agent">
        <AgentSelect agents={agents} value={agentKey} onChange={(key) => { setAgentKey(key); setSuiteId(''); setEvalRunId(''); }} disabled={agentState !== 'ready'} allowAll />
        <VersionSelect versions={versions} value={versionId} onChange={setVersionId} disabled={!agentKey || busy} state={versionState} error={versionError} />
        <div className="field run-action">
          <button className="button primary" type="button" onClick={startRun} disabled={!canRun || busy || !suiteId || !versionId}>Jalankan evaluation</button>
          <small>{suite ? `Suite ${suite.key} · ${suite.case_count} kasus` : 'Pilih agent dan suite terlebih dahulu.'}</small>
        </div>
        {!canRun && <PermissionNote session={session} permission="eval.run" action="Menjalankan evaluation" />}
        {agentState === 'error' && <div className="records-state error-state" role="alert">{agentError}</div>}
      </section>

      {agentKey && (
        <section className="catalog-grid" aria-label="Suites dan kasus">
          <section className="panel records-panel">
            <div className="panel-heading"><div><h2>Suites</h2><p>Suite evaluation untuk {agentKey}.</p></div><span className="count">{suiteState === 'ready' ? `${suites.length} suite` : 'Belum dimuat'}</span></div>
            <RecordsState state={suiteState} message={suiteError} empty="Belum ada suite untuk agent ini." count={suites.length} />
            {suiteState === 'ready' && suites.length > 0 && <div className="provider-list">{suites.map((item) => (
              <button type="button" className={`catalog-item ${item.id === suiteId ? 'selected' : ''}`} key={item.id} onClick={() => setSuiteId(item.id)} aria-pressed={item.id === suiteId}>
                <span className="record-main"><strong>{item.key}</strong><span>{item.description || 'Tanpa deskripsi'}</span><small>{item.case_count} kasus · {item.created_by}</small></span>
              </button>
            ))}</div>}
            <form className="catalog-form" onSubmit={submitSuite}>
              <h3>Suite baru</h3>
              <label className="field"><span>Suite key</span><input name="suiteKey" pattern="[a-z][a-z0-9_.-]{0,62}" maxLength={63} required disabled={!canWrite || busy} /></label>
              <label className="field"><span>Deskripsi</span><input name="suiteDescription" maxLength={2000} disabled={!canWrite || busy} /></label>
              <button className="button" type="submit" disabled={!canWrite || busy}>Buat suite</button>
              <PermissionNote session={session} permission="eval.write" action="Membuat suite" />
            </form>
          </section>

          <section className="panel records-panel">
            <div className="panel-heading"><div><h2>Kasus uji</h2><p>{suite ? `Suite ${suite.key}. Evidence fixture di kasus adalah data uji.` : 'Pilih suite.'}</p></div><span className="count">{caseState === 'ready' ? `${cases.length} kasus` : 'Belum dimuat'}</span></div>
            <RecordsState state={caseState} message={caseError} empty={suiteId ? 'Suite ini belum memiliki kasus.' : 'Pilih suite untuk melihat kasus.'} count={cases.length} />
            {caseState === 'ready' && cases.length > 0 && <div className="provider-list">{cases.map((item) => (
              <article className="provider-row wrap-row" key={item.id}>
                <div className="record-main">
                  <strong>{item.key}</strong>
                  <span>{statusLabel(item.classification)}{item.critical ? ' · critical' : ''} · {Array.isArray(item.evidence_fixture) ? item.evidence_fixture.length : 0} fixture</span>
                  <details><summary className="link-summary">Input, fixture, expectations</summary><JsonBlock value={{ input: item.input, evidence_fixture: item.evidence_fixture, expectations: item.expectations }} /></details>
                </div>
                {item.critical && <StatusBadge value="critical" />}
                <button className="button" type="button" disabled={!canWrite || busy} onClick={() => deleteCase(item)}>Hapus</button>
              </article>
            ))}</div>}
            {suiteId && (
              <form className="catalog-form" onSubmit={submitCase}>
                <h3>Kasus baru</h3>
                <div className="field-grid">
                  <label className="field"><span>Case key</span><input name="caseKey" pattern="[a-z][a-z0-9_.-]{0,62}" maxLength={63} required disabled={!canWrite || busy} /></label>
                  <label className="field"><span>Klasifikasi</span><select name="classification" disabled={!canWrite || busy}>{classifications.map((item) => <option key={item} value={item}>{statusLabel(item)}</option>)}</select></label>
                </div>
                <label className="field checkbox-field"><span><input name="critical" type="checkbox" disabled={!canWrite || busy} /> Critical (kegagalan memblokir gate)</span></label>
                <label className="field"><span>Input (JSON object)</span><textarea name="caseInput" rows={4} defaultValue="{}" spellCheck={false} disabled={!canWrite || busy} /></label>
                <label className="field"><span>Evidence fixture (JSON array, data uji)</span><textarea name="caseFixture" rows={4} defaultValue="[]" spellCheck={false} disabled={!canWrite || busy} /></label>
                <label className="field"><span>Expectations (JSON object)</span><textarea name="caseExpectations" rows={4} defaultValue={'{\n  "expect_status": ["succeeded", "needs_review"]\n}'} spellCheck={false} disabled={!canWrite || busy} /><small>Kunci yang dikenali runtime antara lain expect_status, expect_flags, required_paths, min_citations.</small></label>
                <button className="button" type="submit" disabled={!canWrite || busy}>Tambah kasus</button>
                <PermissionNote session={session} permission="eval.write" action="Menambah kasus" />
              </form>
            )}
          </section>
        </section>
      )}

      <section className="panel deployments-panel" aria-label="Evaluation runs">
        <div className="panel-heading"><div><h2>Evaluation runs</h2><p>{agentKey ? `Untuk ${agentKey}.` : 'Semua agent.'} Gate dan status stale dihitung oleh API.</p></div><span className="count">{evalRunState === 'ready' ? `${evalRunTotal} run` : 'Belum dimuat'}</span></div>
        {canRead && <RecordsState state={evalRunState} message={evalRunError} empty="Belum ada evaluation run." count={evalRuns.length} />}
        {canRead && evalRunState === 'ready' && evalRuns.length > 0 && (
          <div className="table-scroll"><table>
            <caption className="sr-only">Evaluation runs</caption>
            <thead><tr><th>Dibuat</th><th>Agent / versi</th><th>Suite</th><th>Status</th><th>Gate</th><th>Hasil</th><th><span className="sr-only">Aksi</span></th></tr></thead>
            <tbody>{evalRuns.map((run) => (
              <tr key={run.id} className={run.id === evalRunId ? 'selected-row' : undefined}>
                <td><strong>{formatDate(run.created_at)}</strong><small>{run.created_by}</small></td>
                <td><strong>{run.agent_key}</strong><small>v{run.agent_semver}</small></td>
                <td>{run.suite_key}</td>
                <td><StatusBadge value={run.status} /></td>
                <td><GateBadge run={run} /></td>
                <td>{run.summary?.total !== undefined ? `${run.summary.passed ?? 0}/${run.summary.total} lulus` : '—'}</td>
                <td><button className="button" type="button" onClick={() => setEvalRunId(run.id === evalRunId ? '' : run.id)} aria-expanded={run.id === evalRunId}>{run.id === evalRunId ? 'Tutup' : 'Detail'}</button></td>
              </tr>
            ))}</tbody>
          </table></div>
        )}
      </section>

      {evalRunId && <EvalRunDetail evalRunId={evalRunId} session={session} onSettled={() => void loadEvalRuns()} />}
    </>
  );
}

function EvalRunDetail({ evalRunId, session, onSettled }: { evalRunId: string; session: Session | null; onSettled: () => void }) {
  const [run, setRun] = useState<EvalRun | null>(null);
  const [state, setState] = useState<LoadState>('loading');
  const [error, setError] = useState('');
  const [tick, setTick] = useState(0);
  const [openRunId, setOpenRunId] = useState('');

  useEffect(() => { setState('loading'); setRun(null); setOpenRunId(''); setTick(0); }, [evalRunId]);

  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;
    api.evalRun(evalRunId).then((result) => {
      if (cancelled) return;
      setRun(result);
      setState('ready');
      if (result.status === 'running' && tick < 200) timer = window.setTimeout(() => setTick((value) => value + 1), 2000);
      else if (tick > 0) onSettled();
    }, (err) => {
      if (cancelled) return;
      setError(errorText(err));
      setState('error');
    });
    return () => { cancelled = true; if (timer) window.clearTimeout(timer); };
    // onSettled is an inline callback; polling is driven by id and tick only.
  }, [evalRunId, tick]);

  return (
    <section className="panel deployments-panel" aria-label="Detail evaluation run">
      <div className="panel-heading"><div><h2>Detail evaluation</h2><p>{run ? `${run.agent_key} · v${run.agent_semver} · suite ${run.suite_key}` : 'Memuat…'}</p></div>{run && <GateBadge run={run} />}</div>
      {state !== 'ready' || !run ? <RecordsState state={state} message={error} empty="" /> : (
        <>
          {run.status === 'running' && <div className="alert" role="status">Evaluation masih berjalan; halaman memperbarui otomatis.</div>}
          {run.stale && <div className="alert error" role="alert"><strong>Stale.</strong> Versi agent berubah setelah evaluation ini. Jalankan ulang evaluation sebelum mengajukan release.</div>}
          {run.summary?.model_modes?.includes('dev_mock') && <div className="alert mock-alert" role="note"><strong>Dev mock model — bukan model nyata.</strong> Evaluation ini memakai model tiruan, sehingga gate tidak dapat lulus.</div>}
          {run.summary && (
            <dl className="detail-grid">
              <div><dt>Gate</dt><dd>{run.summary.gate ? <StatusBadge value={run.summary.gate} label={`gate ${run.summary.gate}`} /> : '—'}</dd></div>
              <div><dt>Hasil</dt><dd>{run.summary.passed ?? 0} lulus · {run.summary.failed ?? 0} gagal · {run.summary.total ?? 0} total</dd></div>
              <div><dt>Alasan gate</dt><dd><ChipList items={run.summary.reasons} tone={run.summary.gate === 'pass' ? 'good' : 'bad'} /></dd></div>
              <div><dt>Critical gagal</dt><dd><ChipList items={run.summary.critical_failed} tone="bad" /></dd></div>
              <div><dt>Regresi</dt><dd><ChipList items={run.summary.regressions} tone="warn" /></dd></div>
              <div><dt>Regresi critical</dt><dd><ChipList items={run.summary.critical_regressions} tone="bad" /></dd></div>
              <div><dt>Model mode</dt><dd><ChipList items={run.summary.model_modes} tone={run.summary.model_modes?.includes('dev_mock') ? 'warn' : 'neutral'} /></dd></div>
              <div><dt>Fingerprint versi</dt><dd><code>{run.version_fingerprint.slice(0, 16)}…</code></dd></div>
            </dl>
          )}
          <h3 className="section-title">Hasil per kasus</h3>
          {!run.results?.length ? <div className="records-state">Belum ada hasil kasus.</div> : (
            <div className="table-scroll"><table>
              <caption className="sr-only">Hasil kasus evaluasi</caption>
              <thead><tr><th>Kasus</th><th>Status run</th><th>Lulus</th><th>Checks</th><th><span className="sr-only">Aksi</span></th></tr></thead>
              <tbody>{run.results.map((result) => (
                <tr key={result.case_id}>
                  <td><strong>{result.case_key}</strong><small>{statusLabel(result.classification)}{result.critical ? ' · critical' : ''}</small></td>
                  <td><StatusBadge value={result.run_status} /></td>
                  <td>{result.passed === null ? <StatusBadge value="pending" label="menunggu" /> : result.passed ? <StatusBadge value="pass" label="lulus" /> : <StatusBadge value="failed" label="gagal" />}{result.baseline_passed !== undefined && result.baseline_passed !== null && <small>Baseline: {result.baseline_passed ? 'lulus' : 'gagal'}</small>}</td>
                  <td><ul className="check-list">{(result.checks ?? []).map((check, index) => <li key={`${check.name}-${index}`} className={check.passed ? 'check-pass' : 'check-fail'}><span aria-label={check.passed ? 'lulus' : 'gagal'}>{check.passed ? '✓' : '✕'}</span> <code>{check.name}</code>{check.detail && <small>{check.detail}</small>}</li>)}</ul></td>
                  <td><button className="button" type="button" onClick={() => setOpenRunId(openRunId === result.run_id ? '' : result.run_id)}>{openRunId === result.run_id ? 'Tutup run' : 'Lihat run'}</button></td>
                </tr>
              ))}</tbody>
            </table></div>
          )}
          {openRunId && <div className="nested-detail"><RunDetailView runId={openRunId} session={session} /></div>}
        </>
      )}
    </section>
  );
}
