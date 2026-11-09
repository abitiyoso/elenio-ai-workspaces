import ModelBinding from './ModelBinding';
import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import {
  Agent, AgentVersion, AgentVersionInput, Skill, SkillBinding, SkillVersion,
  ToolBinding, ToolVersion, api,
} from '../api';

type LoadState = 'loading' | 'ready' | 'error';

function errorText(error: unknown) {
  return error instanceof Error ? error.message : 'Permintaan gagal. Coba lagi.';
}

function parseObject(value: FormDataEntryValue | null): Record<string, unknown> {
  const parsed: unknown = JSON.parse(String(value ?? '{}'));
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('Masukkan JSON object yang valid.');
  return parsed as Record<string, unknown>;
}

function versionInput(data: FormData): AgentVersionInput {
  return {
    semver: String(data.get('semver')).trim(),
    manifest: parseObject(data.get('manifest')),
    input_schema: parseObject(data.get('inputSchema')),
    output_schema: parseObject(data.get('outputSchema')),
    model_policy: parseObject(data.get('modelPolicy')),
    tool_policy: parseObject(data.get('toolPolicy')),
  };
}

function nextSemver(versions: AgentVersion[]) {
  const parsed = versions.map((version) => version.semver.split('.').map(Number)).filter((parts) => parts.length === 3 && parts.every(Number.isFinite));
  if (!parsed.length) return '0.1.0';
  parsed.sort((a, b) => b[0] - a[0] || b[1] - a[1] || b[2] - a[2]);
  return `${parsed[0][0]}.${parsed[0][1]}.${parsed[0][2] + 1}`;
}

function JsonField({ name, label, value, disabled = false }: { name: string; label: string; value?: Record<string, unknown>; disabled?: boolean }) {
  return <label className="field"><span>{label} (JSON object)</span><textarea name={name} defaultValue={JSON.stringify(value ?? {}, null, 2)} rows={4} spellCheck={false} disabled={disabled} /></label>;
}

function PanelState({ state, error, empty }: { state: LoadState; error?: string; empty: string }) {
  if (state === 'loading') return <div className="records-state" role="status">Memuat dari registry API…</div>;
  if (state === 'error') return <div className="records-state error-state" role="alert">{error || 'Data tidak tersedia.'}</div>;
  return <div className="records-state">{empty}</div>;
}

