import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { Agent, AgentPage, Deployment, IntegrationStatus, Page, Provider, RunMetrics, Session, api } from '../api';
import { PermissionNote, StatusBadge, can, statusLabel } from './shared';

type LoadState = 'loading' | 'ready' | 'error';
type Summary<T> = { state: LoadState; value?: T; error?: string };

function errorText(error: unknown) {
  return error instanceof Error ? error.message : 'Registry API tidak tersedia.';
}

function useSummary<T>(loader: () => Promise<T>) {
  const [summary, setSummary] = useState<Summary<T>>({ state: 'loading' });
  const refresh = useCallback(async () => {
    setSummary({ state: 'loading' });
    try {
      setSummary({ state: 'ready', value: await loader() });
    } catch (error) {
      setSummary({ state: 'error', error: errorText(error) });
    }
  }, [loader]);
  useEffect(() => { void refresh(); }, [refresh]);
  return [summary, refresh] as const;
}

function SummaryCard<T>({ title, state, error, value, children }: {
  title: string;
  state: LoadState;
  error?: string;
  value?: T;
  children: (value: T) => ReactNode;
}) {
  return (
    <section className="panel records-panel">
      <div className="panel-heading"><div><h2>{title}</h2></div><span className="count">{state === 'ready' ? 'Terhubung' : state === 'loading' ? 'Memuat' : 'Tidak tersedia'}</span></div>
      {state === 'loading' ? <div className="records-state" role="status">Memuat dari registry…</div> : state === 'error' ? <div className="records-state error-state" role="alert">{error}</div> : value === undefined ? <div className="records-state">Belum ada data.</div> : children(value)}
    </section>
  );
}

function percent(value: number | null) {
  return value === null || value === undefined ? '—' : `${Math.round(value * 100)}%`;
}

function millis(value: number | null) {
  if (value === null || value === undefined) return '—';
  return value < 1000 ? `${Math.round(value)} ms` : `${(value / 1000).toFixed(1)} s`;
}

function Metric({ label, value, hint, tone }: { label: string; value: ReactNode; hint?: string; tone?: 'warn' | 'bad' }) {
  return <div className={`metric ${tone ? `metric-${tone}` : ''}`}><span>{label}</span><strong>{value}</strong>{hint && <small>{hint}</small>}</div>;
}

function MetricsPanel({ metrics }: { metrics: RunMetrics }) {
  const statusOrder = ['queued', 'running', 'succeeded', 'needs_review', 'failed', 'cancelled'];
  const totalRuns = Object.values(metrics.by_status ?? {}).reduce((sum, value) => sum + value, 0);
  return (
    <>
      <div className="metric-grid">
        <Metric label={`Run (${metrics.window_hours} jam)`} value={totalRuns} hint={Object.entries(metrics.by_kind ?? {}).map(([kind, count]) => `${kind} ${count}`).join(' · ') || 'Belum ada run'} />
        <Metric label="Durasi p50 / p95" value={`${millis(metrics.p50_duration_ms)} / ${millis(metrics.p95_duration_ms)}`} />
        <Metric label="Grounded rate" value={percent(metrics.grounded_rate)} hint="Run tanpa klaim tak didukung atau sitasi tak dikenal" />
        <Metric label="Token in / out" value={`${metrics.tokens_in.toLocaleString()} / ${metrics.tokens_out.toLocaleString()}`} />
        <Metric label="Tool denials" value={metrics.tool_denials} tone={metrics.tool_denials ? 'warn' : undefined} />
        <Metric label="Evaluation selesai" value={metrics.eval_runs_completed} hint={`${metrics.eval_gate_blocked} gate blocked`} tone={metrics.eval_gate_blocked ? 'warn' : undefined} />
        <Metric label="Release aktif" value={metrics.active_releases} hint={`${metrics.pending_release_requests} permintaan menunggu · ${metrics.rollbacks} rollback`} />
        <Metric label="Outbox" value={`${metrics.outbox_pending} pending`} hint={`${metrics.outbox_dead} dead`} tone={metrics.outbox_dead ? 'bad' : undefined} />
      </div>
      <div className="status-strip" aria-label="Run per status">
        {statusOrder.filter((status) => metrics.by_status?.[status] !== undefined).map((status) => <span key={status}><StatusBadge value={status} /> {metrics.by_status[status]}</span>)}
        {Object.keys(metrics.by_status ?? {}).length === 0 && <span className="muted-text">Belum ada run pada jendela ini.</span>}
      </div>
    </>
  );
}

