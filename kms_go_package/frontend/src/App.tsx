import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import {manageDrawerFocus} from './design/mobileNavigation'

type Identity = { user_id: string; tenant_id: string; roles: string[] }
type Page = 'Overview' | 'Documents' | 'Collections' | 'Review queue' | 'Evidence search' | 'Processing' | 'Audit log' | 'Settings'
type Collection = { id?: string; collection_id?: string; name?: string; description?: string; classification?: string; created_at?: string }
type DocumentRecord = {
  id?: string; document_id?: string; title?: string; collection_id?: string; lifecycle_status?: string
  version_id?: string; version_number?: number; parser_status?: string; index_status?: string
  job_id?: string; job_status?: string; job_stage?: string; last_error?: string; created_at?: string
}
type IngestionJob = { id?: string; job_id?: string; document_id?: string; status?: string; stage?: string; attempts?: number; last_error?: string; updated_at?: string }
type QueueCounts = { queued: number; processing: number; failed: number; completed: number; oldest_pending_seconds: number }
type OpsStatus = {
  environment?: string; auth_mode?: string; schema_version?: number; checked_at?: string; pending_reviews: number
  dependencies: Record<'postgres' | 'elasticsearch' | 'object_storage' | 'docling' | 'embeddings', string>
  search_index: { name?: string; alias_targets: string[]; indexed_chunks: number | null; expected_chunks: number; in_sync: boolean | null }
  embedding: { enabled: boolean; model_id?: string; dimensions?: number }
  ingestion: QueueCounts; outbox: QueueCounts
}
type SessionState = 'loading' | 'ready' | 'unauthenticated' | 'unavailable' | 'error'
const phaseLabel = 'Governed knowledge'
type VersionRecord = {
  id: string; version_number: number; checksum_sha256?: string; created_by?: string; created_at?: string
  parser_status?: string; index_status?: string; review_status?: string; trust_status?: string
  publication_status?: string; valid_from?: string; expires_at?: string; capabilities?: string[]
}
type ReviewRecord = {
  id: string; document_id: string; version_id: string; document_title?: string; version_number?: number
  submitted_by?: string; submission_note?: string; submitted_at?: string; status?: string; capabilities?: string[]
}
type ActivityRecord = {
  id: string; actor_type?: string; actor_id?: string; action?: string; resource_type?: string
  resource_id?: string; version_id?: string; outcome?: string; rationale?: string; details?: unknown; created_at?: string
}
type SearchResult = {
  chunk_id?: string; document_id?: string; version_id?: string; title?: string; snippet?: string
  heading_path?: string[] | string; page_start?: number; page_end?: number; source_anchor?: string
  citation?: string; trust_status?: string; relevance_score?: number; matched_reason?: string
}
type ApiFailure = Error & { status?: number; code?: string }
type ApiErrorBody = { message?: string; code?: string; error?: string | { message?: string; code?: string } }

const pages: Page[] = ['Overview', 'Documents', 'Collections', 'Evidence search', 'Processing', 'Review queue', 'Audit log', 'Settings']
const glyphs: Record<Page, string> = {
  Overview: '⌂', Documents: '▤', Collections: '▣', 'Review queue': '✓',
  'Evidence search': '⌕', Processing: '◷', 'Audit log': '≋', Settings: '⚙',
}

// Vite's BASE_URL is "/" by default or e.g. "/apps/kms/" when built with KMS_BASE_PATH. API paths below are
// written as absolute "/api/..." and are prefixed here so the UI works behind the Control Plane sub-path.
const apiBase = import.meta.env.BASE_URL.replace(/\/+$/, '')

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(apiBase + path, { credentials: 'same-origin', ...init })
  const body = await response.json().catch(() => null) as ApiErrorBody | null
  if (!response.ok) {
    const errorMessage = typeof body?.error === 'string' ? body.error : body?.error?.message
    const error = new Error(body?.message ?? errorMessage ?? `Request failed (${response.status})`) as ApiFailure
    error.status = response.status
    error.code = body?.code ?? (typeof body?.error === 'object' ? body.error.code : undefined)
    throw error
  }
  return body as T
}

function resourceID(resource: { id?: string; document_id?: string; collection_id?: string; job_id?: string }) {
  return resource.id ?? resource.document_id ?? resource.collection_id ?? resource.job_id ?? ''
}

function isFinished(status?: string) {
  return ['indexed', 'failed', 'complete', 'completed', 'succeeded', 'cancelled'].includes((status ?? '').toLowerCase())
}

function displayStatus(status?: string, stage?: string) {
  const value = stage || status
  return value ? value.replace(/_/g, ' ') : 'Status pending'
}

// Jobs created in this session plus the latest job of every visible document, so processing state survives reloads.
function mergeJobs(sessionJobs: IngestionJob[], documents: DocumentRecord[]) {
  const merged = new Map<string, IngestionJob>()
  for (const document of documents) {
    if (!document.job_id) continue
    merged.set(document.job_id, { id: document.job_id, document_id: resourceID(document), status: document.job_status, stage: document.job_stage, last_error: document.last_error })
  }
  for (const job of sessionJobs) {
    const id = resourceID(job)
    if (id) merged.set(id, { ...merged.get(id), ...job })
  }
  return [...merged.values()]
}

function documentState(document: DocumentRecord) {
  if (document.job_status && !isFinished(document.job_status)) return displayStatus(document.job_status, document.job_stage)
  if (document.job_status === 'failed') return 'processing failed'
  return displayStatus(document.lifecycle_status)
}