export default function AgentDetail({ agent, onBack }: { agent: Agent; onBack: () => void }) {
  const [versions, setVersions] = useState<AgentVersion[]>([]);
  const [versionsState, setVersionsState] = useState<LoadState>('loading');
  const [versionsError, setVersionsError] = useState('');
  const [selectedID, setSelectedID] = useState('');
  const [skills, setSkills] = useState<Skill[]>([]);
  const [skillsState, setSkillsState] = useState<LoadState>('loading');
  const [skillsError, setSkillsError] = useState('');
  const [tools, setTools] = useState<ToolVersion[]>([]);
  const [toolsState, setToolsState] = useState<LoadState>('loading');
  const [toolsError, setToolsError] = useState('');
  const [skillVersionNames, setSkillVersionNames] = useState<Record<string, string>>({});
  const [skillBindings, setSkillBindings] = useState<SkillBinding[]>([]);
  const [skillBindingsState, setSkillBindingsState] = useState<LoadState>('loading');
  const [skillBindingsError, setSkillBindingsError] = useState('');
  const [toolBindings, setToolBindings] = useState<ToolBinding[]>([]);
  const [toolBindingsState, setToolBindingsState] = useState<LoadState>('loading');
  const [toolBindingsError, setToolBindingsError] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [actionError, setActionError] = useState('');

  const selectedVersion = useMemo(() => versions.find((version) => version.id === selectedID), [versions, selectedID]);
  const canEdit = agent.lifecycle_status !== 'archived';

  const refreshVersions = useCallback(async () => {
    setVersionsState('loading');
    setVersionsError('');
    try {
      const items = (await api.agentVersions(agent.key)).items;
      setVersions(items);
      setSelectedID((current) => items.some((item) => item.id === current) ? current : (items.find((item) => item.state === 'draft') ?? items[0])?.id ?? '');
      setVersionsState('ready');
    } catch (error) {
      setVersionsError(errorText(error));
      setVersionsState('error');
    }
  }, [agent.key]);

  useEffect(() => { void refreshVersions(); }, [refreshVersions]);

  useEffect(() => {
    let current = true;
    setSkillsState('loading');
    setSkillsError('');
    api.skills(agent.key).then((page) => {
      if (!current) return;
      setSkills(page.items);
      setSkillsState('ready');
    }).catch((error: unknown) => {
      if (!current) return;
      setSkillsError(errorText(error));
      setSkillsState('error');
    });
    return () => { current = false; };
  }, [agent.key]);

  useEffect(() => {
    let current = true;
    setToolsState('loading');
    setToolsError('');
    api.tools().then((page) => {
      if (!current) return;
      setTools(page.items);
      setToolsState('ready');
    }).catch((error: unknown) => {
      if (!current) return;
      setToolsError(errorText(error));
      setToolsState('error');
    });
    return () => { current = false; };
  }, []);

  useEffect(() => {
    let current = true;
    setSkillVersionNames({});
    if (!skills.length) return () => { current = false; };
    Promise.allSettled(skills.map((skill) => api.skillVersions(skill.id))).then((results) => {
      if (!current) return;
      const names: Record<string, string> = {};
      results.forEach((result) => {
        if (result.status !== 'fulfilled') return;
        result.value.items.forEach((version: SkillVersion) => { names[version.id] = version.semver; });
      });
      setSkillVersionNames(names);
    });
    return () => { current = false; };
  }, [skills]);

  useEffect(() => {
    let current = true;
    setSkillBindings([]);
    setToolBindings([]);
    if (!selectedID) {
      setSkillBindingsState('ready');
      setToolBindingsState('ready');
      return () => { current = false; };
    }
    setSkillBindingsState('loading');
    setToolBindingsState('loading');
    setSkillBindingsError('');
    setToolBindingsError('');
    Promise.allSettled([api.skillBindings(selectedID), api.toolBindings(selectedID)]).then(([skillResult, toolResult]) => {
      if (!current) return;
      if (skillResult.status === 'fulfilled') {
        setSkillBindings(skillResult.value);
        setSkillBindingsState('ready');
      } else {
        setSkillBindingsError(errorText(skillResult.reason));
        setSkillBindingsState('error');
      }
      if (toolResult.status === 'fulfilled') {
        setToolBindings(toolResult.value);
        setToolBindingsState('ready');
      } else {
        setToolBindingsError(errorText(toolResult.reason));
        setToolBindingsState('error');
      }
    });
    return () => { current = false; };
  }, [selectedID]);

  async function createVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canEdit) return;
    const data = new FormData(event.currentTarget);
    setBusy(true);
    setActionError('');
    setNotice('');
    try {
      await api.createAgentVersion(agent.key, versionInput(data));
      setNotice('Draft agent version dibuat. Belum dipublikasikan atau diberi capability.');
      await refreshVersions();
    } catch (error) {
      setActionError(errorText(error));
    } finally {
      setBusy(false);
    }
  }

  async function updateVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canEdit || !selectedVersion || selectedVersion.state !== 'draft') return;
    const data = new FormData(event.currentTarget);
    setBusy(true);
    setActionError('');
    setNotice('');
    try {
      await api.updateAgentVersion(selectedVersion.id, versionInput(data));
      setNotice('Draft agent version diperbarui.');
      await refreshVersions();
    } catch (error) {
      setActionError(errorText(error));
    } finally {
      setBusy(false);
    }
  }

  const skillNames = useMemo(() => new Map(skills.map((skill) => [skill.id, skill.key])), [skills]);
  const toolNames = useMemo(() => new Map(tools.map((tool) => [tool.id, `${tool.key} · ${tool.semver}`])), [tools]);

  return (
    <>
      <div className="page-heading">
        <div><p className="eyebrow">AUTHORING / AGENT REGISTRY</p><h1>{agent.display_name}</h1><p className="page-subtitle">{agent.key} · {agent.domain} · {agent.lifecycle_status.replaceAll('_', ' ')}</p></div>
        <button className="button secondary" type="button" onClick={onBack}>Kembali ke registry</button>
      </div>
      <div className="scope-banner" role="note"><span className="scope-mark" aria-hidden="true">i</span><div><strong>Draft authoring only</strong><p>Versi dapat dibuat dan diperbarui selama draft. Publish, skill/tool binding, dan perubahan capability menunggu kebijakan SME/AI Ops.</p></div></div>
      {notice && <div className="alert success" role="status">{notice}</div>}
      {actionError && <div className="alert error" role="alert">{actionError}</div>}

      <section className="panel deployments-panel">
        <div className="panel-heading"><div><h2>Agent versions</h2><p>Versi disimpan tenant-scoped; hanya draft yang dapat diedit.</p></div><span className="count">{versionsState === 'ready' ? `${versions.length} versions` : 'Belum dimuat'}</span></div>
        {versionsState === 'loading' || versionsState === 'error' ? <PanelState state={versionsState} error={versionsError} empty="Belum ada versi." /> : versions.length === 0 ? <PanelState state="ready" empty="Belum ada agent version." /> : (
          <>
            <label className="field"><span>Versi terpilih</span><select value={selectedID} onChange={(event) => setSelectedID(event.target.value)}>{versions.map((version) => <option key={version.id} value={version.id}>{version.semver} · {version.state}</option>)}</select></label>
            {selectedVersion && <div className="provider-row"><span className="record-main"><strong>{selectedVersion.semver}</strong><span>Status: {selectedVersion.state}</span>{selectedVersion.checksum && <code>SHA-256 {selectedVersion.checksum}</code>}</span></div>}
          </>
        )}
      </section>

      {selectedVersion && <>
        {selectedVersion.state === 'draft' ? (
          <section className="panel form-panel deployments-panel">
            <div className="panel-heading"><div><h2>Edit draft</h2><p>Schema dan policy disimpan sebagai metadata draft. Tidak ada model atau tool grant otomatis.</p></div></div>
            <VersionForm key={selectedVersion.id} version={selectedVersion} onSubmit={updateVersion} busy={busy || !canEdit} submitLabel="Simpan draft" />
          </section>
        ) : <section className="panel form-panel deployments-panel"><div className="panel-heading"><div><h2>Published version</h2><p>Versi published bersifat immutable.</p></div></div><div className="lower-grid"><JsonReadout label="Manifest" value={selectedVersion.manifest} /><JsonReadout label="Input schema" value={selectedVersion.input_schema} /><JsonReadout label="Output schema" value={selectedVersion.output_schema} /><JsonReadout label="Model policy" value={selectedVersion.model_policy} /><JsonReadout label="Tool policy" value={selectedVersion.tool_policy} /></div></section>}

        <ModelBinding key={selectedVersion.id} version={selectedVersion} />

        <section className="lower-grid">
          <section className="panel records-panel">
            <div className="panel-heading"><div><h2>Skill bindings</h2><p>Binding yang tercatat untuk versi terpilih.</p></div></div>
            {skillsState === 'error' && <p className="form-hint" role="alert">{skillsError} Nama skill tidak dapat dicocokkan; ID binding tetap ditampilkan.</p>}
            {skillBindingsState === 'loading' || skillBindingsState === 'error' ? <PanelState state={skillBindingsState} error={skillBindingsError} empty="Belum ada binding skill." /> : skillBindings.length === 0 ? <PanelState state="ready" empty="Belum ada binding skill." /> : <div className="provider-list">{skillBindings.map((binding) => <article className="provider-row" key={binding.skill_version_id}><span className="provider-glyph" aria-hidden="true">S</span><span className="record-main"><strong>{skillNames.get(binding.skill_id) ?? binding.skill_id}</strong><span>Version {skillVersionNames[binding.skill_version_id] ?? binding.skill_version_id}</span></span></article>)}</div>}
          </section>
          <section className="panel records-panel">
            <div className="panel-heading"><div><h2>Tool bindings</h2><p>Binding yang tercatat untuk versi terpilih.</p></div></div>
            {toolsState === 'error' && <p className="form-hint" role="alert">{toolsError} Nama tool tidak dapat dicocokkan; ID binding tetap ditampilkan.</p>}
            {toolBindingsState === 'loading' || toolBindingsState === 'error' ? <PanelState state={toolBindingsState} error={toolBindingsError} empty="Belum ada binding tool." /> : toolBindings.length === 0 ? <PanelState state="ready" empty="Belum ada binding tool." /> : <div className="provider-list">{toolBindings.map((binding) => <article className="provider-row" key={binding.tool_version_id}><span className="provider-glyph" aria-hidden="true">T</span><span className="record-main"><strong>{toolNames.get(binding.tool_version_id) ?? binding.tool_version_id}</strong><span>{toolsState === 'error' ? toolsError : 'Katalog tool global'}</span></span></article>)}</div>}
          </section>
        </section>
      </>}

      <details className="panel form-panel deployments-panel">
        <summary className="catalog-summary">Buat draft version baru</summary>
        <VersionForm onSubmit={createVersion} busy={busy || !canEdit} initialSemver={nextSemver(versions)} submitLabel="Buat draft version" />
      </details>
    </>
  );
}