function IntegrationPanel({ status }: { status: IntegrationStatus }) {
  const config = status.configuration;
  return (
    <>
      {config.dev_mock_model && <div className="alert mock-alert" role="note"><strong>Dev mock model aktif — bukan model nyata.</strong> Run dan evaluation dapat memakai model tiruan; gate evaluation akan diblokir.</div>}
      <dl className="detail-grid">
        <div><dt>Worker</dt><dd>{config.worker_enabled ? <StatusBadge value="active" label="aktif" /> : <StatusBadge value="failed" label="nonaktif" />}</dd></div>
        <div><dt>KMS</dt><dd>{statusLabel(config.kms_mode)}</dd></div>
        <div><dt>Callback Flowcase</dt><dd>{config.flowcase_callback_configured ? 'Terkonfigurasi' : 'Belum dikonfigurasi'}</dd></div>
        <div><dt>Service clients</dt><dd>{config.service_clients}</dd></div>
        <div><dt>Release environment</dt><dd>{config.release_environment}</dd></div>
        <div><dt>Retensi trace</dt><dd>{config.trace_retention_days} hari</dd></div>
        <div><dt>Queue depth</dt><dd>{Object.entries(status.queue_depth ?? {}).map(([key, value]) => `${key} ${value}`).join(' · ') || '—'}</dd></div>
        <div><dt>Adapter</dt><dd><code>{config.provider_adapter}</code></dd></div>
      </dl>
    </>
  );
}

