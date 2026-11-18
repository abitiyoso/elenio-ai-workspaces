import { useMemo, useState } from "react";
import {
  ArrowDownAZ,
  ChevronRight,
  FileText,
  Folder,
  Library,
  Search,
  ShieldCheck,
  Upload,
} from "lucide-react";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";
import { documents, type KnowledgeDocument } from "./fixtures";

export function Knowledge() {
  const [collection, setCollection] = useState("All documents"),
    [query, setQuery] = useState(""),
    [status, setStatus] = useState("All statuses"),
    [sort, setSort] = useState(false),
    [selected, setSelected] = useState<KnowledgeDocument | null>(null),
    [tab, setTab] = useState("Documents");
  const filtered = useMemo(
    () =>
      documents
        .filter(
          (d) =>
            (collection === "All documents" || d.collection === collection) &&
            d.name.toLowerCase().includes(query.toLowerCase()) &&
            (status === "All statuses" || d.status === status),
        )
        .sort((a, b) =>
          sort
            ? a.name.localeCompare(b.name)
            : documents.indexOf(a) - documents.indexOf(b),
        ),
    [collection, query, status, sort],
  );
  return (
    <section className="page knowledge-page">
      <header className="page-heading">
        <div>
          <span className="eyebrow">CAPABILITY LIBRARY</span>
          <h1>Knowledge Hub</h1>
          <p>Trusted sources. Clear provenance. Better decisions.</p>
        </div>
        <div title="Live KMS session required. Current ingestion supports PDF only.">
          <Button disabled>
            <Upload size={16} /> Ingest PDF
          </Button>
          <small className="disabled-reason">Requires connected KMS</small>
        </div>
      </header>
      <div className="knowledge-tabs tabs">
        {["Documents", "Ingestion jobs", "Retrieval playground"].map((t) => (
          <button
            key={t}
            className={tab === t ? "active" : ""}
            onClick={() => setTab(t)}
          >
            {t}
          </button>
        ))}
      </div>
      {tab !== "Documents" ? (
        <div className="surface">
          <div className="e-empty">
            <Library size={30} />
            <h3>
              {tab === "Ingestion jobs"
                ? "No live ingestion queue connected"
                : "Retrieval needs a scoped KMS session"}
            </h3>
            <p>
              {tab === "Ingestion jobs"
                ? "Fixture document statuses do not represent active jobs."
                : "Only authorized, valid, published evidence is eligible. This preview does not call a retrieval model."}
            </p>
            <Button onClick={() => setTab("Documents")}>
              Browse fixture documents
            </Button>
          </div>
        </div>
      ) : (
        <div className="knowledge-layout">
          <aside className="collections">
            <span className="eyebrow">COLLECTIONS</span>
            {[
              "All documents",
              "Finance",
              "Governance",
              "People",
              "Engineering",
            ].map((c) => (
              <button
                key={c}
                className={collection === c ? "active" : ""}
                onClick={() => setCollection(c)}
              >
                {c === "All documents" ? (
                  <Library size={17} />
                ) : (
                  <Folder size={17} />
                )}
                <span>{c}</span>
                <small>
                  {c === "All documents"
                    ? documents.length
                    : documents.filter((d) => d.collection === c).length}
                </small>
              </button>
            ))}
            <div className="knowledge-scope">
              <ShieldCheck size={19} />
              <strong>Workspace scoped</strong>
              <p>
                Access and publication are enforced by KMS in a connected
                environment.
              </p>
            </div>
          </aside>
          <div className="document-workspace">
            <div className="document-toolbar">
              <label className="search-input">
                <Search size={17} />
                <input
                  aria-label="Search documents"
                  placeholder="Search documents…"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </label>
              <select
                className="filter-select"
                aria-label="Filter by publication status"
                value={status}
                onChange={(e) => setStatus(e.target.value)}
              >
                {[
                  "All statuses",
                  "Published",
                  "In review",
                  "Indexing",
                  "Revoked",
                ].map((s) => (
                  <option key={s}>{s}</option>
                ))}
              </select>
              <Button
                aria-label="Toggle alphabetical sort"
                aria-pressed={sort}
                onClick={() => setSort((v) => !v)}
              >
                <ArrowDownAZ size={17} />
              </Button>
            </div>
            <div className="document-list-heading">
              <span>{collection}</span>
              <small>{filtered.length} synthetic documents</small>
            </div>
            <div className="document-table-wrap">
              <table className="document-table">
                <thead>
                  <tr>
                    <th>Document</th>
                    <th>Status</th>
                    <th>Owner</th>
                    <th>Updated</th>
                    <th>
                      <span className="sr-only">Open</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((d) => (
                    <tr key={d.id}>
                      <td>
                        <button
                          className="document-open"
                          onClick={() => setSelected(d)}
                        >
                          <span className="file-icon">
                            <FileText size={21} />
                          </span>
                          <span>
                            <strong>{d.name}</strong>
                            <small>
                              {d.collection} · PDF · v{d.version} · {d.pages}{" "}
                              pages
                            </small>
                          </span>
                        </button>
                      </td>
                      <td>
                        <Badge
                          tone={
                            d.status === "Published"
                              ? "green"
                              : d.status === "In review"
                                ? "amber"
                                : d.status === "Revoked"
                                  ? "red"
                                  : "blue"
                          }
                        >
                          {d.status}
                        </Badge>
                      </td>
                      <td>{d.owner}</td>
                      <td>{d.updated}</td>
                      <td>
                        <button
                          className="row-open"
                          aria-label={`Preview ${d.name}`}
                          onClick={() => setSelected(d)}
                        >
                          <ChevronRight size={17} />
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {!filtered.length && (
              <div className="e-empty">
                <Search size={28} />
                <h3>No matching documents</h3>
                <p>Try another collection, status or search term.</p>
                <Button
                  onClick={() => {
                    setCollection("All documents");
                    setQuery("");
                    setStatus("All statuses");
                  }}
                >
                  Clear filters
                </Button>
              </div>
            )}
            <footer className="document-footer">
              <span>
                <span className="canvas-dot" />
                Synthetic content for design review
              </span>
              <span>PDF ingestion contract · versions preserved</span>
            </footer>
          </div>
        </div>
      )}
      {selected && (
        <DialogPanel
          title={selected.name}
          description={`Synthetic source · ${selected.collection} · version ${selected.version} · ${selected.pages} pages`}
          onClose={() => setSelected(null)}
          wide
        >
          <div className="preview-meta">
            <Badge tone={selected.status === "Published" ? "green" : "amber"}>
              {selected.status}
            </Badge>
            <span>{selected.owner}</span>
          </div>
          <article className="document-preview">{selected.content}</article>
          <Notice>
            {selected.status === "Published"
              ? "Illustrative citation: " +
                selected.id +
                " · v" +
                selected.version +
                " · section 4. Live citation anchors require the KMS preview contract."
              : "This source is not eligible as published retrieval evidence."}
          </Notice>
          <div className="preview-actions">
            <Button
              disabled
              title="Requires live KMS ACL and independent review"
            >
              {selected.status === "In review"
                ? "Review publication"
                : "View version history"}
            </Button>
            <small>Governance actions require a connected KMS session.</small>
          </div>
        </DialogPanel>
      )}
    </section>
  );
}