function VersionForm({ version, initialSemver, onSubmit, busy, submitLabel }: {
  version?: AgentVersion;
  initialSemver?: string;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  busy: boolean;
  submitLabel: string;
}) {
  return (
    <form onSubmit={onSubmit}>
      <label className="field"><span>Semver</span><input name="semver" defaultValue={version?.semver ?? initialSemver ?? '0.1.0'} pattern="(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)" required disabled={busy} /></label>
      <JsonField name="manifest" label="Manifest" value={version?.manifest} disabled={busy} />
      <JsonField name="inputSchema" label="Input schema" value={version?.input_schema} disabled={busy} />
      <JsonField name="outputSchema" label="Output schema" value={version?.output_schema} disabled={busy} />
      <JsonField name="modelPolicy" label="Model policy" value={version?.model_policy} disabled={busy} />
      <JsonField name="toolPolicy" label="Tool policy" value={version?.tool_policy} disabled={busy} />
      <p className="form-hint">Nilai kosong tidak memberi akses model atau tool. Model nyata dipilih di panel Model binding.</p>
      <button className="button primary" type="submit" disabled={busy}>{busy ? 'Menyimpan…' : submitLabel}</button>
    </form>
  );
}

function JsonReadout({ label, value }: { label: string; value: Record<string, unknown> }) {
  return <div className="field"><span>{label}</span><pre className="records-state">{JSON.stringify(value, null, 2)}</pre></div>;
}
