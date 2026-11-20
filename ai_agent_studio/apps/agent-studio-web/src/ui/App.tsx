import { FormEvent, useCallback, useEffect, useState } from 'react';
import { api, appBasePath, Agent, CredentialStatus, Deployment, ModelApproval, Provider, Session, SIMPackResult } from '../api';
import CatalogWorkbench from './CatalogWorkbench';
import Overview from './Overview';
import AgentDetail from './AgentDetail';
import Playground from './Playground';
import Evaluations from './Evaluations';
import Releases from './Releases';
import Runs from './Runs';
import { TableScroll, PermissionNote, RecordsState, StatusBadge, can } from './shared';

const navigation = [
  { id: 'overview', label: 'Overview' },
  { id: 'agents', label: 'Agent Registry' },
  { id: 'skills', label: 'Skills Studio' },
  { id: 'tools', label: 'Tool Library' },
  { id: 'playground', label: 'Playground' },
  { id: 'evaluations', label: 'Evaluations' },
  { id: 'releases', label: 'Releases' },
  { id: 'runs', label: 'Runs & Audit' },
  { id: 'models', label: 'Models & Providers' },
] as const;

type Page = (typeof navigation)[number]['id'];
type LoadState = 'loading' | 'ready' | 'error';
type WorkspaceContext = {
  user: {id: string; display_name: string};
  workspace: {name: string} | null;
  apps: {app_key: string; route_base: string; status: string; integration: string}[];
};

function statusLabel(value: string) {
  return value.replaceAll('_', ' ');
}

const simOutcomeLabels: Record<string, string> = {
  configured: 'dikonfigurasi',
  already_configured: 'sudah dikonfigurasi',
  no_unconfigured_draft: 'tidak ada draft kosong',
};

function providerHasValidHTTPSURL(value: string) {
  try {const url=new URL(value);return url.protocol==='https:'&&!url.username&&!url.password&&!url.search&&!url.hash;}catch{return false;}
}
function pageFromHash():Page {const value=window.location.hash.slice(1);return navigation.some(item=>item.id===value)?value as Page:'models';}

function messageFor(error: unknown) {
  return error instanceof Error ? error.message : 'Permintaan gagal. Coba lagi.';
}

