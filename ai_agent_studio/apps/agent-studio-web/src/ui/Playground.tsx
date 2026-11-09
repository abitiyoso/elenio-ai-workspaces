import { FormEvent, useCallback, useEffect, useState } from 'react';
import { Run, Session, api } from '../api';
import {
  AgentSelect, JsonBlock, LoadState, ModelModeBadge, PermissionNote, RecordsState, RunDetailView, StatusBadge, VersionSelect,
  can, errorText, formatDate, newIdempotencyKey, parseJsonArray, parseJsonObject, useAgentVersions, useAgents,
} from './shared';

// Editable example evidence: clearly test data, never presented as a tenant source.
const exampleFixture = [
  {
    source_id: 'example-fixture-1',
    source_version: 'v1',
    title: 'Contoh dokumen uji',
    snippet: 'Contoh: Platform harus mendukung single sign-on melalui SAML 2.0.',
    trust_status: 'approved',
    publication_status: 'published',
    anchor: 'p.1',
  },
];

function exampleFromSchema(schema: Record<string, unknown> | undefined): Record<string, unknown> {
  const properties = (schema?.properties ?? {}) as Record<string, { type?: string }>;
  const required = Array.isArray(schema?.required) ? (schema?.required as string[]) : [];
  const example: Record<string, unknown> = {};
  for (const key of required) {
    const type = properties[key]?.type;
    example[key] = type === 'array' ? [] : type === 'object' ? {} : type === 'number' || type === 'integer' ? 0 : type === 'boolean' ? false : 'Contoh teks uji';
  }
  return example;
}

