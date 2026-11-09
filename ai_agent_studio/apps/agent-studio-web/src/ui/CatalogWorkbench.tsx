import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import {
  Agent, AgentVersion, AgentVersionInput, Skill, SkillBinding, SkillVersion,
  SkillVersionInput, ToolBinding, ToolVersion, api,
} from '../api';

type Mode = 'skills' | 'tools';
type LoadState = 'loading' | 'ready' | 'error';

function errorText(error: unknown) {
  return error instanceof Error ? error.message : 'Permintaan gagal. Coba lagi.';
}

function parseObject(value: FormDataEntryValue | null): Record<string, unknown> {
  const parsed: unknown = JSON.parse(String(value ?? '{}'));
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('Masukkan JSON object yang valid.');
  return parsed as Record<string, unknown>;
}

function stateLabel(value: string) {
  return value.replaceAll('_', ' ');
}

export default function CatalogWorkbench({ mode }: { mode: Mode }) {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [agentKey, setAgentKey] = useState('');
  const [agentVersions, setAgentVersions] = useState<AgentVersion[]>([]);
  const [agentVersionID, setAgentVersionID] = useState('');
  const [skills, setSkills] = useState<Skill[]>([]);
  const [skillID, setSkillID] = useState('');
  const [skillVersions, setSkillVersions] = useState<SkillVersion[]>([]);
  const [skillVersionID, setSkillVersionID] = useState('');
  const [tools, setTools] = useState<ToolVersion[]>([]);
  const [toolState, setToolState] = useState<LoadState>('loading');
  const [toolError, setToolError] = useState('');
  const [skillBindings, setSkillBindings] = useState<SkillBinding[]>([]);
  const [toolBindings, setToolBindings] = useState<ToolBinding[]>([]);
  const [state, setState] = useState<LoadState>('loading');
  const [detailState, setDetailState] = useState<LoadState>('loading');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [working, setWorking] = useState(false);

  const selectedSkill = useMemo(() => skills.find((item) => item.id === skillID), [skills, skillID]);
  const selectedSkillVersion = useMemo(() => skillVersions.find((item) => item.id === skillVersionID), [skillVersions, skillVersionID]);
  const selectedAgentVersion = useMemo(() => agentVersions.find((item) => item.id === agentVersionID), [agentVersions, agentVersionID]);
  const selectedAgent = useMemo(() => agents.find((item) => item.key === agentKey), [agents, agentKey]);

  const loadAgents = useCallback(async () => {
    setState('loading');
    setError('');
    try {
      const page = await api.agents(100, 0);
      const available = page.items.filter((agent) => agent.lifecycle_status !== 'archived');
      setAgents(available);
      setAgentKey((current) => available.some((agent) => agent.key === current) ? current : available[0]?.key ?? '');
      setState('ready');
    } catch (loadError) {
      setError(errorText(loadError));
      setState('error');
    }
  }, []);

  useEffect(() => { void loadAgents(); }, [loadAgents]);

  useEffect(() => {
    let current = true;
    setDetailState('loading');
    setError('');
    if (!agentKey) {
      setSkills([]);
      setSkillID('');
      setAgentVersions([]);
      setAgentVersionID('');
      setDetailState(state === 'error' ? 'error' : 'ready');
      return () => { current = false; };
    }
    Promise.all([api.skills(agentKey), api.agentVersions(agentKey)]).then(([skillPage, versionPage]) => {
      if (!current) return;
      const nextSkills = skillPage.items;
      const nextVersions = versionPage.items;
      setSkills(nextSkills);
      setSkillID((old) => nextSkills.some((skill) => skill.id === old) ? old : nextSkills[0]?.id ?? '');
      setAgentVersions(nextVersions);
      const preferred = nextVersions.find((version) => version.state === 'draft') ?? nextVersions[0];
      setAgentVersionID((old) => nextVersions.some((version) => version.id === old) ? old : preferred?.id ?? '');
      setDetailState('ready');
    }).catch((loadError: unknown) => {
      if (!current) return;
      setError(errorText(loadError));
      setDetailState('error');
    });
    return () => { current = false; };
  }, [agentKey, state]);

  useEffect(() => {
    let current = true;
    setSkillVersions([]);
    setSkillVersionID('');
    if (!skillID) {
      return () => { current = false; };
    }
    api.skillVersions(skillID).then((page) => {
      if (!current) return;
      const items = page.items;
      setSkillVersions(items);
      const preferred = items.find((version) => version.state === 'draft') ?? items[0];
      setSkillVersionID((old) => items.some((version) => version.id === old) ? old : preferred?.id ?? '');
    }).catch((loadError: unknown) => {
      if (!current) return;
      setError(errorText(loadError));
      setDetailState('error');
    });
    return () => { current = false; };
  }, [skillID]);

  useEffect(() => {
    let current = true;
    if (!agentVersionID) {
      setSkillBindings([]);
      setToolBindings([]);
      return () => { current = false; };
    }
    Promise.all([api.skillBindings(agentVersionID), api.toolBindings(agentVersionID)]).then(([nextSkills, nextTools]) => {
      if (!current) return;
      setSkillBindings(nextSkills);
      setToolBindings(nextTools);
    }).catch((loadError: unknown) => {
      if (!current) return;
      setError(errorText(loadError));
      setDetailState('error');
    });
    return () => { current = false; };
  }, [agentVersionID]);

  useEffect(() => {
    if (mode !== 'tools') return;
    let current = true;
    setToolState('loading');
    setToolError('');
    api.tools().then((page) => {
      if (current) {
        setTools(page.items);
        setToolState('ready');
      }
    }).catch((loadError: unknown) => {
      if (current) {
        setToolError(errorText(loadError));
        setToolState('error');
      }
    });
    return () => { current = false; };
  }, [mode]);

  async function perform(action: () => Promise<unknown>, success: string, refresh: () => Promise<unknown>) {
    setWorking(true);
    setNotice('');
    setError('');
    try {
      await action();
      setNotice(success);
      await refresh();
    } catch (actionError) {
      setError(errorText(actionError));
    } finally {
      setWorking(false);
    }
  }

  async function refreshCatalog() {
    await loadAgents();
  }

  async function refreshSkillDetails() {
    if (!agentKey) return;
    const [skillPage, versionPage] = await Promise.all([api.skills(agentKey), api.agentVersions(agentKey)]);
    const nextSkills = skillPage.items;
    const nextVersions = versionPage.items;
    setSkills(nextSkills);
    setSkillID((old) => nextSkills.some((skill) => skill.id === old) ? old : nextSkills[0]?.id ?? '');
    setAgentVersions(nextVersions);
    const preferred = nextVersions.find((version) => version.state === 'draft') ?? nextVersions[0];
    setAgentVersionID((old) => nextVersions.some((version) => version.id === old) ? old : preferred?.id ?? '');
  }

  async function createSkill(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!agentKey) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    await perform(() => api.createSkill(agentKey, {
      key: String(data.get('skillKey')).trim(),
      domain: String(data.get('skillDomain')).trim(),
    }), 'Draft skill dan versi awal dibuat.', async () => { form.reset(); await refreshSkillDetails(); });
  }

  async function createSkillVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedSkill) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const input: SkillVersionInput = {
        semver: String(data.get('skillSemver')).trim(),
        instructions: String(data.get('instructions')).trim(),
        procedures: [], examples: [], forbidden_actions: [],
        output_schema: parseObject(data.get('skillSchema')),
      };
      await perform(() => api.createSkillVersion(selectedSkill.id, input), 'Versi skill draft dibuat.', async () => {
        const items = (await api.skillVersions(selectedSkill.id)).items;
        setSkillVersions(items);
        setSkillVersionID(items[0]?.id ?? '');
        form.reset();
      });
    } catch (parseError) {
      setError(errorText(parseError));
    }
  }

  async function saveSkillVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedSkillVersion || selectedSkillVersion.state !== 'draft') return;
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const input: SkillVersionInput = {
        semver: String(data.get('skillSemver')).trim(),
        instructions: String(data.get('instructions')).trim(),
        procedures: selectedSkillVersion.procedures ?? [],
        examples: selectedSkillVersion.examples ?? [],
        forbidden_actions: selectedSkillVersion.forbidden_actions ?? [],
        output_schema: parseObject(data.get('skillSchema')),
      };
      await perform(() => api.updateSkillVersion(selectedSkillVersion.id, input), 'Draft skill diperbarui.', async () => {
        setSkillVersions((await api.skillVersions(selectedSkillVersion.skill_id)).items);
      });
    } catch (parseError) {
      setError(errorText(parseError));
    }
  }

  async function createAgentVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!agentKey) return;
    const data = new FormData(event.currentTarget);
    try {
      const input: AgentVersionInput = {
        semver: String(data.get('agentSemver')).trim(),
        manifest: parseObject(data.get('manifest')),
        input_schema: parseObject(data.get('inputSchema')),
        output_schema: parseObject(data.get('outputSchema')),
        model_policy: parseObject(data.get('modelPolicy')),
        tool_policy: parseObject(data.get('toolPolicy')),
      };
      await perform(() => api.createAgentVersion(agentKey, input), 'Draft agent version dibuat.', refreshSkillDetails);
    } catch (parseError) {
      setError(errorText(parseError));
    }
  }

  async function saveAgentVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedAgentVersion || selectedAgentVersion.state !== 'draft') return;
    const data = new FormData(event.currentTarget);
    try {
      const input: AgentVersionInput = {
        semver: String(data.get('agentSemver')).trim(),
        manifest: parseObject(data.get('manifest')),
        input_schema: parseObject(data.get('inputSchema')),
        output_schema: parseObject(data.get('outputSchema')),
        model_policy: parseObject(data.get('modelPolicy')),
        tool_policy: parseObject(data.get('toolPolicy')),
      };
      await perform(() => api.updateAgentVersion(selectedAgentVersion.id, input), 'Draft agent version diperbarui.', refreshSkillDetails);
    } catch (parseError) {
      setError(errorText(parseError));
    }
  }

  async function refreshTools() {
    setToolState('loading');
    setToolError('');
    try {
      setTools((await api.tools()).items);
      setToolState('ready');
    } catch (loadError) {
      setToolError(errorText(loadError));
      setToolState('error');
    }
  }

  const draftAgentVersion = selectedAgentVersion?.state === 'draft';
  const availableTools = tools.filter((tool) => tool.lifecycle_status === 'active' && Boolean(tool.published_at));

  return (
    <>
      <div className="page-heading">
        <div>
          <p className="eyebrow">AUTHORING / TENANT CATALOG</p>
          <h1>{mode === 'skills' ? 'Skills Studio' : 'Tool Library'}</h1>
          <p className="page-subtitle">{mode === 'skills' ? 'Kelola draft skill dan agent. Publikasi serta perubahan capability menunggu kebijakan SME dan AI Ops.' : 'Tinjau katalog global dan binding yang sudah ada. Perubahan capability menunggu kebijakan delegasi.'}</p>
        </div>
        <button className="button secondary" type="button" onClick={() => { void refreshCatalog(); if (mode === 'tools') void refreshTools(); }} disabled={state === 'loading' || detailState === 'loading' || working}>Muat ulang</button>
      </div>
      <div className="scope-banner" role="note">
        <span className="scope-mark" aria-hidden="true">i</span>
        <div><strong>{mode === 'tools' ? 'Katalog global, binding tenant' : 'Tenant-scoped authoring'}</strong><p>{mode === 'tools' ? 'Definisi tool dibaca dari katalog global. Binding hanya ditampilkan; perubahan dibekukan sampai kebijakan delegasi tool disetujui.' : 'Semua data berasal dari API terverifikasi. Draft dapat diedit; publish dan binding dinonaktifkan sampai kontrak persetujuan SME/AI Ops tersedia.'}</p></div>
      </div>
      {notice && <div className="alert success" role="status">{notice}</div>}
      {error && <div className="alert error" role="alert">{error}</div>}

      {state === 'loading' ? <div className="records-state" role="status">Memuat agent tenant…</div> : state === 'error' ? <div className="records-state error-state" role="alert">{error || 'Agent tenant tidak tersedia.'}</div> : agents.length === 0 && mode === 'skills' ? (
        <div className="panel not-ready"><h2>Belum ada agent</h2><p>Buat draft agent dari Agent Registry sebelum mengelola versi, skill, atau tool binding.</p></div>
      ) : (
        <>
          {agents.length > 0 && <section className="catalog-controls panel" aria-label="Pilihan agent dan versi">
            <label className="field"><span>Agent tenant</span><select value={agentKey} onChange={(event) => setAgentKey(event.target.value)}>{agents.map((agent) => <option key={agent.id} value={agent.key}>{agent.display_name} · {agent.key}</option>)}</select></label>
            <label className="field"><span>Agent version</span><select value={agentVersionID} onChange={(event) => setAgentVersionID(event.target.value)}><option value="">Pilih versi</option>{agentVersions.map((version) => <option key={version.id} value={version.id}>{version.semver} · {stateLabel(version.state)}</option>)}</select></label>
            <span className={`status-badge ${draftAgentVersion ? 'status-active' : ''}`}>{selectedAgentVersion ? stateLabel(selectedAgentVersion.state) : 'Belum ada versi'}</span>
          </section>}
          {agents.length === 0 && <p className="form-hint">Belum ada agent tenant. Katalog global tetap dapat ditinjau tanpa memilih agent.</p>}
          {detailState === 'loading' ? <div className="records-state" role="status">Memuat katalog untuk {selectedAgent?.display_name ?? agentKey}…</div> : detailState === 'error' ? <div className="records-state error-state" role="alert">{error || 'Katalog belum tersedia.'}</div> : mode === 'skills' ? (
            <SkillsWorkspace
              skills={skills}
              selectedSkill={selectedSkill}
              skillID={skillID}
              setSkillID={setSkillID}
              versions={skillVersions}
              selectedVersion={selectedSkillVersion}
              versionID={skillVersionID}
              setVersionID={setSkillVersionID}
              bindings={skillBindings}
              agentVersion={selectedAgentVersion}
              draftAgentVersion={Boolean(draftAgentVersion)}
              working={working}
              onCreateSkill={createSkill}
              onCreateVersion={createSkillVersion}
              onSaveVersion={saveSkillVersion}
              onCreateAgentVersion={createAgentVersion}
              onSaveAgentVersion={saveAgentVersion}
            />
          ) : (
            <ToolsWorkspace
              tools={tools}
              toolState={toolState}
              toolError={toolError}
              availableTools={availableTools}
              bindings={toolBindings}
              agentVersion={selectedAgentVersion}
              working={working}
              onRefresh={refreshTools}
            />
          )}
        </>
      )}
    </>
  );
}