export default function App() {
  const [identity, setIdentity] = useState<Identity | null>(null)
  const [sessionState, setSessionState] = useState<SessionState>('loading')
  const [sessionAttempt, setSessionAttempt] = useState(0)
  const headingRef = useRef<HTMLHeadingElement>(null)
  const menuButtonRef = useRef<HTMLButtonElement>(null)
  const firstRender = useRef(true)
  const [page, setPage] = useState<Page>('Overview')
  const [menuOpen, setMenuOpen] = useState(false)
  useEffect(()=>{
    if(!menuOpen)return;
    const drawer=document.getElementById('workspace-navigation');
    if(drawer&&menuButtonRef.current)return manageDrawerFocus(drawer,menuButtonRef.current,()=>setMenuOpen(false));
  },[menuOpen])
  const [collections, setCollections] = useState<Collection[]>([])
  const [documents, setDocuments] = useState<DocumentRecord[]>([])
  const [selectedDocument, setSelectedDocument] = useState<DocumentRecord | null>(null)
  const [jobs, setJobs] = useState<IngestionJob[]>([])
  const [collectionError, setCollectionError] = useState('')
  const [documentError, setDocumentError] = useState('')
  const [jobError, setJobError] = useState('')
  const [collectionBusy, setCollectionBusy] = useState(false)
  const [uploadBusy, setUploadBusy] = useState(false)
  const [searchBusy, setSearchBusy] = useState(false)
  const [searchError, setSearchError] = useState('')
  const [searchResults, setSearchResults] = useState<SearchResult[]>([])
  const [searched, setSearched] = useState(false)
  const [searchMode, setSearchMode] = useState<'keyword' | 'semantic' | 'hybrid'>('keyword')
  const [collectionName, setCollectionName] = useState('')
  const [collectionDescription, setCollectionDescription] = useState('')
  const [classification, setClassification] = useState('internal')
  const [uploadTitle, setUploadTitle] = useState('')
  const [uploadCollectionID, setUploadCollectionID] = useState('')
  const [uploadCaseID, setUploadCaseID] = useState('')
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const [query, setQuery] = useState('')

  const loadCollections = useCallback(async () => {
    try {
      const data = await request<{ collections?: Collection[] }>('/api/v1/collections')
      setCollections(Array.isArray(data.collections) ? data.collections : [])
      setCollectionError('')
    } catch (error) {
      setCollectionError(messageFor(error, 'Collections could not be loaded.'))
    }
  }, [])

  const loadDocuments = useCallback(async () => {
    try {
      const data = await request<{ documents?: DocumentRecord[] }>('/api/v1/documents')
      setDocuments(Array.isArray(data.documents) ? data.documents : [])
      setDocumentError('')
    } catch (error) {
      setDocumentError(messageFor(error, 'Documents could not be loaded.'))
    }
  }, [])

  const refreshJob = useCallback(async (jobID: string) => {
    try {
      const data = await request<{ job?: IngestionJob }>(`/api/v1/ingestion/jobs/${encodeURIComponent(jobID)}`)
      if (data.job) {
        setJobs(current => [data.job!, ...current.filter(job => resourceID(job) !== jobID)])
        setJobError('')
        if (isFinished(data.job.status)) void loadDocuments()
      }
    } catch (error) {
      setJobError(messageFor(error, 'Processing status could not be refreshed.'))
    }
  }, [loadDocuments])

  useEffect(() => {
    let active = true
    setSessionState('loading')
    request<Identity>('/api/v1/session')
      .then(data => { if (active) { setIdentity(data); setSessionState('ready') } })
      .catch(error => {
        if (!active) return
        const status = (error as ApiFailure).status
        setIdentity(null)
        setSessionState(status === 401 ? 'unauthenticated' : status === 503 ? 'unavailable' : 'error')
      })
    return () => { active = false }
  }, [sessionAttempt])

  useEffect(() => {
    if (firstRender.current) { firstRender.current = false; return }
    headingRef.current?.focus()
  }, [page])

  useEffect(() => {
    if (sessionState === 'ready') {
      void loadCollections()
      void loadDocuments()
    }
  }, [sessionState, loadCollections, loadDocuments])

  const trackedJobs = useMemo(() => mergeJobs(jobs, documents), [jobs, documents])
  const documentTitles = useMemo(() => new Map(documents.map(document => [resourceID(document), document.title ?? ''])), [documents])
  const pendingJobIDs = useMemo(() => trackedJobs.filter(job => !isFinished(job.status)).map(resourceID).filter(Boolean), [trackedJobs])
  const pendingJobsKey = pendingJobIDs.join('|')
  useEffect(() => {
    if (!pendingJobsKey) return
    const timer = window.setInterval(() => {
      pendingJobsKey.split('|').forEach(jobID => { void refreshJob(jobID) })
    }, 3000)
    return () => window.clearInterval(timer)
  }, [pendingJobsKey, refreshJob])

  async function createCollection(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCollectionBusy(true)
    setCollectionError('')
    try {
      await request('/api/v1/collections', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: collectionName.trim(), description: collectionDescription.trim(), classification }),
      })
      setCollectionName('')
      setCollectionDescription('')
      await loadCollections()
    } catch (error) {
      setCollectionError(messageFor(error, 'Collection could not be created.'))
    } finally {
      setCollectionBusy(false)
    }
  }

  async function uploadDocument(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!uploadFile) return
    setUploadBusy(true)
    setDocumentError('')
    const form = new FormData()
    form.set('file', uploadFile)
    form.set('collection_id', uploadCollectionID)
    form.set('title', uploadTitle.trim() || uploadFile.name)
    if (uploadCaseID.trim()) form.set('case_id', uploadCaseID.trim())
    try {
      const data = await request<{ document?: DocumentRecord; job?: IngestionJob }>('/api/v1/documents', { method: 'POST', body: form })
      if (data.job) setJobs(current => [data.job!, ...current.filter(job => resourceID(job) !== resourceID(data.job!))])
      setUploadTitle('')
      setUploadCaseID('')
      setUploadFile(null)
      const input = document.getElementById('source-file') as HTMLInputElement | null
      if (input) input.value = ''
      await Promise.all([loadDocuments(), loadCollections()])
      setPage('Processing')
    } catch (error) {
      setDocumentError(messageFor(error, 'The document could not be uploaded.'))
    } finally {
      setUploadBusy(false)
    }
  }

  async function runSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!query.trim()) return
    setSearchBusy(true)
    setSearchError('')
    setSearched(true)
    setSearchResults([])
    try {
      const data = await request<{ results?: SearchResult[] }>('/api/v1/search', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: query.trim(), mode: searchMode }),
      })
      setSearchResults(Array.isArray(data.results) ? data.results : [])
    } catch (error) {
      setSearchError(messageFor(error, 'Search could not be completed.'))
    } finally {
      setSearchBusy(false)
    }
  }

  async function retryJob(jobID: string) {
    setJobError('')
    try {
      await request(`/api/v1/ingestion/jobs/${encodeURIComponent(jobID)}/retry`, { method: 'POST' })
      await refreshJob(jobID)
    } catch (error) {
      setJobError(messageFor(error, 'The job could not be retried.'))
    }
  }

  const isOverview = page === 'Overview'
  return <div className="app-shell">
    <a className="skip-link" href="#main-content">Skip to main content</a>
    <aside id="workspace-navigation" className={`sidebar ${menuOpen ? 'open' : ''}`} aria-label="Workspace navigation">
      <a className="brand" href="#overview" onClick={event => { event.preventDefault(); setPage('Overview') }}>
        <img src={`${import.meta.env.BASE_URL}brand/elenio-logo.png`} alt="Elenio" />
        <small>KNOWLEDGE HUB</small>
      </a>
      <div className="tenant-card"><span className="tenant-mark" aria-hidden="true">K</span><span><strong>Knowledge workspace</strong><small>{identity ? 'Verified tenant access' : 'Authentication required'}</small></span></div>
      {apiBase && <a className="cp-back" href="/" aria-label="Back to the AI Workspace Control Plane"><span aria-hidden="true">←</span> AI Workspace</a>}
      <div className="nav-section">WORKSPACE</div>
      <nav className="nav-list" aria-label="Workspace">
        {pages.slice(0, pages.indexOf('Processing')).map(item => <button key={item} className={`nav-item ${page === item ? 'active' : ''}`} onClick={() => { setPage(item); setMenuOpen(false) }} aria-current={page === item ? 'page' : undefined}>
          <span className="nav-glyph" aria-hidden="true">{glyphs[item]}</span><span>{item}</span>
        </button>)}
        <button className={`nav-item ${page === 'Processing' ? 'active' : ''}`} onClick={() => { setPage('Processing'); setMenuOpen(false) }} aria-current={page === 'Processing' ? 'page' : undefined}>
          <span className="nav-glyph" aria-hidden="true">{glyphs.Processing}</span><span>Processing</span>
          {pendingJobIDs.length > 0 && <span className="nav-count">{pendingJobIDs.length}</span>}
        </button>
      </nav>
      <div className="nav-section governance-label">GOVERNANCE</div>
      <nav className="nav-list" aria-label="Governance">
        {pages.slice(pages.indexOf('Processing') + 1).map(item => <button key={item} className={`nav-item ${page === item ? 'active' : ''}`} onClick={() => { setPage(item); setMenuOpen(false) }} aria-current={page === item ? 'page' : undefined}>
          <span className="nav-glyph" aria-hidden="true">{glyphs[item]}</span><span>{item}</span>
        </button>)}
      </nav>
      <div className="sidebar-spacer" />
      <div className="storage-card"><div className="storage-head"><span>Source files</span><span>API protected</span></div><div className="storage-note">Original files stay behind server authorization.</div></div>
      <div className="profile"><span className="profile-avatar">{identity ? identity.user_id.slice(0, 2).toUpperCase() : '—'}</span><span><strong>{identity ? 'Verified session' : 'Host session'}</strong><small>{identity?.roles.join(', ') ?? 'Authentication required'}</small></span></div>
    </aside>
    {menuOpen && <button className="scrim" aria-label="Close navigation" onClick={() => setMenuOpen(false)} />}
    <main className="main-area" id="main-content" tabIndex={-1}>
      <header className="topbar">
        <div className="topbar-leading"><button ref={menuButtonRef} className="menu-button" aria-label="Open navigation menu" aria-controls="workspace-navigation" aria-expanded={menuOpen} onClick={() => setMenuOpen(true)}>☰</button><span className="crumb">Workspace <span>/</span> <strong>{page}</strong></span></div>
        <div className="topbar-actions"><span className="system-state"><i className={sessionState === 'ready' ? 'ready' : ''} /> {phaseLabel}</span><span className="top-avatar" aria-hidden="true">KW</span></div>
      </header>
      <section className="content">
        <div className="page-heading"><div><div className="eyebrow">KNOWLEDGE OPERATIONS</div><h1 ref={headingRef} tabIndex={-1}>{page}</h1><p>{isOverview ? 'Organize source documents, follow processing and search authorized evidence.' : pageDescription(page)}</p></div>
          {isOverview && <button className="button primary" onClick={() => setPage('Documents')}>Add a document</button>}
        </div>
        {sessionState === 'loading' && <div className="notice" role="status">Checking the host session…</div>}
        {sessionState === 'unauthenticated' && <div className="notice notice-warning" role="alert"><strong>Your host session has ended.</strong><span>This mini-app has no login page. Return to the host application to sign in again, then reopen Knowledge Workspace.</span><button className="button compact" type="button" onClick={() => setSessionAttempt(value => value + 1)}>Check again</button></div>}
        {sessionState === 'unavailable' && <div className="notice notice-warning" role="alert"><strong>Host authentication is not connected.</strong><span>Open this mini-app through the host application after the trusted session verifier is configured.</span><button className="button compact" type="button" onClick={() => setSessionAttempt(value => value + 1)}>Check again</button></div>}
        {sessionState === 'error' && <div className="notice notice-warning" role="alert"><strong>The workspace service could not be reached.</strong><span>Check your connection; the service may be restarting.</span><button className="button compact" type="button" onClick={() => setSessionAttempt(value => value + 1)}>Try again</button></div>}
        {sessionState === 'ready' && <div className="notice notice-success" role="status"><strong>Verified workspace session</strong><span>Tenant-scoped API access is active for this session.</span></div>}
        {isOverview && <Overview documents={documents} collections={collections} ready={sessionState === 'ready'} onNavigate={setPage} />}
        {page === 'Documents' && <DocumentsView documents={documents} collections={collections} error={documentError || collectionError} busy={uploadBusy} title={uploadTitle} setTitle={setUploadTitle} collectionID={uploadCollectionID} setCollectionID={setUploadCollectionID} caseID={uploadCaseID} setCaseID={setUploadCaseID} file={uploadFile} setFile={setUploadFile} onSubmit={uploadDocument} onRefresh={loadDocuments} onGoCollections={() => setPage('Collections')} selectedDocument={selectedDocument} onSelectDocument={setSelectedDocument} onDocumentDeleted={() => { setSelectedDocument(null); void loadDocuments() }} onJobCreated={job => setJobs(current => [job, ...current.filter(existing => resourceID(existing) !== resourceID(job))])} />}
        {page === 'Collections' && <CollectionsView collections={collections} error={collectionError} busy={collectionBusy} name={collectionName} setName={setCollectionName} description={collectionDescription} setDescription={setCollectionDescription} classification={classification} setClassification={setClassification} onSubmit={createCollection} onRefresh={loadCollections} />}
        {page === 'Evidence search' && <SearchView query={query} setQuery={setQuery} mode={searchMode} setMode={setSearchMode} busy={searchBusy} searched={searched} error={searchError} results={searchResults} onSubmit={runSearch} />}
        {page === 'Processing' && <ProcessingView jobs={trackedJobs} titles={documentTitles} error={jobError} onRefresh={refreshJob} onRetry={retryJob} />}
        {page === 'Review queue' && <ReviewQueueView />}
        {page === 'Audit log' && <AuditLogView collections={collections} />}
        {page === 'Settings' && sessionState === 'ready' && <SettingsView />}
        <footer className="page-footer"><span>Knowledge Workspace</span><span>{phaseLabel}</span></footer>
      </section>
    </main>
  </div>
}