export default function App() {
  const [page, setCurrentPage] = useState<Page>(pageFromHash);
  function setPage(value:Page){setCurrentPage(value);window.history.replaceState(null,'',`#${value}`);}
  useEffect(()=>{const update=()=>setCurrentPage(pageFromHash());window.addEventListener('hashchange',update);return()=>window.removeEventListener('hashchange',update)},[]);
  const [session, setSession] = useState<Session | null>(null);
  const [workspaceContext, setWorkspaceContext] = useState<WorkspaceContext | null>(null);
  const [selectedAgentKey, setSelectedAgentKey] = useState('');
  const [mobileOpen, setMobileOpen] = useState(false);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [providerTotal, setProviderTotal] = useState(0);
  const [deploymentTotal, setDeploymentTotal] = useState(0);
  const [providerOffset, setProviderOffset] = useState(0);
  const [deploymentOffset, setDeploymentOffset] = useState(0);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [agentTotal, setAgentTotal] = useState(0);
  const [agentOffset, setAgentOffset] = useState(0);
  const [providerState, setProviderState] = useState<LoadState>('loading');
  const [deploymentState, setDeploymentState] = useState<LoadState>('loading');
  const [agentState, setAgentState] = useState<LoadState>('loading');
  const [providerError, setProviderError] = useState('');
  const [deploymentError, setDeploymentError] = useState('');
  const [agentError, setAgentError] = useState('');
  const [notice, setNotice] = useState('');
  const [saving, setSaving] = useState(false);
  const [credentialStatuses, setCredentialStatuses] = useState<Record<string, CredentialStatus>>({});
  const [credentialStatusErrors, setCredentialStatusErrors] = useState<Record<string, string>>({});
  const [approvalDeploymentId, setApprovalDeploymentId] = useState('');
  const [approvalEnvironment, setApprovalEnvironment] = useState<ModelApproval['environment']>('development');
  const [approvalState, setApprovalState] = useState<LoadState | 'idle'>('idle');
  const [approvals, setApprovals] = useState<ModelApproval[]>([]);
  const [approvalError, setApprovalError] = useState('');
  const [approvalReason, setApprovalReason] = useState('');
  const [approvalNotice, setApprovalNotice] = useState('');
  const [probeProviderId, setProbeProviderId] = useState('');
  const [probeDeploymentId, setProbeDeploymentId] = useState('');
  const [probeJson, setProbeJson] = useState(false);
  const [probeTools, setProbeTools] = useState(false);
  const [probeCostAcknowledged, setProbeCostAcknowledged] = useState(false);
  const [probeState, setProbeState] = useState<LoadState | 'idle'>('idle');
  const [probeResult, setProbeResult] = useState<Awaited<ReturnType<typeof api.probeProvider>> | null>(null);
  const [probeError, setProbeError] = useState('');
  const [simPackResult, setSimPackResult] = useState<SIMPackResult | null>(null);

  const refresh = useCallback(async () => {
    setProviderState('loading');
    setDeploymentState('loading');
    setProviderError('');
    setDeploymentError('');
    const [providerResult, deploymentResult] = await Promise.allSettled([
      api.providers(100, providerOffset), api.deployments(100, deploymentOffset),
    ]);
    if (providerResult.status === 'fulfilled') {
      setProviders(providerResult.value.items);
      setProviderTotal(providerResult.value.total);
      setProviderState('ready');
      const statusResults = await Promise.allSettled(providerResult.value.items.map((provider) => api.providerCredentialStatus(provider.id)));
      const statuses: Record<string, CredentialStatus> = {};
      const statusErrors: Record<string, string> = {};
      statusResults.forEach((result, index) => {
        const provider = providerResult.value.items[index];
        if (result.status === 'fulfilled') statuses[provider.id] = result.value;
        else statusErrors[provider.id] = messageFor(result.reason);
      });
      setCredentialStatuses(statuses);
      setCredentialStatusErrors(statusErrors);
    } else {
      setProviderError(messageFor(providerResult.reason));
      setProviderState('error');
    }
    if (deploymentResult.status === 'fulfilled') {
      setDeployments(deploymentResult.value.items);
      setDeploymentTotal(deploymentResult.value.total);
      setDeploymentState('ready');
    } else {
      setDeploymentError(messageFor(deploymentResult.reason));
      setDeploymentState('error');
    }
  }, [providerOffset, deploymentOffset]);

  const refreshAgents = useCallback(async () => {
    setAgentState('loading');
    setAgentError('');
    try {
      const result = await api.agents(25, agentOffset);
      setAgents(result.items);
      setAgentTotal(result.total);
      setAgentState('ready');
    } catch (error) {
      setAgentError(messageFor(error));
      setAgentState('error');
    }
  }, [agentOffset]);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => { api.session().then(setSession, () => setSession(null)); }, []);
  useEffect(() => {
    if (appBasePath !== '/apps/agent-studio') return;
    const controller = new AbortController();
    fetch('/api/v1/me', {credentials: 'same-origin', cache: 'no-store', signal: controller.signal})
      .then(async response => {
        if (!response.ok) throw new Error('Workspace session unavailable');
        const data: WorkspaceContext = await response.json();
        if (typeof data.user?.id !== 'string' || typeof data.user?.display_name !== 'string' || !Array.isArray(data.apps) || data.apps.length > 1000 || !data.apps.every(app => app && typeof app.app_key === 'string' && typeof app.route_base === 'string' && typeof app.status === 'string' && typeof app.integration === 'string')) throw new Error('Invalid workspace context');
        if (!controller.signal.aborted) setWorkspaceContext(data);
      }).catch(() => { if (!controller.signal.aborted) setWorkspaceContext(null); });
    return () => controller.abort();
  }, []);
  useEffect(() => { if (page === 'agents') void refreshAgents(); }, [page, refreshAgents]);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMobileOpen(false);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  const apiReady = providerState === 'ready' && deploymentState === 'ready';
  const apiLoading = providerState === 'loading' || deploymentState === 'loading';
  const heading = navigation.find((item) => item.id === page)?.label ?? 'Agent Studio';
  const selectedAgent = agents.find((agent) => agent.key === selectedAgentKey);
  const sharedIdentity = session?.identity_mode === 'control-plane' && session.issuer === window.location.origin && workspaceContext?.user.id === session.subject;
  const workspaceName = sharedIdentity && typeof workspaceContext?.workspace?.name === 'string' ? workspaceContext.workspace.name : session?.identity_mode === 'development' ? 'Development workspace' : 'Workspace session';
  const userName = sharedIdentity ? workspaceContext?.user.display_name : 'Verified service identity';
  const engineeringHome = sharedIdentity && workspaceContext?.apps.some(app => app.app_key === 'flowcase' && app.route_base === '/apps/flowcase/' && app.status === 'published' && app.integration === 'proxy') ? '/apps/flowcase/elenio.html#/connected' : '/';

  useEffect(() => { document.title = `${heading} · Agent Studio`; }, [heading]);

  async function submitProvider(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    setNotice('');
    setSaving(true);
    const data = new FormData(form);
    const input = {
      key: String(data.get('providerKey')).trim(),
      display_name: String(data.get('providerName')).trim(),
      adapter_type: String(data.get('adapterType')),
      base_url: String(data.get('baseUrl')).trim(),
    };
    try {
      await api.createProvider(input);
      form.reset();
      setNotice('Provider tersimpan dengan status pending test.');
      await refresh();
    } catch (error) {
      setNotice(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  async function submitDeployment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    setNotice('');
    setSaving(true);
    const data = new FormData(form);
    const input = {
      provider_id: String(data.get('providerId')),
      key: String(data.get('deploymentKey')).trim(),
      provider_model_id: String(data.get('modelId')).trim(),
      display_name: String(data.get('modelName')).trim(),
      capabilities: {
        tools: data.get('tools') === 'on',
        json: data.get('json') === 'on',
      },
      max_output_tokens: Number(data.get('maxTokens')),
      timeout_ms: Number(data.get('timeoutSeconds')) * 1000,
      cost_policy: {},
    };
    try {
      await api.createDeployment(input);
      form.reset();
      setNotice('Model tersimpan dengan status pending test.');
      await refresh();
    } catch (error) {
      setNotice(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  async function submitCredential(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const providerId = String(new FormData(form).get('credentialProvider'));
    const input = form.elements.namedItem('credential') as HTMLInputElement | null;
    let credential = input?.value ?? '';
    setNotice('');
    setSaving(true);
    try {
      if (!credential) throw new Error('Masukkan kredensial provider.');
      const result = await api.setProviderCredential(providerId, credential);
      setCredentialStatuses((current) => ({ ...current, [providerId]: result }));
      setCredentialStatusErrors((current) => { const next = { ...current }; delete next[providerId]; return next; });
      setNotice(result.previous_secret_cleanup_pending ? 'Kredensial tersimpan. Pembersihan secret sebelumnya masih tertunda.' : 'Kredensial tersimpan di vault backend. Nilainya tidak dapat dibaca kembali.');
      await refresh();
    } catch (error) {
      setNotice(messageFor(error));
    } finally {
      credential = '';
      if (input) input.value = '';
      form.reset();
      setSaving(false);
    }
  }

  async function removeCredential(provider: Provider) {
    if (!window.confirm(`Hapus kredensial untuk ${provider.display_name}?`)) return;
    setNotice('');
    setSaving(true);
    try {
      const result = await api.deleteProviderCredential(provider.id);
      setCredentialStatuses((current) => ({ ...current, [provider.id]: result }));
      setCredentialStatusErrors((current) => { const next = { ...current }; delete next[provider.id]; return next; });
      setNotice(result.vault_cleanup_pending ? 'Referensi kredensial dihapus. Pembersihan record vault masih tertunda.' : 'Referensi kredensial dihapus dari provider.');
      await refresh();
    } catch (error) {
      setNotice(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  async function decideApproval(state: 'approved' | 'revoked') {
    if (!approvalDeploymentId || !approvalReason.trim()) return;
    setSaving(true);
    setApprovalNotice('');
    try {
      await api.setModelApproval(approvalDeploymentId, approvalEnvironment, state, approvalReason.trim());
      setApprovalReason('');
      setApprovalNotice(state === 'approved' ? `Deployment di-approve untuk ${approvalEnvironment}.` : `Approval ${approvalEnvironment} dicabut; run baru dengan deployment ini ditolak.`);
      await loadApprovals();
    } catch (error) {
      setApprovalNotice(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  async function loadApprovals() {
    if (!approvalDeploymentId) return;
    setApprovalState('loading');
    setApprovalError('');
    try {
      setApprovals(await api.modelApprovals(approvalDeploymentId, approvalEnvironment));
      setApprovalState('ready');
    } catch (error) {
      setApprovals([]);
      setApprovalError(messageFor(error));
      setApprovalState('error');
    }
  }

  async function submitProviderProbe(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const provider = providers.find((item) => item.id === probeProviderId);
    if (!provider || !probeDeploymentId || !credentialStatuses[provider.id]?.credential_configured || !providerHasValidHTTPSURL(provider.base_url)) return;
    const costsTokens = probeJson || probeTools;
    if (costsTokens && !probeCostAcknowledged) return;
    if (costsTokens && !probeJson && !probeTools) return;
    setProbeState('loading');
    setProbeError('');
    setProbeResult(null);
    try {
      const result = await api.probeProvider(provider.id, {
        deployment_id: probeDeploymentId,
        mode: costsTokens ? 'capabilities' : 'connectivity',
        ...(costsTokens ? { capabilities: { ...(probeJson ? { json: true } : {}), ...(probeTools ? { tools: true } : {}) } } : {}),
        confirm_token_cost: costsTokens && probeCostAcknowledged,
      });
      setProbeResult(result);
      setProbeState('ready');
    } catch (error) {
      setProbeError(messageFor(error));
      setProbeState('error');
    }
  }

  async function submitAgent(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setNotice('');
    setSaving(true);
    try {
      await api.createAgent({
        key: String(data.get('agentKey')).trim(),
        display_name: String(data.get('agentName')).trim(),
        domain: String(data.get('agentDomain')).trim(),
      });
      form.reset();
      setNotice('Draft agent berhasil dibuat.');
      await refreshAgents();
    } catch (error) {
      setNotice(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  async function bootstrapSIM() {
    setSaving(true);
    setNotice('');
    try {
      const result = await api.bootstrapSIM();
      setNotice(`${result.agents.length} SIM identity templates tersedia. SME dan tool grants belum ditetapkan.`);
      await refreshAgents();
    } catch (error) {
      setNotice(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  async function applySIMPack() {
    setSaving(true);
    setNotice('');
    setSimPackResult(null);
    try {
      const result = await api.applySIMPack();
      setSimPackResult(result);
      setNotice(`SIM pack ${result.pack_version} diterapkan ke draft yang belum dikonfigurasi. Tidak ada yang dipublish atau disetujui.`);
      await refreshAgents();
    } catch (error) {
      setNotice(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  async function archiveAgent(agent: Agent) {
    setSaving(true);
    setNotice('');
    try {
      await api.archiveAgent(agent.key);
      setNotice(`Agent ${agent.key} diarsipkan.`);
      await refreshAgents();
    } catch (error) {
      setNotice(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="app-shell">
      {mobileOpen && <button className="scrim" aria-label="Tutup menu navigasi" onClick={() => setMobileOpen(false)} />}
      <aside className={`sidebar ${mobileOpen ? 'is-open' : ''}`} aria-label="Navigasi utama">
        <a className="brand" href={appBasePath ? engineeringHome : '#overview'} aria-label="Elenio AI OS"><img src={`${import.meta.env.BASE_URL}brand/elenio-logo.png`} alt="Elenio"/><span>AI OS</span></a>
        {appBasePath ? <a className="workspace" href="/" aria-label={`Workspace: ${workspaceName}. Open Control Plane to change workspace`}><span className="workspace-mark" aria-hidden="true">E</span><span><strong>{workspaceName}</strong><small>{userName}</small></span></a> : <div className="workspace"><span className="workspace-mark" aria-hidden="true">E</span><span><strong>{workspaceName}</strong><small>{userName}</small></span></div>}
        {appBasePath && <a className="cp-back" href={engineeringHome}><span aria-hidden="true">←</span> Solution engineering</a>}
        <p className="nav-title">Agent engineering</p>
        <nav id="primary-nav" className="nav-list">
          {navigation.map((item) => (
            <button key={item.id} className={`nav-item ${page === item.id ? 'active' : ''}`} aria-current={page === item.id ? 'page' : undefined} onClick={() => { setPage(item.id); setSelectedAgentKey(''); setMobileOpen(false); }}>
              <span className="nav-indicator" aria-hidden="true" />{item.label}
            </button>
          ))}
        </nav>
        <div className="side-note"><span className="status-dot" />Tenant scope comes from the verified host session.</div>
      </aside>

      <main className="main-content">
        <header className="topbar">
          <button className="menu-button" aria-label="Buka menu navigasi" aria-controls="primary-nav" aria-expanded={mobileOpen} onClick={() => setMobileOpen(true)}><span /><span /><span /></button>
          <div className="breadcrumbs"><span>Agent Studio</span><span aria-hidden="true">/</span><strong>{heading}</strong></div>
          <div className="topbar-spacer" />
          <span className={`api-chip ${apiReady ? 'connected' : apiLoading ? 'pending' : 'unavailable'}`}><span className="status-dot" />{apiReady ? 'Registry API aktif' : apiLoading ? 'Memeriksa API' : 'API belum tersedia'}</span>
          {session?.identity_mode === 'development' && <span className="api-chip dev-identity" title={`Tanpa login: semua akses memakai identitas ${session.subject}`}><span className="status-dot" />Mode dev · tanpa login</span>}
          <span className="avatar" aria-label="Akun dikelola host" title={userName}>{userName?.slice(0, 1).toUpperCase() ?? 'H'}</span>
        </header>

        <div className="page-content">
          {page === 'overview' ? (
            <Overview session={session} onOpenAgent={(key) => { setSelectedAgentKey(key); setPage('agents'); }} />
          ) : page === 'agents' ? selectedAgentKey ? (
            agentState === 'loading' ? <RecordsState state="loading" message="" empty="" /> : agentState === 'error' ? <RecordsState state="error" message={agentError} empty="" /> : selectedAgent ? <AgentDetail agent={selectedAgent} onBack={() => { setSelectedAgentKey(''); void refreshAgents(); }} /> : <section className="not-ready panel"><p>Agent tidak ditemukan pada halaman registry yang dimuat.</p><button className="button secondary" onClick={() => setSelectedAgentKey('')}>Kembali ke registry</button></section>
          ) : (
            <>
              <div className="page-heading">
                <div><p className="eyebrow">AUTHORING / TENANT REGISTRY</p><h1>Agent Registry</h1><p className="page-subtitle">Identitas dan draft agent yang tersimpan untuk tenant terverifikasi. Data SME dan izin tool memerlukan persetujuan terpisah.</p></div>
                <button className="button secondary" onClick={() => void refreshAgents()} disabled={agentState === 'loading' || saving}>Muat ulang</button>
              </div>
              {notice && <div className={`alert ${notice.includes('berhasil') || notice.includes('tersedia') || notice.includes('diarsipkan') || notice.includes('diterapkan') ? 'success' : 'error'}`} role="status">{notice}</div>}
              <div className="scope-banner" role="note"><span className="scope-mark" aria-hidden="true">i</span><div><strong>Draft only</strong><p>Agent baru mulai sebagai draft 0.1.0. Tidak ada model, skill, tool grant, atau pemilik SME yang otomatis ditetapkan.</p></div></div>
              <section className="provider-layout" aria-label="Agent registry">
                <div className="panel records-panel">
                  <div className="panel-heading"><div><h2>Agents</h2><p>Daftar identitas agent di tenant.</p></div><span className="count">{agentState === 'ready' ? `${agentTotal} agents` : 'Belum dimuat'}</span></div>
                  <RecordsState state={agentState} message={agentError} empty="Belum ada agent di tenant ini." count={agents.length} />
                  {agentState === 'ready' && agents.length > 0 && <div className="provider-list">{agents.map((agent) => <article className="provider-row" style={{ flexWrap: 'wrap' }} key={agent.id}><div className="provider-glyph" aria-hidden="true">A</div><div className="record-main"><strong>{agent.display_name}</strong><span>{agent.key} · {agent.domain}</span><small>Status: {statusLabel(agent.lifecycle_status)} · Owner: {agent.owner_subject ?? 'belum ditetapkan'}</small></div><StatusBadge value={agent.lifecycle_status} /><button className="button" style={{ minHeight: 44 }} type="button" onClick={() => setSelectedAgentKey(agent.key)}>Detail</button>{agent.lifecycle_status !== 'archived' && <button className="button" style={{ minHeight: 44 }} type="button" disabled={saving} onClick={() => void archiveAgent(agent)}>Arsipkan</button>}</article>)}</div>}
                  {agentState === 'ready' && agentTotal > 0 && <div className="pagination"><p className="footer-note">Menampilkan {agentOffset + 1} sampai {agentOffset + agents.length} dari {agentTotal} agent.</p><div><button className="button" type="button" disabled={agentOffset === 0 || saving} onClick={() => setAgentOffset(Math.max(0, agentOffset - 25))}>Sebelumnya</button><button className="button" type="button" disabled={agentOffset + agents.length >= agentTotal || saving} onClick={() => setAgentOffset(agentOffset + 25)}>Berikutnya</button></div></div>}
                </div>
                <div className="panel form-panel">
                  <form onSubmit={submitAgent}>
                    <div className="panel-heading"><div><h2>Buat agent draft</h2><p>Versi awal 0.1.0 dibuat bersama record.</p></div></div>
                    <label className="field"><span>Nama agent</span><input name="agentName" maxLength={200} required disabled={saving || agentState !== 'ready'} /></label>
                    <label className="field"><span>Agent key</span><input name="agentKey" pattern="[a-z][a-z0-9_.\-]{0,62}" maxLength={63} required disabled={saving || agentState !== 'ready'} /><small>Huruf kecil, angka, titik, garis bawah, atau tanda hubung.</small></label>
                    <label className="field"><span>Domain</span><input name="agentDomain" maxLength={100} required disabled={saving || agentState !== 'ready'} /></label>
                    <button className="button primary full" type="submit" disabled={saving || agentState !== 'ready'}>Buat draft agent</button>
                  </form>
                  <details className="domain-pack">
                  <summary>SIM domain pack</summary>
                  <p className="form-hint">Buat tujuh identitas SIM yang kosong dan belum berwenang: enam spesialis workflow dan Case Intake Assistant.</p>
                  <button className="button primary full" type="button" onClick={() => void bootstrapSIM()} disabled={agentState !== 'ready' || saving}>{saving ? 'Memproses…' : 'Bootstrap 7 SIM agents'}</button>
                  <p className="form-hint">Operasi ini idempotent. Data agent yang sudah ada tidak ditimpa.</p>
                  <button className="button full" type="button" style={{ marginTop: 12 }} onClick={() => void applySIMPack()} disabled={agentState !== 'ready' || saving || !can(session, 'agent.bootstrap')}>{saving ? 'Memproses…' : 'Apply SIM pack v1'}</button>
                  <p className="form-hint">Mengisi draft SIM yang masih kosong dengan definisi pack, draft skill, tool binding, dan suite evaluation dasar. Draft yang sudah diubah author tidak disentuh; tidak ada publish atau approval.</p>
                  <PermissionNote session={session} permission="agent.bootstrap" action="Apply SIM pack" />
                  {simPackResult && <div className="provider-list sim-pack-result" role="status">{simPackResult.agents.map((item) => <article className="provider-row" key={item.agent_key}><div className="record-main"><strong>{item.agent_key}</strong><small>{item.eval_suite_id ? 'Suite evaluation tersedia' : 'Tanpa suite baru'}</small></div><StatusBadge value={item.outcome === 'configured' ? 'succeeded' : 'superseded'} label={simOutcomeLabels[item.outcome] ?? statusLabel(item.outcome)} /></article>)}</div>}
                  </details>
                </div>
              </section>
            </>
          ) : page === 'models' ? (
            <>
              <div className="page-heading">
                <div><p className="eyebrow">PLATFORM SETTINGS / MODEL GOVERNANCE</p><h1>Models &amp; Providers</h1><p className="page-subtitle">Daftarkan metadata provider dan model untuk tenant ini. Kredensial harus masuk melalui vault backend.</p></div>
                <button className="button secondary" onClick={() => void refresh()} disabled={providerState === 'loading' || deploymentState === 'loading'}>Muat ulang</button>
              </div>

              <div className="scope-banner" role="note"><span className="scope-mark" aria-hidden="true">i</span><div><strong>Tenant-scoped registry</strong><p>API memverifikasi sesi host dan izin setiap permintaan. Provider baru berstatus <code>pending test</code>; tidak ada koneksi model yang dijalankan dari browser.</p></div></div>

              {(providerError || deploymentError) && <div className="alert error" role="alert"><strong>Registry belum siap</strong><p>{[...new Set([providerError, deploymentError].filter(Boolean))].join(' ')}</p><span>Data contoh tidak ditampilkan.</span></div>}
              {notice && <div className={`alert ${notice.includes('tersimpan') ? 'success' : 'error'}`} role="status">{notice}</div>}

              <section className="provider-layout" aria-label="Provider registry">
                <div className="panel records-panel">
                  <div className="panel-heading"><div><h2>Provider connections</h2><p>Metadata endpoint; secret dikelola terpisah melalui vault backend.</p></div><span className="count">{providerState === 'ready' ? `${providerTotal} provider` : 'Belum dimuat'}</span></div>
                  <RecordsState state={providerState} message={providerError} empty="Belum ada provider di tenant ini." count={providers.length} />
                  {providerState === 'ready' && providers.length > 0 && <div className="provider-list">{providers.map((provider) => { const credentialStatus = credentialStatuses[provider.id]; return <article className="provider-row" key={provider.id}><div className="provider-glyph" aria-hidden="true">{provider.display_name.slice(0, 1).toUpperCase()}</div><div className="record-main"><strong>{provider.display_name}</strong><span>{provider.key} · {provider.adapter_type}</span><code>{provider.base_url}</code><small>{credentialStatus ? (credentialStatus.credential_configured ? 'Credential configured; value is write-only.' : 'No credential configured.') : credentialStatusErrors[provider.id] ? `Credential status unavailable: ${credentialStatusErrors[provider.id]}` : 'Loading credential status…'}</small></div><StatusBadge value={provider.status} /><button className="button" type="button" disabled={saving} onClick={() => void removeCredential(provider)}>Hapus credential</button></article>; })}</div>}
                  {providerState === 'ready' && providerTotal > 100 && <div className="pagination"><p className="footer-note">Menampilkan {providerOffset + 1} sampai {providerOffset + providers.length} dari {providerTotal} provider.</p><div><button className="button" type="button" disabled={providerOffset === 0 || apiLoading} onClick={() => setProviderOffset(Math.max(0, providerOffset - 100))}>Sebelumnya</button><button className="button" type="button" disabled={providerOffset + providers.length >= providerTotal || apiLoading} onClick={() => setProviderOffset(providerOffset + 100)}>Berikutnya</button></div></div>}
                </div>

                <form className="panel form-panel" onSubmit={submitProvider}>
                  <div className="panel-heading"><div><h2>Register provider</h2><p>Mulai dari metadata. Kredensial tidak diterima di form ini.</p></div></div>
                  <label className="field"><span>Nama koneksi</span><input name="providerName" autoComplete="off" maxLength={200} required placeholder="Gateway internal" disabled={!apiReady || saving} /></label>
                  <label className="field"><span>Provider key</span><input name="providerKey" autoComplete="off" pattern="[a-z][a-z0-9_\-]{0,62}" required placeholder="internal_gateway" disabled={!apiReady || saving} /><small>Huruf kecil, angka, garis bawah, atau tanda hubung.</small></label>
                  <label className="field"><span>Adapter type</span><select name="adapterType" required disabled={!apiReady || saving}><option value="openai_compatible">OpenAI-compatible</option><option value="openai">OpenAI</option><option value="azure_openai">Azure OpenAI</option><option value="ollama">Ollama</option><option value="custom">Custom adapter</option></select></label>
                  <label className="field"><span>Base URL</span><input name="baseUrl" type="url" required pattern="https://[^\/@?#]+(?:/[^?#]*)?" placeholder="https://gateway.example/v1" disabled={!apiReady || saving} /><small>HTTPS diperlukan. Host harus ada di allowlist egress server; uji koneksi lewat Provider probe.</small></label>
                  <button className="button primary full" type="submit" disabled={!apiReady || saving}>{saving ? 'Menyimpan…' : 'Simpan provider'}</button>
                  {!apiReady && <p className="form-hint" id="provider-form-hint">Form aktif setelah API dan sesi host tersedia.</p>}
                </form>
              </section>

              <section className="lower-grid" aria-label="Credential and approval controls">
                <form className="panel form-panel" onSubmit={submitCredential}>
                  <div className="panel-heading"><div><h2>Provider credential</h2><p>Nilai dikirim sekali ke vault backend dan tidak dapat dibaca kembali.</p></div></div>
                  <label className="field"><span>Provider</span><select name="credentialProvider" required disabled={providerState !== 'ready' || providers.length === 0 || saving}><option value="">Pilih provider</option>{providers.map((provider) => <option key={provider.id} value={provider.id}>{provider.display_name}</option>)}</select></label>
                  <label className="field"><span>Credential</span><input name="credential" type="password" autoComplete="new-password" maxLength={98000} required disabled={providerState !== 'ready' || providers.length === 0 || saving} /></label>
                  <button className="button primary" type="submit" disabled={providerState !== 'ready' || providers.length === 0 || saving}>{saving ? 'Menyimpan…' : 'Simpan atau rotasi credential'}</button>
                  <p className="form-hint">Form dikosongkan setelah setiap percobaan. API hanya mengembalikan status configured; nilai credential dan referensi vault tidak pernah ditampilkan.</p>
                </form>
                <form className="panel form-panel" onSubmit={submitProviderProbe}>
                  <div className="panel-heading"><div><p className="eyebrow">CONNECTION CHECK</p><h2>Provider probe</h2><p>Probe hanya berjalan setelah tindakan eksplisit. Pemeriksaan konektivitas meminta daftar model tanpa mengirim prompt.</p></div></div>
                  <p className="scope-banner" role="status"><span className="scope-mark" aria-hidden="true">i</span><span>Probe memakai endpoint provider yang terdaftar. Server memeriksa allowlist egress, izin akun dan persetujuan penggunaan token.</span></p>
                  <label className="field"><span>Provider</span><select value={probeProviderId} onChange={(event) => { setProbeProviderId(event.target.value); setProbeDeploymentId(''); setProbeResult(null); setProbeState('idle'); }} disabled={providerState !== 'ready' || providers.length === 0 || probeState === 'loading'}><option value="">Pilih provider</option>{providers.filter((item) => item.adapter_type === 'openai_compatible').map((item) => <option key={item.id} value={item.id}>{item.display_name}</option>)}</select></label>
                  <label className="field"><span>Deployment</span><select value={probeDeploymentId} onChange={(event) => { setProbeDeploymentId(event.target.value); setProbeResult(null); setProbeState('idle'); }} disabled={!probeProviderId || probeState === 'loading'}><option value="">Pilih deployment provider</option>{deployments.filter((item) => item.provider_id === probeProviderId).map((item) => <option key={item.id} value={item.id}>{item.display_name} · {item.provider_model_id}</option>)}</select></label>
                  {probeProviderId && <p className="form-hint" role="status">{providerHasValidHTTPSURL(providers.find((item) => item.id === probeProviderId)?.base_url ?? '') ? 'Endpoint HTTPS terdaftar; kebijakan host diverifikasi server.' : 'Endpoint HTTPS valid diperlukan; probe dinonaktifkan.'} {credentialStatuses[probeProviderId]?.credential_configured ? 'Credential configured pada backend.' : credentialStatusErrors[probeProviderId] ? 'Status credential tidak dapat dibaca; probe tetap dinonaktifkan.' : 'Credential belum dikonfirmasi configured.'}</p>}
                  <fieldset className="capabilities"><legend>Pengujian tambahan</legend><label><input type="checkbox" checked={probeJson} onChange={(event) => { setProbeJson(event.target.checked); setProbeCostAcknowledged(false); }} disabled={probeState === 'loading'} /> JSON output</label><label><input type="checkbox" checked={probeTools} onChange={(event) => { setProbeTools(event.target.checked); setProbeCostAcknowledged(false); }} disabled={probeState === 'loading'} /> Tool calling</label><p>Pengujian JSON atau tool mengirim prompt sintetis ke provider dan dapat menimbulkan biaya token.</p></fieldset>
                  {(probeJson || probeTools) && <label className="field"><span><input type="checkbox" checked={probeCostAcknowledged} onChange={(event) => setProbeCostAcknowledged(event.target.checked)} disabled={probeState === 'loading'} /> Saya memahami pengujian kapabilitas mengirim prompt sintetis dan dapat menimbulkan biaya provider.</span></label>}
                  <button className="button primary" type="submit" disabled={!probeProviderId || !probeDeploymentId || !credentialStatuses[probeProviderId]?.credential_configured || !providerHasValidHTTPSURL(providers.find((item) => item.id === probeProviderId)?.base_url ?? '') || ((probeJson || probeTools) && !probeCostAcknowledged) || probeState === 'loading'}>{probeState === 'loading' ? 'Menjalankan probe…' : (probeJson || probeTools) ? 'Jalankan probe capability' : 'Jalankan pemeriksaan koneksi'}</button>
                  {probeState === 'error' && <div className="records-state error-state" role="alert">{probeError}</div>}
                  {probeState === 'ready' && probeResult && <div className="records-state" role="status"><strong>Probe {statusLabel(probeResult.status)}</strong><span> · {new Date(probeResult.checked_at).toLocaleString()}</span>{probeResult.may_incur_token_cost && <p>Probe mengirim request yang dapat menimbulkan biaya token.</p>}</div>}
                </form>
              </section>

              <section className="panel deployments-panel" aria-label="Model deployment registry">
                <div className="panel-heading"><div><h2>Model deployments</h2><p>ID model yang dikonfigurasi beserta batas pemakaian.</p></div><span className="count">{deploymentState === 'ready' ? `${deploymentTotal} model` : 'Belum dimuat'}</span></div>
                <RecordsState state={deploymentState} message={deploymentError} empty="Belum ada deployment untuk tenant ini." count={deployments.length} />
                {deploymentState === 'ready' && deployments.length > 0 && <TableScroll label="Tenant model deployments"><table><caption className="sr-only">Tenant model deployments</caption><thead><tr><th>Deployment</th><th>Provider model ID</th><th>Capabilities</th><th>Limits</th><th>Status</th></tr></thead><tbody>{deployments.map((deployment) => { const provider = providers.find((item) => item.id === deployment.provider_id); const capabilities = deployment.capabilities ?? {}; return <tr key={deployment.id}><td><strong>{deployment.display_name}</strong><small>{deployment.key}{provider ? ` · ${provider.display_name}` : ''}</small></td><td><code>{deployment.provider_model_id}</code></td><td>{capabilities.tools === true ? 'Tools' : 'No tools'} · {capabilities.json === true ? 'JSON' : 'No JSON'}</td><td>{deployment.max_output_tokens.toLocaleString()} tokens<br />{Math.round(deployment.timeout_ms / 1000)} sec</td><td><StatusBadge value={deployment.status} /></td></tr>; })}</tbody></table></TableScroll>}
                {deploymentState === 'ready' && deploymentTotal > 100 && <div className="pagination"><p className="footer-note">Menampilkan {deploymentOffset + 1} sampai {deploymentOffset + deployments.length} dari {deploymentTotal} deployment.</p><div><button className="button" type="button" disabled={deploymentOffset === 0 || apiLoading} onClick={() => setDeploymentOffset(Math.max(0, deploymentOffset - 100))}>Sebelumnya</button><button className="button" type="button" disabled={deploymentOffset + deployments.length >= deploymentTotal || apiLoading} onClick={() => setDeploymentOffset(deploymentOffset + 100)}>Berikutnya</button></div></div>}
              </section>

              <section className="lower-grid">
                <form className="panel form-panel" onSubmit={submitDeployment}>
                  <div className="panel-heading"><div><h2>Register model deployment</h2><p>Deployment baru menunggu probe dan approval backend.</p></div></div>
                  <div className="field-grid">
                    <label className="field"><span>Nama deployment</span><input name="modelName" maxLength={200} required placeholder="General drafting" disabled={!apiReady || saving} /></label>
                    <label className="field"><span>Deployment key</span><input name="deploymentKey" pattern="[a-z][a-z0-9_\-]{0,62}" required placeholder="general_drafting" disabled={!apiReady || saving} /></label>
                  </div>
                  <label className="field"><span>Provider connection</span><select name="providerId" required disabled={!apiReady || saving || providers.length === 0}><option value="">Pilih provider</option>{providers.map((provider) => <option key={provider.id} value={provider.id}>{provider.display_name}</option>)}</select></label>
                  <label className="field"><span>Provider model ID</span><input name="modelId" maxLength={256} required placeholder="Exact model identifier" disabled={!apiReady || saving} /></label>
                  <div className="field-grid limits-grid"><label className="field"><span>Batas output token</span><input name="maxTokens" type="number" min="1" max="1000000" defaultValue="2048" required disabled={!apiReady || saving} /></label><label className="field"><span>Timeout (detik)</span><input name="timeoutSeconds" type="number" min="1" max="600" defaultValue="60" required disabled={!apiReady || saving} /></label></div>
                  <fieldset className="capabilities"><legend>Kapabilitas yang diklaim</legend><label><input name="tools" type="checkbox" disabled={!apiReady || saving} /> Tool calls</label><label><input name="json" type="checkbox" disabled={!apiReady || saving} /> JSON output</label><p>Nilai ini belum diverifikasi oleh probe provider.</p></fieldset>
                  <button className="button primary" type="submit" disabled={!apiReady || saving || providers.length === 0}>{saving ? 'Menyimpan…' : 'Simpan deployment'}</button>
                  {(!apiReady || providers.length === 0) && <p className="form-hint">Tambahkan provider dan pastikan API tersedia sebelum menyimpan deployment.</p>}
                </form>
                <section className="panel form-panel" aria-label="Model approval status">
                  <div className="panel-heading"><div><h2>Model approval</h2><p>Approve deployment per environment setelah capability probe (JSON atau tool calling) lulus.</p></div></div>
                  <label className="field"><span>Deployment</span><select value={approvalDeploymentId} onChange={(event) => { setApprovalDeploymentId(event.target.value); setApprovalState('idle'); }} disabled={deploymentState !== 'ready' || deployments.length === 0}><option value="">Pilih deployment</option>{deployments.map((item) => <option key={item.id} value={item.id}>{item.display_name}</option>)}</select></label>
                  <label className="field"><span>Environment</span><select value={approvalEnvironment} onChange={(event) => { setApprovalEnvironment(event.target.value as ModelApproval['environment']); setApprovalState('idle'); }}><option value="development">Development</option><option value="staging">Staging</option><option value="production">Production</option></select></label>
                  <button className="button" type="button" onClick={() => void loadApprovals()} disabled={!approvalDeploymentId || approvalState === 'loading'}>{approvalState === 'loading' ? 'Memuat…' : 'Muat status approval'}</button>
                  {approvalState === 'error' && <div className="records-state error-state" role="alert">{approvalError}</div>}
                  {approvalState === 'ready' && (approvals.length ? <div className="provider-list">{approvals.map((item) => <article className="provider-row" key={item.id}><div className="record-main"><strong>{statusLabel(item.state)}</strong><span>{item.environment} · {item.purpose}</span><small>{item.decision_reason || 'Tidak ada alasan tercatat'}{item.decided_at ? ` · ${new Date(item.decided_at).toLocaleString()}` : ''}</small></div></article>)}</div> : <div className="records-state">Belum ada approval untuk environment ini.</div>)}
                  {(can(session, 'model_deployment.approve') || can(session, 'model_deployment.revoke')) ? (
                    <>
                      <label className="field"><span>Alasan keputusan</span><input value={approvalReason} maxLength={1000} onChange={(event) => setApprovalReason(event.target.value)} placeholder="Probe kapabilitas lulus; dipakai untuk sandbox" disabled={saving} /></label>
                      <div className="button-row">
                        {can(session, 'model_deployment.approve') && <button className="button primary" type="button" onClick={() => void decideApproval('approved')} disabled={saving || !approvalDeploymentId || !approvalReason.trim()}>Approve untuk {approvalEnvironment}</button>}
                        {can(session, 'model_deployment.revoke') && <button className="button" type="button" onClick={() => void decideApproval('revoked')} disabled={saving || !approvalDeploymentId || !approvalReason.trim()}>Cabut approval</button>}
                      </div>
                      {approvalNotice && <p className="form-hint" role="status">{approvalNotice}</p>}
                      <p className="form-hint">Approval ditolak API bila belum ada capability probe yang lulus untuk konfigurasi deployment saat ini.</p>
                    </>
                  ) : <p className="form-hint">Sesi ini tidak memiliki izin approve/revoke model; status hanya dapat dibaca.</p>}
                </section>
              </section>
            </>
          ) : page === 'skills' || page === 'tools' ? (
            <CatalogWorkbench mode={page} />
          ) : page === 'playground' ? (
            <Playground session={session} />
          ) : page === 'evaluations' ? (
            <Evaluations session={session} />
          ) : page === 'releases' ? (
            <Releases session={session} />
          ) : (
            <Runs session={session} />
          )}
          <footer className="footer-note">Agent Studio · Tenant and action scope are enforced by the host session and API.</footer>
        </div>
      </main>
    </div>
  );
}