function SkillsWorkspace(props: {
  skills: Skill[];
  selectedSkill?: Skill;
  skillID: string;
  setSkillID: (value: string) => void;
  versions: SkillVersion[];
  selectedVersion?: SkillVersion;
  versionID: string;
  setVersionID: (value: string) => void;
  bindings: SkillBinding[];
  agentVersion?: AgentVersion;
  draftAgentVersion: boolean;
  working: boolean;
  onCreateSkill: (event: FormEvent<HTMLFormElement>) => void;
  onCreateVersion: (event: FormEvent<HTMLFormElement>) => void;
  onSaveVersion: (event: FormEvent<HTMLFormElement>) => void;
  onCreateAgentVersion: (event: FormEvent<HTMLFormElement>) => void;
  onSaveAgentVersion: (event: FormEvent<HTMLFormElement>) => void;
}) {
  const {
    skills, selectedSkill, skillID, setSkillID, versions, selectedVersion,
    versionID, setVersionID, bindings, agentVersion, draftAgentVersion, working,
  } = props;
  return (
    <div className="catalog-grid skills-grid">
      <section className="panel records-panel">
        <div className="panel-heading"><div><h2>Skill catalog</h2><p>Daftar skill yang dimiliki agent terpilih.</p></div><span className="count">{skills.length} skills</span></div>
        {skills.length === 0 ? <div className="records-state">Agent ini belum memiliki skill.</div> : <div className="provider-list">{skills.map((skill) => <button type="button" className={`catalog-item ${skill.id === skillID ? 'selected' : ''}`} key={skill.id} onClick={() => setSkillID(skill.id)}><span className="provider-glyph" aria-hidden="true">S</span><span className="record-main"><strong>{skill.key}</strong><span>{skill.domain}</span><small>{skill.owner_subject ?? 'Owner belum ditetapkan'}</small></span></button>)}</div>}
        <form className="catalog-form" onSubmit={props.onCreateSkill}>
          <h3>Buat skill draft</h3>
          <label className="field"><span>Skill key</span><input name="skillKey" pattern="[a-z][a-z0-9_.-]{0,62}" required disabled={working} /></label>
          <label className="field"><span>Domain</span><input name="skillDomain" maxLength={100} required disabled={working} /></label>
          <button className="button primary full" type="submit" disabled={working}>Buat skill</button>
        </form>
      </section>

      <section className="panel form-panel">
        <div className="panel-heading"><div><h2>Skill version</h2><p>{selectedSkill ? `${selectedSkill.key} · ${selectedSkill.domain}` : 'Pilih skill untuk melihat versi.'}</p></div></div>
        {!selectedSkill ? <div className="records-state">Pilih skill dari katalog.</div> : <>
          <label className="field"><span>Versi</span><select value={versionID} onChange={(event) => setVersionID(event.target.value)}><option value="">Pilih versi</option>{versions.map((version) => <option key={version.id} value={version.id}>{version.semver} · {stateLabel(version.state)}</option>)}</select></label>
          {selectedVersion && <form onSubmit={props.onSaveVersion}>
            <label className="field"><span>Semver</span><input name="skillSemver" key={`${selectedVersion.id}-semver`} defaultValue={selectedVersion.semver} pattern="(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)" required disabled={working || selectedVersion.state !== 'draft'} /></label>
            <label className="field"><span>Instructions</span><textarea name="instructions" key={`${selectedVersion.id}-instructions`} defaultValue={selectedVersion.instructions} rows={5} maxLength={20000} disabled={working || selectedVersion.state !== 'draft'} /></label>
            <label className="field"><span>Output schema (JSON object)</span><textarea name="skillSchema" key={`${selectedVersion.id}-schema`} defaultValue={JSON.stringify(selectedVersion.output_schema ?? {}, null, 2)} rows={5} spellCheck={false} disabled={working || selectedVersion.state !== 'draft'} /></label>
            {selectedVersion.state === 'draft' && <div className="catalog-actions"><button className="button" type="submit" disabled={working}>Simpan draft</button><button className="button" type="button" disabled>Publish menunggu approval</button></div>}
            {selectedVersion.state !== 'draft' && <p className="form-hint">Versi published bersifat immutable.</p>}
            {selectedVersion.checksum && <p className="form-hint">Checksum <code>{selectedVersion.checksum}</code></p>}
          </form>}
          {selectedSkill && <form className="catalog-form" onSubmit={props.onCreateVersion}>
            <h3>Buat versi berikutnya</h3>
            <label className="field"><span>Semver</span><input name="skillSemver" pattern="(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)" placeholder="0.2.0" required disabled={working} /></label>
            <label className="field"><span>Instructions</span><textarea name="instructions" rows={3} maxLength={20000} disabled={working} /></label>
            <label className="field"><span>Output schema (JSON object)</span><textarea name="skillSchema" defaultValue="{}" rows={3} spellCheck={false} disabled={working} /></label>
            <button className="button" type="submit" disabled={working}>Buat versi draft</button>
          </form>}
        </>}
      </section>

      <section className="panel records-panel">
        <div className="panel-heading"><div><h2>Agent version</h2><p>{agentVersion ? `${agentVersion.semver} · ${stateLabel(agentVersion.state)}` : 'Belum ada versi untuk agent ini.'}</p></div></div>
        <details className="catalog-form">
          <summary className="catalog-summary">Buat versi agent baru</summary>
          <form onSubmit={props.onCreateAgentVersion}>
          <h3>Draft agent version</h3>
          <label className="field"><span>Semver</span><input name="agentSemver" placeholder={agentVersion ? '0.2.0' : '0.1.0'} pattern="(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)" required disabled={working} /></label>
          <JsonObjectField name="manifest" label="Manifest" />
          <JsonObjectField name="inputSchema" label="Input schema" />
          <JsonObjectField name="outputSchema" label="Output schema" />
          <JsonObjectField name="modelPolicy" label="Model policy" />
          <JsonObjectField name="toolPolicy" label="Tool policy" />
          <p className="form-hint">Semua policy dimulai kosong. Model approval dan model binding belum tersedia; probe provider ada di Models &amp; Providers.</p>
          <button className="button primary" type="submit" disabled={working}>Buat versi draft</button>
          </form>
        </details>
        {agentVersion && <>
          {agentVersion.state === 'draft' && <form className="catalog-form" onSubmit={props.onSaveAgentVersion}>
            <h3>Edit agent draft</h3>
            <label className="field"><span>Semver</span><input name="agentSemver" key={`${agentVersion.id}-semver`} defaultValue={agentVersion.semver} pattern="(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)" required disabled={working} /></label>
            <JsonObjectField name="manifest" label="Manifest" value={agentVersion.manifest} />
            <JsonObjectField name="inputSchema" label="Input schema" value={agentVersion.input_schema} />
            <JsonObjectField name="outputSchema" label="Output schema" value={agentVersion.output_schema} />
            <JsonObjectField name="modelPolicy" label="Model policy" value={agentVersion.model_policy} />
            <JsonObjectField name="toolPolicy" label="Tool policy" value={agentVersion.tool_policy} />
            <button className="button" type="submit" disabled={working}>Simpan draft</button>
          </form>}
          <div className="catalog-binding-list">{bindings.length === 0 ? <p className="form-hint">Belum ada skill yang diikat ke versi ini.</p> : bindings.map((binding) => {
            const version = versions.find((item) => item.id === binding.skill_version_id);
            return <div className="binding-row" key={binding.skill_version_id}><span>{skills.find((skill) => skill.id === binding.skill_id)?.key ?? binding.skill_id} · {version?.semver ?? binding.skill_version_id}</span></div>;
          })}</div>
          <button className="button" type="button" disabled>Ikat skill menunggu approval</button>
          <p className="policy-status" role="note">Binding skill dan publish menunggu kontrak persetujuan SME/AI Ops.</p>
          {draftAgentVersion && <div className="catalog-actions"><button className="button" type="button" disabled>Publish agent menunggu approval</button><span className="form-hint">Versi published bersifat immutable.</span></div>}
        </>}
      </section>
    </div>
  );
}