function useOpsStatus(enabled: boolean) {
  const [status, setStatus] = useState<OpsStatus | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const load = useCallback(async () => {
    setLoading(true)
    try {
      setStatus(await request<OpsStatus>('/api/v1/ops/status'))
      setError('')
    } catch (failure) {
      setError(messageFor(failure, 'Operational status could not be loaded.'))
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => { if (enabled) void load() }, [enabled, load])
  return { status, error, loading, reload: load }
}

function Overview({ documents, collections, ready, onNavigate }: { documents: DocumentRecord[]; collections: Collection[]; ready: boolean; onNavigate: (page: Page) => void }) {
  const { status } = useOpsStatus(ready)
  const backlog = status ? status.ingestion.queued + status.ingestion.processing : null
  return <>
    <section className="welcome-panel">
      <div className="welcome-copy"><div className="panel-kicker">{phaseLabel.toUpperCase()}</div><h2>Keep each source close to its evidence.</h2><p>Upload source PDFs, follow their processing state, and search excerpts with document and page citations.</p><button className="button light" onClick={() => onNavigate('Documents')}>Open document library</button></div>
      <div className="panel-mark" aria-hidden="true"><div className="orbit orbit-one"/><div className="orbit orbit-two"/><div className="doc-stack"><span>PDF</span><span>MD</span><span>↗</span></div></div>
    </section>
    <div className="section-heading"><div><h2>Workspace contents</h2><p>Counts from the tenant-scoped API</p></div><span className="section-rule" /></div>
    <div className="foundation-grid phase2-counts">
      <article className="foundation-card"><span className="foundation-icon navy" aria-hidden="true">▤</span><div><strong>Documents</strong><p>{documents.length} available to this session</p></div></article>
      <article className="foundation-card"><span className="foundation-icon cobalt" aria-hidden="true">▣</span><div><strong>Collections</strong><p>{collections.length} available to this session</p></div></article>
      <article className="foundation-card"><span className="foundation-icon slate" aria-hidden="true">⌕</span><div><strong>Search</strong><p>{status ? (status.embedding.enabled ? 'Keyword, semantic and hybrid modes' : 'Keyword mode; semantic needs an embedding provider') : 'Keyword and configured vector modes'}</p></div></article>
    </div>
    {status && <>
      <div className="section-heading lower"><div><h2>Workspace health</h2><p>Live counts for this tenant</p></div><button className="text-button" onClick={() => onNavigate('Settings')}>Open settings <span aria-hidden="true">›</span></button></div>
      <div className="foundation-grid health-grid">
        <button type="button" className="foundation-card health-card" onClick={() => onNavigate('Review queue')}><span className="foundation-icon cobalt" aria-hidden="true">✓</span><div><strong>{status.pending_reviews} pending {status.pending_reviews === 1 ? 'review' : 'reviews'}</strong><p>Versions waiting for an independent decision</p></div></button>
        <button type="button" className="foundation-card health-card" onClick={() => onNavigate('Processing')}><span className={`foundation-icon ${status.ingestion.failed > 0 ? 'alert' : 'navy'}`} aria-hidden="true">◷</span><div><strong>{backlog} in progress · {status.ingestion.failed} failed</strong><p>Ingestion jobs{status.ingestion.oldest_pending_seconds > 0 ? `; oldest waiting ${formatAge(status.ingestion.oldest_pending_seconds)}` : ''}</p></div></button>
        <button type="button" className="foundation-card health-card" onClick={() => onNavigate('Settings')}><span className={`foundation-icon ${status.search_index.in_sync === false || status.outbox.failed > 0 ? 'alert' : 'slate'}`} aria-hidden="true">≋</span><div><strong>{status.search_index.in_sync === false ? 'Search index catching up' : status.search_index.in_sync ? 'Search index in sync' : 'Search index unavailable'}</strong><p>{status.outbox.queued + status.outbox.processing} queued index updates · {status.outbox.failed} failed</p></div></button>
      </div>
    </>}
    <div className="section-heading lower"><div><h2>Recent documents</h2><p>Source records visible to this session</p></div><button className="text-button" onClick={() => onNavigate('Documents')}>Open library <span aria-hidden="true">›</span></button></div>
    {documents.length === 0 ? <div className="empty-card"><span className="empty-symbol" aria-hidden="true">▤</span><strong>No documents in this workspace yet</strong><p>Create a collection, then upload a PDF to start its processing job.</p><button className="button primary" onClick={() => onNavigate('Collections')}>Create a collection</button></div> : <DocumentTable documents={documents.slice(0, 5)} />}
  </>
}

function DocumentsView(props: {
  documents: DocumentRecord[]; collections: Collection[]; error: string; busy: boolean; title: string; setTitle: (value: string) => void
  collectionID: string; setCollectionID: (value: string) => void; caseID: string; setCaseID: (value: string) => void
  file: File | null; setFile: (value: File | null) => void; onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onRefresh: () => void; onGoCollections: () => void; selectedDocument: DocumentRecord | null
  onSelectDocument: (document: DocumentRecord) => void; onDocumentDeleted: () => void; onJobCreated: (job: IngestionJob) => void
}) {
  const collectionOptions = props.collections.map(collection => ({ id: resourceID(collection), name: collection.name ?? 'Unnamed collection' })).filter(collection => collection.id)
  return <div className="phase2-layout">
    <section className="work-panel upload-panel">
      <div className="panel-heading"><div><h2>Upload a source</h2><p>PDF files are stored privately before asynchronous processing.</p></div></div>
      {props.collections.length === 0 ? <div className="inline-empty"><strong>A collection is required first.</strong><p>Create a tenant-scoped collection before uploading a source.</p><button className="button primary" onClick={props.onGoCollections}>Create a collection</button></div> : <form onSubmit={props.onSubmit} className="form-stack">
        <div className="field"><label htmlFor="source-file">PDF source</label><input id="source-file" type="file" accept="application/pdf,.pdf" required onChange={event => { const file = event.target.files?.[0] ?? null; props.setFile(file); if (file && !props.title) props.setTitle(file.name.replace(/\.pdf$/i, '')) }} /><small>Choose a PDF source. Processing status appears in the Processing section.</small></div>
        <div className="field"><label htmlFor="source-title">Document title</label><input id="source-title" value={props.title} onChange={event => props.setTitle(event.target.value)} maxLength={240} required /></div>
        <div className="field"><label htmlFor="source-collection">Collection</label><select id="source-collection" value={props.collectionID} onChange={event => props.setCollectionID(event.target.value)} required><option value="">Select a collection</option>{collectionOptions.map(collection => <option key={collection.id} value={collection.id}>{collection.name}</option>)}</select></div>
        <div className="field"><label htmlFor="source-case">Case scope <span className="optional-label">Optional</span></label><input id="source-case" value={props.caseID} onChange={event => props.setCaseID(event.target.value)} autoComplete="off" /><small>Leave empty for a collection source.</small></div>
        {props.error && <p className="inline-error" role="alert">{props.error}</p>}
        <button className="button primary" type="submit" disabled={props.busy || !props.file || !props.collectionID}>{props.busy ? 'Uploading…' : 'Upload and process'}</button>
      </form>}
    </section>
    <section className="work-panel library-panel">
      <div className="panel-heading"><div><h2>Document library</h2><p>Metadata is returned by the tenant-scoped API.</p></div><button className="button" type="button" onClick={props.onRefresh}>Refresh list</button></div>
      {props.error && props.collections.length > 0 && <p className="inline-error" role="alert">{props.error}</p>}
      {props.documents.length > 0 ? <DocumentTable documents={props.documents} onSelect={props.onSelectDocument} /> : <div className="inline-empty"><strong>No documents found.</strong><p>Upload a PDF after choosing a collection. The list will show its server-reported state.</p></div>}
      {props.selectedDocument && <DocumentVersionsView document={props.selectedDocument} onRefreshDocuments={props.onRefresh} onDeleted={props.onDocumentDeleted} onJobCreated={props.onJobCreated} />}
    </section>
  </div>
}

function CollectionsView(props: {
  collections: Collection[]; error: string; busy: boolean; name: string; setName: (value: string) => void
  description: string; setDescription: (value: string) => void; classification: string; setClassification: (value: string) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void; onRefresh: () => void
}) {
  return <div className="phase2-layout collection-layout">
    <section className="work-panel">
      <div className="panel-heading"><div><h2>New collection</h2><p>Collections are scoped to the verified tenant.</p></div></div>
      <form className="form-stack" onSubmit={props.onSubmit}>
        <div className="field"><label htmlFor="collection-name">Collection name</label><input id="collection-name" value={props.name} onChange={event => props.setName(event.target.value)} maxLength={160} required /></div>
        <div className="field"><label htmlFor="collection-description">Description <span className="optional-label">Optional</span></label><textarea id="collection-description" rows={3} value={props.description} onChange={event => props.setDescription(event.target.value)} maxLength={1000} /></div>
        <div className="field"><label htmlFor="collection-classification">Classification</label><select id="collection-classification" value={props.classification} onChange={event => props.setClassification(event.target.value)}><option value="internal">Internal</option><option value="confidential">Confidential</option><option value="public">Public</option></select></div>
        {props.error && <p className="inline-error" role="alert">{props.error}</p>}
        <button className="button primary" type="submit" disabled={props.busy || !props.name.trim()}>{props.busy ? 'Creating…' : 'Create collection'}</button>
      </form>
    </section>
    <section className="work-panel">
      <div className="panel-heading"><div><h2>Collections</h2><p>{props.collections.length} returned for this session</p></div><button className="button" type="button" onClick={props.onRefresh}>Refresh list</button></div>
      {props.collections.length === 0 ? <div className="inline-empty"><strong>No collections yet.</strong><p>Create a collection to organize source documents.</p></div> : <ul className="collection-list">{props.collections.map((collection, index) => <li key={resourceID(collection) || `${collection.name}-${index}`}><div className="collection-mark" aria-hidden="true">▣</div><div className="collection-copy"><strong>{collection.name || 'Unnamed collection'}</strong><p>{collection.description || 'No description provided.'}</p></div><span className="classification-label">{collection.classification || 'internal'}</span></li>)}</ul>}
    </section>
  </div>
}

function SearchView(props: {
  query: string; setQuery: (value: string) => void; mode: 'keyword' | 'semantic' | 'hybrid'; setMode: (value: 'keyword' | 'semantic' | 'hybrid') => void
  busy: boolean; searched: boolean; error: string; results: SearchResult[]; onSubmit: (event: FormEvent<HTMLFormElement>) => void
}) {
  return <>
    <section className="work-panel search-panel">
      <div className="panel-heading"><div><h2>Find source evidence</h2><p>Results include source citations. Access is checked by the server for each result.</p></div></div>
      <form className="search-form" onSubmit={props.onSubmit}>
        <div className="field search-query"><label htmlFor="knowledge-query">Search terms</label><input id="knowledge-query" value={props.query} onChange={event => props.setQuery(event.target.value)} placeholder="Search authorized source documents" required /></div>
        <div className="field search-mode"><label htmlFor="search-mode">Search mode</label><select id="search-mode" value={props.mode} onChange={event => props.setMode(event.target.value as 'keyword' | 'semantic' | 'hybrid')}><option value="keyword">Keyword</option><option value="semantic">Semantic</option><option value="hybrid">Hybrid</option></select></div>
        <button className="button primary search-submit" type="submit" disabled={props.busy || !props.query.trim()}>{props.busy ? 'Searching…' : 'Search sources'}</button>
      </form>
    </section>
    {props.error && <div className="notice notice-warning search-unavailable" role="alert"><strong>Search is unavailable.</strong><span>{props.error}</span>{props.mode !== 'keyword' && <span>Choose Keyword mode only if lexical search meets your need; no automatic mode substitution was made.</span>}</div>}
    {props.searched && !props.error && !props.busy && props.results.length === 0 && <div className="inline-empty search-empty"><strong>No authorized results found.</strong><p>Try different terms or check whether the source has finished indexing.</p></div>}
    {props.results.length > 0 && <section className="search-results" aria-label="Search results"><div className="results-heading"><div><h2>Source excerpts</h2><p>{props.results.length} results · {props.mode} search</p></div></div>{props.results.map((result, index) => <SearchResultCard key={result.chunk_id || `${result.document_id}-${index}`} result={result} />)}</section>}
  </>
}

function SearchResultCard({ result }: { result: SearchResult }) {
  const path = Array.isArray(result.heading_path) ? result.heading_path.join(' / ') : result.heading_path
  const pageCitation = result.page_start === undefined ? '' : result.page_end && result.page_end !== result.page_start ? `Pages ${result.page_start}–${result.page_end}` : `Page ${result.page_start}`
  const citation = result.citation || [result.title, pageCitation, path].filter(Boolean).join(' · ')
  const score = typeof result.relevance_score === 'number' ? result.relevance_score.toFixed(2) : null
  return <article className="result-card">
    <div className="result-meta"><span className="result-source">{result.title || 'Source document'}</span>{result.trust_status && <span className="trust-label">{result.trust_status.replace(/_/g, ' ')}</span>}</div>
    {result.snippet && <p className="result-snippet">{result.snippet}</p>}
    {citation && <p className="result-citation"><span aria-hidden="true">⌖</span> {citation}</p>}
    <div className="result-foot"><span>{result.matched_reason || 'Source excerpt'}</span>{score && <span>Relevance {score}</span>}</div>
  </article>
}

function DocumentVersionsView(props: {
  document: DocumentRecord
  onRefreshDocuments: () => void
  onDeleted: () => void
  onJobCreated: (job: IngestionJob) => void
}) {
  const documentID = resourceID(props.document)
  const [versions, setVersions] = useState<VersionRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [rationales, setRationales] = useState<Record<string, string>>({})
  const [reviewNotes, setReviewNotes] = useState<Record<string, string>>({})
  const [expiryValues, setExpiryValues] = useState<Record<string, string>>({})
  const [newVersionFile, setNewVersionFile] = useState<File | null>(null)
  const [deleteConfirmed, setDeleteConfirmed] = useState(false)

  const loadVersions = useCallback(async () => {
    if (!documentID) return
    setLoading(true)
    try {
      const data = await request<{ versions?: VersionRecord[] }>(`/api/v1/documents/${encodeURIComponent(documentID)}/versions`)
      setVersions(Array.isArray(data.versions) ? data.versions : [])
      setError('')
    } catch (caught) {
      setError(governanceError(caught, 'Version history could not be loaded.'))
    } finally {
      setLoading(false)
    }
  }, [documentID])

  useEffect(() => { void loadVersions() }, [loadVersions])

  const can = (capability: string) => versions.some(version => version.capabilities?.includes(capability))
  const rationaleFor = (versionID: string) => rationales[versionID] ?? ''
  const setRationaleFor = (versionID: string, value: string) => setRationales(current => ({ ...current, [versionID]: value }))

  async function mutate(path: string, method: 'POST' | 'PATCH' | 'DELETE', body: unknown) {
    setBusy(true)
    setError('')
    setStatus('')
    try {
      const result = await request<{ status?: string }>(path, {
        method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
      })
      setStatus(mutationLabel(result.status))
      await Promise.all([loadVersions(), Promise.resolve(props.onRefreshDocuments())])
      return true
    } catch (caught) {
      setError(governanceError(caught, 'The governance action could not be completed.'))
      return false
    } finally {
      setBusy(false)
    }
  }

  async function submitForReview(event: FormEvent<HTMLFormElement>, version: VersionRecord) {
    event.preventDefault()
    const note = reviewNotes[version.id]?.trim() ?? ''
    if (!note) return
    const completed = await mutate(`/api/v1/documents/${encodeURIComponent(documentID)}/submit-review`, 'POST', { version_id: version.id, note })
    if (completed) setReviewNotes(current => ({ ...current, [version.id]: '' }))
  }

  async function changePublication(version: VersionRecord, action: 'publish' | 'revoke') {
    const rationale = rationaleFor(version.id).trim()
    if (!rationale) return
    const completed = await mutate(`/api/v1/documents/${encodeURIComponent(documentID)}/versions/${encodeURIComponent(version.id)}/${action}`, 'POST', { rationale })
    if (completed) setRationaleFor(version.id, '')
  }

  async function setExpiry(event: FormEvent<HTMLFormElement>, version: VersionRecord) {
    event.preventDefault()
    const rationale = rationaleFor(version.id).trim()
    if (!rationale) return
    const value = expiryValues[version.id] ?? ''
    const expiresAt = value ? new Date(`${value}T23:59:59`).toISOString() : null
    const completed = await mutate(`/api/v1/documents/${encodeURIComponent(documentID)}/versions/${encodeURIComponent(version.id)}/validity`, 'PATCH', { expires_at: expiresAt, rationale })
    if (completed) setRationaleFor(version.id, '')
  }

  async function uploadVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!newVersionFile) return
    setBusy(true)
    setError('')
    setStatus('')
    const form = new FormData()
    form.set('file', newVersionFile)
    try {
      const result = await request<{ document?: DocumentRecord; job?: IngestionJob }>(`/api/v1/documents/${encodeURIComponent(documentID)}/versions`, { method: 'POST', body: form })
      if (result.job) props.onJobCreated(result.job)
      setNewVersionFile(null)
      const input = window.document.getElementById('next-version-file') as HTMLInputElement | null
      if (input) input.value = ''
      setStatus('New version queued for processing.')
      await Promise.all([loadVersions(), Promise.resolve(props.onRefreshDocuments())])
    } catch (caught) {
      setError(governanceError(caught, 'The new version could not be uploaded.'))
    } finally {
      setBusy(false)
    }
  }

  async function deleteDocument(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const rationale = rationaleFor('__document__').trim()
    if (!rationale || !deleteConfirmed) return
    if (await mutate(`/api/v1/documents/${encodeURIComponent(documentID)}`, 'DELETE', { rationale })) {
      props.onDeleted()
      setStatus('Document access was revoked; cleanup is queued.')
    }
  }

  return <section className="work-panel governance-detail" aria-labelledby="version-history-title">
    <div className="panel-heading"><div><h2 id="version-history-title">Version history</h2><p>{props.document.title || 'Selected document'} · status and actions are provided by the server for this scope.</p></div><button className="button" type="button" onClick={() => void loadVersions()} disabled={loading}>Refresh</button></div>
    {error && <p className="inline-error" role="alert">{error}</p>}
    {status && <p className="governance-success" role="status">{status}</p>}
    {loading ? <div className="inline-empty" role="status">Loading version history…</div> : versions.length === 0 ? <div className="inline-empty"><strong>No version history available.</strong><p>The API returned no versions for this document.</p></div> : <div className="version-list">{versions.map(version => <article className="version-card" key={version.id}>
      <div className="version-head"><div><strong>Version {version.version_number}</strong><span className="document-id">{version.id}</span></div><span className={`job-state ${version.publication_status === 'revoked' || version.review_status === 'rejected' ? 'failed' : ''}`}>{version.publication_status || version.review_status || 'Status unavailable'}</span></div>
      <dl className="version-facts">
        <div><dt>Processing</dt><dd>{displayStatus(version.parser_status)} · {displayStatus(version.index_status)}</dd></div>
        <div><dt>Review</dt><dd>{displayStatus(version.review_status)}</dd></div>
        <div><dt>Trust</dt><dd>{displayStatus(version.trust_status)}</dd></div>
        <div><dt>Created</dt><dd>{version.created_at ? new Date(version.created_at).toLocaleString() : 'Not provided'}</dd></div>
        <div><dt>Valid until</dt><dd>{version.expires_at ? new Date(version.expires_at).toLocaleDateString() : 'No expiry set'}</dd></div>
        {version.checksum_sha256 && <div className="version-checksum"><dt>SHA-256</dt><dd>{version.checksum_sha256}</dd></div>}
      </dl>
      {version.capabilities?.includes('submit_review') && (version.review_status === 'draft' || version.review_status === 'rejected') && <form className="governance-action" onSubmit={event => void submitForReview(event, version)}>
        <div className="field"><label htmlFor={`submit-note-${version.id}`}>Submission note</label><textarea id={`submit-note-${version.id}`} value={reviewNotes[version.id] ?? ''} onChange={event => setReviewNotes(current => ({ ...current, [version.id]: event.target.value }))} maxLength={2000} required /></div>
        <button className="button primary" type="submit" disabled={busy || !reviewNotes[version.id]?.trim()}>Submit version for review</button>
      </form>}
      {(version.capabilities?.includes('publish') || version.capabilities?.includes('revoke') || version.capabilities?.includes('set_expiry')) && <div className="governance-action">
        <div className="field"><label htmlFor={`governance-rationale-${version.id}`}>Action rationale</label><textarea id={`governance-rationale-${version.id}`} value={rationaleFor(version.id)} onChange={event => setRationaleFor(version.id, event.target.value)} maxLength={2000} required /></div>
        <div className="governance-buttons">
          {version.capabilities?.includes('publish') && version.publication_status !== 'published' && <button className="button primary" type="button" disabled={busy || !rationaleFor(version.id).trim()} onClick={() => void changePublication(version, 'publish')}>Publish approved version</button>}
          {version.capabilities?.includes('revoke') && version.publication_status === 'published' && <button className="button danger" type="button" disabled={busy || !rationaleFor(version.id).trim()} onClick={() => void changePublication(version, 'revoke')}>Revoke publication</button>}
        </div>
        {version.capabilities?.includes('set_expiry') && <form className="expiry-form" onSubmit={event => void setExpiry(event, version)}>
          <div className="field"><label htmlFor={`expiry-${version.id}`}>Expiration date</label><input id={`expiry-${version.id}`} type="date" value={expiryValues[version.id] ?? (version.expires_at ? version.expires_at.slice(0, 10) : '')} onChange={event => setExpiryValues(current => ({ ...current, [version.id]: event.target.value }))} /></div>
          <button className="button" type="submit" disabled={busy || !rationaleFor(version.id).trim()}>Save expiry</button>
          <small>Leave the date empty to clear an existing expiry. A rationale is required.</small>
        </form>}
      </div>}
    </article>)}</div>}
    {can('upload_version') && <form className="version-upload" onSubmit={event => void uploadVersion(event)}>
      <div><h3>Upload a new version</h3><p>The existing version remains available while this version is processed and reviewed.</p></div>
      <div className="field"><label htmlFor="next-version-file">PDF source</label><input id="next-version-file" type="file" accept="application/pdf,.pdf" required onChange={event => setNewVersionFile(event.target.files?.[0] ?? null)} /></div>
      <button className="button primary" type="submit" disabled={busy || !newVersionFile}>{busy ? 'Uploading…' : 'Upload version'}</button>
    </form>}
    {can('delete_document') && <details className="danger-zone">
      <summary>Delete this document and its versions</summary>
      <p>Access is revoked immediately. Stored object cleanup runs in the background; the audit record remains.</p>
      <form className="form-stack" onSubmit={event => void deleteDocument(event)}>
        <div className="field"><label htmlFor="document-delete-rationale">Deletion rationale</label><textarea id="document-delete-rationale" value={rationaleFor('__document__')} onChange={event => setRationaleFor('__document__', event.target.value)} maxLength={2000} required /></div>
        <label className="confirm-delete"><input type="checkbox" checked={deleteConfirmed} onChange={event => setDeleteConfirmed(event.target.checked)} />I understand deletion revokes access and queues storage cleanup.</label>
        <button className="button danger" type="submit" disabled={busy || !rationaleFor('__document__').trim() || !deleteConfirmed}>Delete document</button>
      </form>
    </details>}
  </section>
}