export default function Overview({ onOpenAgent, session }: { onOpenAgent: (key: string) => void; session: Session | null }) {
  const canMetrics = can(session, 'metrics.read');
  const loadMetrics = useCallback(() => canMetrics ? api.metrics(24) : Promise.reject(new Error('Sesi ini tidak memiliki izin metrics.read.')), [canMetrics]);
  const loadIntegrations = useCallback(() => canMetrics ? api.integrationStatus() : Promise.reject(new Error('Sesi ini tidak memiliki izin metrics.read.')), [canMetrics]);
  const [metrics, refreshMetrics] = useSummary(loadMetrics);
  const [integrations, refreshIntegrations] = useSummary(loadIntegrations);
  const loadAgents = useCallback(() => api.agents(5, 0), []);
  const loadProviders = useCallback(() => api.providers(), []);
  const loadDeployments = useCallback(() => api.deployments(), []);
  const [agents, refreshAgents] = useSummary(loadAgents);
  const [providers, refreshProviders] = useSummary(loadProviders);
  const [deployments, refreshDeployments] = useSummary(loadDeployments);
  const states = [agents.state, providers.state, deployments.state];
  const unavailable = states.filter((state) => state === 'error').length;
  const loading = states.some((state) => state === 'loading');

  return (
    <>
      <div className="page-heading">
        <div><p className="eyebrow">WORKSPACE / LIVE REGISTRY</p><h1>Overview</h1><p className="page-subtitle">Ringkasan hanya menggunakan data yang berhasil dibaca dari API tenant.</p></div>
        <button className="button secondary" type="button" onClick={() => { void refreshAgents(); void refreshProviders(); void refreshDeployments(); void refreshMetrics(); void refreshIntegrations(); }} disabled={loading}>Muat ulang</button>
      </div>
      {unavailable > 0 && <div className="alert error" role="alert">{unavailable} sumber registry tidak tersedia. Ringkasan untuk sumber tersebut disembunyikan, tanpa data contoh.</div>}
      <section className="provider-layout overview-cards" aria-label="Registry summaries">
        <SummaryCard<AgentPage> title="Agents" state={agents.state} error={agents.error} value={agents.value}>
          {(result) => <><p className="overview-value" style={{ color: 'var(--ink)', fontSize: 32, fontWeight: 750, lineHeight: 1, marginBottom: 0 }}>{result.total}</p><p className="footer-note">Identitas tenant. API mengembalikan {result.items.length} record terbaru pada halaman pertama.</p></>}
        </SummaryCard>
        <SummaryCard<Page<Provider>> title="Providers" state={providers.state} error={providers.error} value={providers.value}>
          {(page) => <><p className="overview-value" style={{ color: 'var(--ink)', fontSize: 32, fontWeight: 750, lineHeight: 1, marginBottom: 0 }}>{page.total}</p><p className="footer-note">Metadata provider tenant; {page.items.filter((item) => item.status === 'pending_test').length} pada halaman ini menunggu pengujian.</p></>}
        </SummaryCard>
        <SummaryCard<Page<Deployment>> title="Model deployments" state={deployments.state} error={deployments.error} value={deployments.value}>
          {(page) => <><p className="overview-value" style={{ color: 'var(--ink)', fontSize: 32, fontWeight: 750, lineHeight: 1, marginBottom: 0 }}>{page.total}</p><p className="footer-note">Metadata deployment; {page.items.filter((item) => item.status === 'pending_test').length} pada halaman ini menunggu pengujian.</p></>}
        </SummaryCard>
      </section>

      <section className="lower-grid" aria-label="Runtime metrics">
        <section className="panel records-panel">
          <div className="panel-heading"><div><h2>Runtime metrics · 24 jam</h2><p>Dihitung oleh API dari run, evaluation, dan release tenant.</p></div><span className="count">{metrics.state === 'ready' ? 'Terhubung' : metrics.state === 'loading' ? 'Memuat' : 'Tidak tersedia'}</span></div>
          {!canMetrics ? <PermissionNote session={session} permission="metrics.read" action="Metrics" /> : metrics.state === 'loading' ? <div className="records-state" role="status">Memuat metrics…</div> : metrics.state === 'error' ? <div className="records-state error-state" role="alert">{metrics.error}</div> : metrics.value && <MetricsPanel metrics={metrics.value} />}
        </section>
        <section className="panel records-panel">
          <div className="panel-heading"><div><h2>Integrasi &amp; antrean</h2><p>Konfigurasi deployment yang dilaporkan server.</p></div></div>
          {!canMetrics ? <PermissionNote session={session} permission="metrics.read" action="Status integrasi" /> : integrations.state === 'loading' ? <div className="records-state" role="status">Memuat status…</div> : integrations.state === 'error' ? <div className="records-state error-state" role="alert">{integrations.error}</div> : integrations.value && <IntegrationPanel status={integrations.value} />}
        </section>
      </section>

      <section className="panel deployments-panel">
        <div className="panel-heading"><div><h2>Recent agents</h2><p>Data identitas terbaru dari tenant registry.</p></div><span className="count">{agents.state === 'ready' ? `${agents.value?.items.length ?? 0} ditampilkan` : 'Belum dimuat'}</span></div>
        {agents.state === 'loading' ? <div className="records-state" role="status">Memuat agent…</div> : agents.state === 'error' ? <div className="records-state error-state" role="alert">{agents.error}</div> : agents.value?.items.length ? (
          <div className="provider-list">{agents.value.items.map((agent: Agent) => <article className="provider-row" key={agent.id}><span className="provider-glyph" aria-hidden="true">A</span><span className="record-main"><strong>{agent.display_name}</strong><span>{agent.key} · {agent.domain}</span><small>Status: {agent.lifecycle_status.replaceAll('_', ' ')}</small></span><button className="button" type="button" onClick={() => onOpenAgent(agent.key)}>Buka detail</button></article>)}</div>
        ) : <div className="records-state">Belum ada agent di tenant ini.</div>}
      </section>
    </>
  );
}
