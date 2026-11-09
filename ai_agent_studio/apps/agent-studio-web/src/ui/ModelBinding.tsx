import { useCallback, useEffect, useState } from 'react';
import { api, AgentModelBinding, AgentVersion, ApiError, Deployment } from '../api';

type Props = { version: AgentVersion };

// Binds an approved model deployment to a draft version. The API refuses
// deployments that are not approved for the chosen environment, and the
// binding changes the version fingerprint, so earlier evaluations go stale.
export default function ModelBinding({ version }: Props) {
  const [binding, setBinding] = useState<AgentModelBinding | null>(null);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [deploymentId, setDeploymentId] = useState('');
  const [environment, setEnvironment] = useState<AgentModelBinding['environment']>('development');
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const isDraft = version.state === 'draft';

  const load = useCallback(async () => {
    setState('loading');
    setMessage('');
    const [bindingResult, deploymentResult] = await Promise.allSettled([api.modelBinding(version.id), api.deployments(100, 0)]);
    if (deploymentResult.status === 'fulfilled') setDeployments(deploymentResult.value.items);
    if (bindingResult.status === 'fulfilled') {
      setBinding(bindingResult.value);
      setDeploymentId(bindingResult.value.model_deployment_id);
      setEnvironment(bindingResult.value.environment || 'development');
      setState('ready');
    } else if (bindingResult.reason instanceof ApiError && bindingResult.reason.status === 404) {
      setBinding(null);
      setState('ready');
    } else {
      setMessage(bindingResult.reason instanceof Error ? bindingResult.reason.message : 'Binding tidak dapat dimuat.');
      setState('error');
    }
  }, [version.id]);

  useEffect(() => { void load(); }, [load]);

  async function save() {
    if (!deploymentId) return;
    setBusy(true);
    setMessage('');
    try {
      setBinding(await api.setModelBinding(version.id, deploymentId, environment));
      setMessage('Binding disimpan. Evaluation sebelumnya untuk versi ini menjadi stale; jalankan ulang sebelum request release.');
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Binding tidak dapat disimpan.');
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    setMessage('');
    try {
      await api.deleteModelBinding(version.id);
      setBinding(null);
      setMessage('Binding dihapus. Sandbox kembali memakai dev mock (hanya di mode development).');
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Binding tidak dapat dihapus.');
    } finally {
      setBusy(false);
    }
  }

  const bound = deployments.find((item) => item.id === binding?.model_deployment_id);
  return (
    <section className="panel form-panel" aria-label="Model binding">
      <div className="panel-heading"><div><h2>Model binding</h2><p>Model nyata untuk versi {version.semver}. Hanya deployment yang sudah di-approve untuk environment ini yang diterima.</p></div></div>
      {state === 'loading' && <div className="records-state" role="status">Memuat binding…</div>}
      {state === 'error' && <div className="records-state error-state" role="alert">{message}</div>}
      {state === 'ready' && (
        <>
          <div className="provider-row"><span className="record-main">
            <strong>{binding ? (bound ? `${bound.display_name} (${bound.provider_model_id})` : binding.model_deployment_id) : 'Belum ada binding'}</strong>
            <span>{binding ? `Environment: ${binding.environment}` : 'Run sandbox memakai dev mock (hanya mode development); run production ditolak.'}</span>
          </span></div>
          {isDraft ? (
            <>
              <label className="field"><span>Deployment</span>
                <select value={deploymentId} onChange={(event) => setDeploymentId(event.target.value)} disabled={busy}>
                  <option value="">Pilih deployment</option>
                  {deployments.map((item) => <option key={item.id} value={item.id}>{item.display_name} · {item.provider_model_id} · {item.status}</option>)}
                </select>
              </label>
              <label className="field"><span>Environment approval</span>
                <select value={environment} onChange={(event) => setEnvironment(event.target.value as AgentModelBinding['environment'])} disabled={busy}>
                  <option value="development">Development (sandbox &amp; evaluation)</option>
                  <option value="staging">Staging</option>
                  <option value="production">Production</option>
                </select>
              </label>
              <div className="button-row">
                <button className="button primary" type="button" onClick={() => void save()} disabled={busy || !deploymentId}>{busy ? 'Menyimpan…' : 'Simpan binding'}</button>
                {binding && <button className="button" type="button" onClick={() => void remove()} disabled={busy}>Hapus binding</button>}
              </div>
            </>
          ) : <p className="form-hint">Versi published: binding immutable.</p>}
          {message && <p className="form-hint" role="status">{message}</p>}
        </>
      )}
    </section>
  );
}