function ReviewQueueView() {
  const [reviews, setReviews] = useState<ReviewRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [busyReview, setBusyReview] = useState('')
  const [error, setError] = useState('')
  const [notes, setNotes] = useState<Record<string, string>>({})
  const [status, setStatus] = useState('')

  const loadReviews = useCallback(async () => {
    setLoading(true)
    try {
      const data = await request<{ reviews?: ReviewRecord[] }>('/api/v1/reviews')
      setReviews(Array.isArray(data.reviews) ? data.reviews : [])
      setError('')
    } catch (caught) {
      setError(governanceError(caught, 'The review queue could not be loaded.'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void loadReviews() }, [loadReviews])

  async function decide(review: ReviewRecord, decision: 'approve' | 'reject') {
    const rationale = notes[review.id]?.trim() ?? ''
    if (!rationale) return
    setBusyReview(review.id)
    setError('')
    setStatus('')
    try {
      const result = await request<{ status?: string }>(`/api/v1/reviews/${encodeURIComponent(review.id)}/${decision}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ rationale }),
      })
      setStatus(mutationLabel(result.status))
      setNotes(current => ({ ...current, [review.id]: '' }))
      await loadReviews()
    } catch (caught) {
      setError(governanceError(caught, 'The review decision was not accepted.'))
    } finally {
      setBusyReview('')
    }
  }

  return <section className="governance-page" aria-labelledby="review-queue-title">
    <div className="panel-heading"><div><h2 id="review-queue-title">Pending version reviews</h2><p>Decisions apply to the specific version shown. The server checks reviewer scope and blocks self-approval.</p></div><button className="button" type="button" onClick={() => void loadReviews()} disabled={loading}>Refresh queue</button></div>
    {error && <div className="notice notice-warning" role="alert"><strong>Review queue unavailable.</strong><span>{error}</span></div>}
    {status && <p className="governance-success" role="status">{status}</p>}
    {loading ? <div className="inline-empty" role="status">Loading review queue…</div> : !error && reviews.length === 0 ? <div className="inline-empty"><strong>No pending reviews are available.</strong><p>Only reviews within your granted collection or case scope are shown.</p></div> : <div className="review-list">{reviews.map(review => <article className="review-card" key={review.id}>
      <div className="review-heading"><div><span className="review-version">Version {review.version_number ?? '—'}</span><h3>{review.document_title || 'Document review'}</h3><p>Document {shortID(review.document_id)} · Version {shortID(review.version_id)}</p></div><span className="job-state">{displayStatus(review.status)}</span></div>
      <div className="review-note"><strong>Contributor note</strong><p>{review.submission_note || 'No note provided.'}</p></div>
      {review.capabilities?.includes('review') ? <form className="form-stack" onSubmit={event => { event.preventDefault(); void decide(review, 'approve') }}>
        <div className="field"><label htmlFor={`decision-rationale-${review.id}`}>Decision rationale</label><textarea id={`decision-rationale-${review.id}`} value={notes[review.id] ?? ''} onChange={event => setNotes(current => ({ ...current, [review.id]: event.target.value }))} maxLength={2000} required /></div>
        <div className="governance-buttons"><button className="button primary" type="submit" disabled={busyReview === review.id || !notes[review.id]?.trim()}>Approve this version</button><button className="button danger" type="button" disabled={busyReview === review.id || !notes[review.id]?.trim()} onClick={() => void decide(review, 'reject')}>Reject this version</button></div>
      </form> : <p className="inline-empty review-not-authorized">No review action is granted for this item.</p>}
    </article>)}</div>}
  </section>
}

function AuditLogView({ collections }: { collections: Collection[] }) {
  const [collectionID, setCollectionID] = useState('')
  const [activity, setActivity] = useState<ActivityRecord[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!collectionID) { setActivity([]); setError(''); return }
    let active = true
    setLoading(true)
    request<{ activity?: ActivityRecord[] }>(`/api/v1/activity?${new URLSearchParams({ collection_id: collectionID })}`)
      .then(data => { if (active) { setActivity(Array.isArray(data.activity) ? data.activity : []); setError('') } })
      .catch(caught => { if (active) { setActivity([]); setError(governanceError(caught, 'Activity could not be loaded.')) } })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [collectionID])

  return <section className="governance-page" aria-labelledby="audit-log-title">
    <div className="panel-heading"><div><h2 id="audit-log-title">Collection activity</h2><p>Events are append-only. Access is checked for the selected collection.</p></div></div>
    <div className="field audit-scope"><label htmlFor="audit-collection">Collection scope</label><select id="audit-collection" value={collectionID} onChange={event => setCollectionID(event.target.value)}><option value="">Choose a collection</option>{collections.map(collection => <option key={resourceID(collection)} value={resourceID(collection)}>{collection.name || shortID(resourceID(collection))}</option>)}</select></div>
    {error && <div className="notice notice-warning" role="alert"><strong>Activity unavailable.</strong><span>{error}</span></div>}
    {!collectionID && <div className="inline-empty"><strong>Select a collection.</strong><p>Audit records are loaded only for an explicitly selected scope.</p></div>}
    {collectionID && loading && <div className="inline-empty" role="status">Loading collection activity…</div>}
    {collectionID && !loading && !error && activity.length === 0 && <div className="inline-empty"><strong>No activity found for this collection.</strong><p>Governance and retrieval events will appear here when they are recorded.</p></div>}
    {collectionID && activity.length > 0 && <ol className="activity-list">{activity.map(event => <li className="activity-entry" key={event.id}>
      <time dateTime={event.created_at}>{event.created_at ? new Date(event.created_at).toLocaleString() : 'Time unavailable'}</time>
      <div className="activity-copy"><strong>{(event.action || 'Activity').replace(/[._]/g, ' ')}</strong><span>{event.actor_type || 'actor'} {shortID(event.actor_id)} · {event.outcome || 'outcome unavailable'}</span>{event.rationale && <p>{event.rationale}</p>}<small>{event.resource_type || 'resource'} {shortID(event.resource_id)}{event.version_id ? ` · Version ${shortID(event.version_id)}` : ''}</small></div>
    </li>)}</ol>}
  </section>
}

function governanceError(error: unknown, fallback: string) {
  if (error instanceof Error && 'status' in error) {
    const status = (error as ApiFailure).status
    if (status === 401) return 'The host session is missing or expired. Reopen the mini-app through the host.'
    if (status === 403) return 'Your server-side permissions do not allow this action or collection scope.'
    if (status === 404) return 'This resource is no longer available in the selected scope.'
  }
  return messageFor(error, fallback)
}

function mutationLabel(status?: string) {
  const labels: Record<string, string> = {
    submitted: 'Version submitted for review.', approved: 'Version approved.', rejected: 'Version rejected.',
    published: 'Version published.', revoked: 'Publication revoked.', updated: 'Validity updated.', deleted: 'Document access revoked and cleanup queued.',
  }
  return status ? labels[status] ?? `Action completed: ${status.replace(/_/g, ' ')}.` : 'Action completed.'
}

function ProcessingView(props: { jobs: IngestionJob[]; titles: Map<string, string>; error: string; onRefresh: (jobID: string) => void; onRetry: (jobID: string) => void }) {
  return <section className="work-panel">
    <div className="panel-heading"><div><h2>Ingestion jobs</h2><p>Processing status is separate from review or publication state.</p></div></div>
    {props.error && <p className="inline-error" role="alert">{props.error}</p>}
    {props.jobs.length === 0 ? <div className="inline-empty"><strong>No ingestion jobs yet.</strong><p>Uploads and new versions appear here with their current processing state.</p></div> : <ul className="job-list">{props.jobs.map((job, index) => {
      const id = resourceID(job)
      const status = job.status?.toLowerCase()
      return <li key={id || index}>
        <div className="job-icon" aria-hidden="true">◷</div>
        <div className="job-copy"><strong>{(job.document_id && props.titles.get(job.document_id)) || (job.document_id ? `Document ${job.document_id.slice(0, 8)}` : 'Document processing')}</strong><p>{job.last_error ? `${displayStatus(job.stage)}: ${job.last_error}` : `Job ${id ? id.slice(0, 8) : 'pending'} · ${displayStatus(job.status, job.stage)}${job.attempts && job.attempts > 1 ? ` · attempt ${job.attempts}` : ''}`}</p></div>
        <span className={`job-state ${status === 'failed' ? 'failed' : ''}`}>{displayStatus(job.status)}</span>
        <div className="job-actions"><button className="button" type="button" disabled={!id} onClick={() => id && props.onRefresh(id)}>Refresh</button>{status === 'failed' && <button className="button" type="button" disabled={!id} onClick={() => id && props.onRetry(id)}>Retry</button>}</div>
      </li>
    })}</ul>}
  </section>
}

function DocumentTable({ documents, onSelect }: { documents: DocumentRecord[]; onSelect?: (document: DocumentRecord) => void }) {
  return <div className="table-wrap"><table className="data-table"><thead><tr><th scope="col">Document</th><th scope="col">Collection</th><th scope="col">Processing</th><th scope="col">Created</th>{onSelect && <th scope="col">Governance</th>}</tr></thead><tbody>{documents.map((document, index) => {
    const id = resourceID(document)
    const status = documentState(document)
    const date = document.created_at
    return <tr key={id || index}><th scope="row"><span className="document-name">{document.title || 'Untitled source'}</span><span className="document-id">{id ? id.slice(0, 12) : 'ID unavailable'}</span></th><td>{shortID(document.collection_id) || 'Collection unavailable'}</td><td><span className={`job-state ${document.job_status === 'failed' ? 'failed' : ''}`}>{status}</span>{document.version_number ? <span className="document-id">Version {document.version_number}</span> : null}</td><td>{date ? new Date(date).toLocaleDateString() : '—'}</td>{onSelect && <td><button className="button compact" type="button" onClick={() => onSelect(document)}>Versions &amp; actions</button></td>}</tr>
  })}</tbody></table></div>
}

const dependencyLabels: Record<keyof OpsStatus['dependencies'], string> = {
  postgres: 'PostgreSQL', elasticsearch: 'Elasticsearch', object_storage: 'Object storage', docling: 'Docling Serve', embeddings: 'Embedding provider',
}

function SettingsView() {
  const { status, error, loading, reload } = useOpsStatus(true)
  return <section className="work-panel" aria-labelledby="settings-title" aria-busy={loading}>
    <div className="panel-heading"><div><h2 id="settings-title">Integration status</h2><p>Read-only health for this tenant. Configuration and secrets stay with the host environment.</p></div><button className="button" type="button" onClick={() => void reload()} disabled={loading}>{loading ? 'Checking…' : 'Check again'}</button></div>
    {error && <div className="notice notice-warning" role="alert"><strong>Status unavailable.</strong><span>{error}</span></div>}
    {!status && !error && <div className="inline-empty" role="status">Checking integrations…</div>}
    {status && <div className="settings-grid">
      <article className="settings-card"><h3>Dependencies</h3><dl className="status-list">{(Object.keys(dependencyLabels) as (keyof OpsStatus['dependencies'])[]).map(key => <div key={key}><dt>{dependencyLabels[key]}</dt><dd><span className={`health-pill ${healthTone(status.dependencies[key])}`}>{displayStatus(status.dependencies[key])}</span></dd></div>)}</dl></article>
      <article className="settings-card"><h3>Search index</h3><dl className="status-list">
        <div><dt>Index</dt><dd>{status.search_index.name || '—'}{status.search_index.alias_targets.length > 0 ? ` → ${status.search_index.alias_targets.join(', ')}` : ''}</dd></div>
        <div><dt>Indexed chunks</dt><dd>{status.search_index.indexed_chunks ?? '—'} of {status.search_index.expected_chunks} expected</dd></div>
        <div><dt>Consistency</dt><dd><span className={`health-pill ${status.search_index.in_sync ? 'good' : status.search_index.in_sync === false ? 'warn' : 'bad'}`}>{status.search_index.in_sync ? 'in sync' : status.search_index.in_sync === false ? 'catching up' : 'unknown'}</span></dd></div>
        <div><dt>Embeddings</dt><dd>{status.embedding.enabled ? `${status.embedding.model_id} · ${status.embedding.dimensions} dimensions` : 'Not configured (keyword search only)'}</dd></div>
      </dl></article>
      <article className="settings-card"><h3>Ingestion jobs</h3><QueueList counts={status.ingestion} completedLabel="Indexed" /></article>
      <article className="settings-card"><h3>Index &amp; storage cleanup</h3><QueueList counts={status.outbox} completedLabel="Done" /><p className="settings-hint">Failed cleanup events are retried by operators with <code>kms-admin requeue-outbox</code>.</p></article>
      <article className="settings-card"><h3>Session &amp; deployment</h3><dl className="status-list">
        <div><dt>Authentication</dt><dd>{status.auth_mode === 'mock' ? 'Development identity (not for production)' : 'Trusted host session'}</dd></div>
        <div><dt>Environment</dt><dd>{status.environment || '—'}</dd></div>
        <div><dt>Schema version</dt><dd>{status.schema_version ?? '—'}</dd></div>
        <div><dt>Checked</dt><dd>{status.checked_at ? new Date(status.checked_at).toLocaleString() : '—'}</dd></div>
      </dl></article>
    </div>}
  </section>
}

function QueueList({ counts, completedLabel }: { counts: QueueCounts; completedLabel: string }) {
  return <dl className="status-list">
    <div><dt>Queued</dt><dd>{counts.queued}</dd></div>
    <div><dt>Processing</dt><dd>{counts.processing}</dd></div>
    <div><dt>Failed</dt><dd><span className={`health-pill ${counts.failed > 0 ? 'bad' : 'good'}`}>{counts.failed}</span></dd></div>
    <div><dt>{completedLabel}</dt><dd>{counts.completed}</dd></div>
    <div><dt>Oldest waiting</dt><dd>{counts.oldest_pending_seconds > 0 ? formatAge(counts.oldest_pending_seconds) : 'none'}</dd></div>
  </dl>
}

function healthTone(state: string) {
  if (state === 'ok' || state === 'configured') return 'good'
  if (state === 'disabled' || state === 'not_configured') return 'neutral'
  return 'bad'
}

function formatAge(seconds: number) {
  if (seconds < 90) return `${Math.round(seconds)}s`
  if (seconds < 5400) return `${Math.round(seconds / 60)} min`
  return `${Math.round(seconds / 3600)} h`
}

function shortID(value?: string) {
  return value ? value.slice(0, 12) : ''
}

function messageFor(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) return error.message
  return fallback
}

function pageDescription(page: Page) {
  const descriptions: Record<Page, string> = {
    Overview: 'A governed home for source documents and evidence.',
    Documents: 'Upload PDF sources and follow their ingestion state.',
    Collections: 'Organize knowledge by tenant-scoped collection and classification.',
    'Review queue': 'Review decisions are tied to a specific immutable document version.',
    'Evidence search': 'Search authorized source excerpts with version and page citations.',
    Processing: 'Track durable ingestion jobs, parser outcomes and retries.',
    'Audit log': 'Append-only governance events for collections you can administer.',
    Settings: 'Integration health, search index consistency and queue backlog.',
  }
  return descriptions[page]
}