function ToolsWorkspace(props: {
  tools: ToolVersion[];
  toolState: LoadState;
  toolError: string;
  availableTools: ToolVersion[];
  bindings: ToolBinding[];
  agentVersion?: AgentVersion;
  working: boolean;
  onRefresh: () => Promise<unknown>;
}) {
  return (
    <div className="catalog-grid tools-grid">
      <section className="panel records-panel">
        <div className="panel-heading"><div><h2>Published tool versions</h2><p>Definisi berasal dari katalog global.</p></div><button className="button" type="button" disabled={props.working} onClick={() => void props.onRefresh()}>Muat ulang</button></div>
        {props.toolState === 'loading' ? <div className="records-state" role="status">Memuat katalog global…</div> : props.toolState === 'error' ? <div className="records-state error-state" role="alert">{props.toolError || 'Katalog global tidak tersedia.'}</div> : props.tools.length === 0 ? <div className="records-state">Katalog belum memiliki tool versi published.</div> : <div className="provider-list">{props.tools.map((tool) => <article className="provider-row" key={tool.id}><span className="provider-glyph" aria-hidden="true">T</span><span className="record-main"><strong>{tool.key} · {tool.semver}</strong><span>{tool.kind} · owner {tool.owner}</span><small>Risk: {tool.risk_level} · {stateLabel(tool.lifecycle_status)}</small><code>{tool.adapter_ref}</code></span><StatusBadge value={tool.lifecycle_status} /></article>)}</div>}
        <p className="form-hint">{props.availableTools.length} tool memenuhi status active dan published. Binding tetap dinonaktifkan sampai kebijakan delegasi disetujui.</p>
      </section>
      <section className="panel form-panel">
        <div className="panel-heading"><div><h2>Tenant tool bindings</h2><p>{props.agentVersion ? `${props.agentVersion.semver} · ${stateLabel(props.agentVersion.state)}` : 'Pilih agent version dari panel atas.'}</p></div></div>
        {!props.agentVersion ? <div className="records-state">Belum ada agent version untuk dipilih.</div> : <>
          {props.bindings.length === 0 ? <div className="records-state">Belum ada tool yang diikat ke versi ini.</div> : <div className="catalog-binding-list">{props.bindings.map((binding) => {
            const tool = props.tools.find((item) => item.id === binding.tool_version_id);
            return <div className="binding-row" key={binding.tool_version_id}><span>{tool ? `${tool.key} · ${tool.semver}` : binding.tool_version_id}</span></div>;
          })}</div>}
          <p className="policy-status" role="note">Binding dan perubahan scope tool dinonaktifkan sampai kebijakan delegasi disetujui.</p>
        </>}
      </section>
    </div>
  );
}

function JsonObjectField({ name, label, value }: { name: string; label: string; value?: Record<string, unknown> }) {
  return <label className="field"><span>{label} (JSON object)</span><textarea name={name} defaultValue={JSON.stringify(value ?? {}, null, 2)} rows={3} spellCheck={false} /></label>;
}

function StatusBadge({ value }: { value: string }) {
  return <span className={`status-badge status-${value.replaceAll('_', '-')}`}>{stateLabel(value)}</span>;
}