export default function Playground({ session }: { session: Session | null }) {
  const { agents, state: agentState, error: agentError } = useAgents();
  const [agentKey, setAgentKey] = useState('');
  const { versions, state: versionState, error: versionError } = useAgentVersions(agentKey);
  const [versionId, setVersionId] = useState('');
  const [inputText, setInputText] = useState('{}');
  const [fixtureText, setFixtureText] = useState(JSON.stringify(exampleFixture, null, 2));
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState('');
  const [runId, setRunId] = useState('');
  const [recent, setRecent] = useState<Run[]>([]);
  const [recentState, setRecentState] = useState<LoadState>('loading');
  const [recentError, setRecentError] = useState('');
  const allowed = can(session, 'sandbox.run');
  const version = versions.find((item) => item.id === versionId);

  useEffect(() => {
    if (versionState !== 'ready') { setVersionId(''); return; }
    const preferred = versions.find((item) => item.state === 'draft') ?? versions[0];
    setVersionId(preferred?.id ?? '');
  }, [versions, versionState]);

  useEffect(() => { setInputText(JSON.stringify(exampleFromSchema(version?.input_schema), null, 2)); }, [version]);

  const loadRecent = useCallback(async () => {
    if (!can(session, 'run.read')) { setRecentState('ready'); setRecent([]); return; }
    setRecentState('loading');
    try {
      setRecent((await api.runs({ kind: 'sandbox', agent_key: agentKey, limit: 8 })).items);
      setRecentState('ready');
    } catch (err) {
      setRecentError(errorText(err));
      setRecentState('error');
    }
  }, [agentKey, session]);
  useEffect(() => { void loadRecent(); }, [loadRecent]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError('');
    let input: Record<string, unknown>;
    let fixture: unknown[];
    try {
      input = parseJsonObject(inputText, 'Input');
      fixture = parseJsonArray(fixtureText, 'Evidence fixture');
    } catch (err) {
      setFormError(errorText(err));
      return;
    }
    setBusy(true);
    try {
      const run = await api.sandboxRun({ agent_key: agentKey, agent_version_id: versionId, input, evidence_fixture: fixture, idempotency_key: newIdempotencyKey('sandbox-ui') });
      setRunId(run.id);
      void loadRecent();
    } catch (err) {
      setFormError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <div className="page-heading">
        <div><p className="eyebrow">BUILD / SANDBOX</p><h1>Playground</h1><p className="page-subtitle">Jalankan versi agent di sandbox dengan input dan evidence fixture uji. Hasilnya selalu draft dan tidak memengaruhi release.</p></div>
      </div>
      <div className="scope-banner" role="note"><span className="scope-mark" aria-hidden="true">i</span><div><strong>Sandbox · draft only</strong><p>Sandbox run memakai evidence fixture yang Anda isi, bukan sumber KMS. Agent hanya menghasilkan draft; tidak ada keputusan approval dari agent.</p></div></div>

      <section className="provider-layout" aria-label="Sandbox run">
        <div className="panel records-panel">
          <div className="panel-heading"><div><h2>Hasil sandbox run</h2><p>Status diperbarui otomatis sampai run selesai.</p></div>{runId && <span className="count">Run aktif</span>}</div>
          {runId ? <RunDetailView runId={runId} session={session} onChanged={(run) => setRecent((items) => items.map((item) => item.id === run.id ? run : item))} /> : <div className="records-state">Belum ada run. Pilih agent dan versi, lalu jalankan sandbox.</div>}
        </div>

        <form className="panel form-panel" onSubmit={submit}>
          <div className="panel-heading"><div><h2>Sandbox run</h2><p>Input divalidasi terhadap input schema versi terpilih oleh runtime.</p></div></div>
          {agentState === 'error' && <div className="records-state error-state" role="alert">{agentError}</div>}
          <AgentSelect agents={agents} value={agentKey} onChange={(key) => { setAgentKey(key); setRunId(''); }} disabled={agentState !== 'ready' || busy} />
          <VersionSelect versions={versions} value={versionId} onChange={setVersionId} disabled={!agentKey || busy} state={versionState} error={versionError} />
          {version && <details className="detail-disclosure"><summary className="catalog-summary">Input schema v{version.semver}</summary><JsonBlock value={version.input_schema} /></details>}
          <label className="field"><span>Input (JSON object)</span><textarea value={inputText} onChange={(event) => setInputText(event.target.value)} rows={7} spellCheck={false} disabled={busy} /><small>Kerangka dibuat dari field wajib di input schema; isi dengan skenario uji.</small></label>
          <label className="field"><span>Evidence fixture (JSON array) <span className="status-badge status-fixture">Contoh data uji</span></span><textarea value={fixtureText} onChange={(event) => setFixtureText(event.target.value)} rows={10} spellCheck={false} disabled={busy} /><small>Contoh yang dapat diubah, bukan sumber tenant. Field: source_id, source_version, title, snippet, trust_status (approved, unverified, customer_source, expired, conflicted), publication_status, anchor.</small></label>
          <div className="catalog-actions">
            <button className="button" type="button" disabled={busy} onClick={() => setFixtureText(JSON.stringify(exampleFixture, null, 2))}>Reset contoh</button>
            <button className="button" type="button" disabled={busy} onClick={() => setFixtureText('[]')}>Tanpa evidence</button>
          </div>
          <button className="button primary full" type="submit" disabled={!allowed || busy || !agentKey || !versionId} style={{ marginTop: 14 }}>{busy ? 'Mengirim…' : 'Jalankan sandbox run'}</button>
          <PermissionNote session={session} permission="sandbox.run" action="Sandbox run" />
          {formError && <div className="alert error" role="alert" style={{ marginTop: 12 }}>{formError}</div>}
        </form>
      </section>

      <section className="panel deployments-panel" aria-label="Recent sandbox runs">
        <div className="panel-heading"><div><h2>Sandbox run terbaru</h2><p>{agentKey ? `Untuk ${agentKey}.` : 'Semua agent.'} Klik untuk membuka detail.</p></div><button className="button" type="button" onClick={() => void loadRecent()} disabled={recentState === 'loading'}>Muat ulang</button></div>
        {!can(session, 'run.read') ? <PermissionNote session={session} permission="run.read" action="Daftar run" /> : <RecordsState state={recentState} message={recentError} empty="Belum ada sandbox run." count={recent.length} />}
        {recentState === 'ready' && recent.length > 0 && <div className="provider-list">{recent.map((run) => (
          <article className={`provider-row wrap-row ${run.id === runId ? 'selected-row' : ''}`} key={run.id}>
            <div className="record-main"><strong>{run.agent_key} · v{run.agent_semver}</strong><span>{formatDate(run.created_at)} · {run.caller_subject}</span></div>
            <StatusBadge value={run.status} />
            <ModelModeBadge mode={run.model_mode} />
            <button className="button" type="button" onClick={() => setRunId(run.id)}>Lihat</button>
          </article>
        ))}</div>}
      </section>
    </>
  );
}
