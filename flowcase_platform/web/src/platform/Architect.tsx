import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  ArrowRight,
  ArrowUp,
  Check,
  GitBranch,
  RefreshCw,
  Sparkles,
} from "lucide-react";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";
import { newIdempotencyKey } from "../api/client";
import { validateDocument, type ValidationReport } from "../api/workflows";
import { createDefinition } from "./connectedApi";
import { CapabilityMap } from "./Launchpad";
import { definitionDiff } from "./definitionDiff";
import { inspectGraph, parseDefinition, processEdges } from "./graphModel";
import {
  agentRequest,
  ARCHITECT_KEY,
  getAuthoringRun,
  listAuthoringRuns,
  loadArchitect,
  parseProposal,
  type ArchitectReady,
  type AuthoringRun,
} from "./architectApi";

const settled = (status: string) =>
  ["succeeded", "needs_review", "failed", "cancelled"].includes(status);
function sameScope(a: ArchitectReady, b: ArchitectReady) {
  return (
    a.agent.tenant_id === b.agent.tenant_id &&
    a.agent.subject === b.agent.subject &&
    a.flowcase.user_id === b.flowcase.user_id
  );
}
function errorText(error: unknown) {
  return error instanceof Error
    ? error.message
    : "The request could not be completed.";
}

export function ArchitectBrief({
  brief,
  onBrief,
  onBuild,
  solutionContext,
  onBeforeGenerate,
  onBeforeApply,
  onCreated,
}: {
  brief: string;
  onBrief: (value: string) => void;
  onBuild: () => void;
  solutionContext?: {id: string; revision: number};
  onBeforeGenerate?: () => Promise<void>;
  onBeforeApply?: () => Promise<void>;
  onCreated?: (workflow: {key: string; version: number}) => Promise<void>;
}) {
  const navigate = useNavigate();
  const [ready, setReady] = useState<ArchitectReady | null>(null),
    [readinessError, setReadinessError] = useState(""),
    [epoch, setEpoch] = useState(0);
  const [submitted, setSubmitted] = useState(""),
    [run, setRun] = useState<AuthoringRun | null>(null),
    [history, setHistory] = useState<AuthoringRun[]>([]);
  const [error, setError] = useState(""),
    [sending, setSending] = useState(false),
    [pollError, setPollError] = useState(""),
    [pollEpoch, setPollEpoch] = useState(0);
  const [preview, setPreview] = useState(false),
    [edit, setEdit] = useState<{ run: string; text: string } | null>(null),
    [validated, setValidated] = useState<{
      text: string;
      report: ValidationReport;
    } | null>(null),
    [saving, setSaving] = useState(false);
  const pending = useRef<{ fingerprint: string; key: string } | null>(null),
    creation = useRef<{ fingerprint: string; key: string } | null>(null);
  const conversation = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const pane = conversation.current;
    if (pane) pane.scrollTop = pane.scrollHeight;
  }, [run?.id, run?.status]);
  useEffect(() => {
    const controller = new AbortController();
    loadArchitect(controller.signal)
      .then(async (value) => {
        if (controller.signal.aborted) return;
        setReady(value);
        const recent = await listAuthoringRuns(controller.signal);
        if (!controller.signal.aborted) setHistory(recent.items);
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setReadinessError(errorText(e));
      });
    return () => controller.abort();
  }, [epoch]);
  const runId = run?.id;
  useEffect(() => {
    const id = runId;
    if (!id) return;
    const controller = new AbortController();
    async function watch(currentRunId: string) {
      for (
        let attempt = 0;
        attempt < 180 && !controller.signal.aborted;
        attempt++
      ) {
        const current = await getAuthoringRun(currentRunId, controller.signal);
        if (controller.signal.aborted) return;
        setRun(current);
        setPollError("");
        if (settled(current.status)) {
          const recent = await listAuthoringRuns(controller.signal);
          if (!controller.signal.aborted) setHistory(recent.items);
          return;
        }
        await new Promise<void>((resolve, reject) => {
          const timer = setTimeout(() => {
            controller.signal.removeEventListener("abort", abort);
            resolve();
          }, 1000);
          function abort() {
            clearTimeout(timer);
            reject(new DOMException("Cancelled", "AbortError"));
          }
          controller.signal.addEventListener("abort", abort, { once: true });
        });
      }
      if (!controller.signal.aborted)
        setPollError(
          "The run is still pending. You can resume checking or reopen it from workspace history.",
        );
    }
    watch(id).catch((e: unknown) => {
      if (!controller.signal.aborted) setPollError(errorText(e));
    });
    return () => controller.abort();
  }, [runId, pollEpoch]);
  const result = useMemo(() => {
    if (!run || !["succeeded", "needs_review"].includes(run.status))
      return { proposal: null, error: "" };
    try {
      return { proposal: parseProposal(run), error: "" };
    } catch (e) {
      return { proposal: null, error: errorText(e) };
    }
  }, [run]);
  const workflow = result.proposal?.workflow;
  const jsonText = useMemo(() => {
    if (!workflow || !run) return "";
    if (edit?.run === run.id) return edit.text;
    return JSON.stringify(
      {
        ...workflow,
        key: `${workflow.key.slice(0, 48)}-${run.id.slice(0, 8)}`,
        metadata: {
          ...workflow.metadata,
          architect: {
            run_id: run.id,
            agent_version_id: run.agent_version_id,
            model_mode: run.model_mode,
            ...(solutionContext ? {solution_id: solutionContext.id, solution_revision: solutionContext.revision} : {}),
          },
        },
      },
      null,
      2,
    );
  }, [workflow, run, edit, solutionContext]);
  const draft = useMemo(() => {
    if (!jsonText || !ready) return { definition: null, issues: [], error: "" };
    try {
      const definition = parseDefinition(jsonText);
      if (
        !/^[a-z][a-z0-9-]{2,63}$/.test(definition.key) ||
        definition.version !== 1 ||
        definition.status !== "draft" ||
        definition.tenant_scope !== undefined ||
        definition.nodes.length > 20
      )
        throw new Error(
          "Use a new schema 1.0 draft, a lowercase workflow key and at most 20 nodes; tenant overrides are not allowed.",
        );
      const issues = inspectGraph(
        definition,
        ready.capabilities.supported_node_types,
      );
      return { definition, issues, error: "" };
    } catch (e) {
      return { definition: null, issues: [], error: errorText(e) };
    }
  }, [jsonText, ready]);
  const working = !!run && !settled(run.status);
  async function generate() {
    if (!ready || brief.trim().length < 10 || sending || working) return;
    setSending(true);
    setError("");
    try {
      await onBeforeGenerate?.();
      const fresh = await loadArchitect();
      if (!sameScope(ready, fresh)) {
        setRun(null);
        throw new Error(
          "Your workspace identity changed. Refresh readiness before continuing.",
        );
      }
      const input = {
        requirement: brief.trim(),
        catalog: fresh.catalog,
        supported_node_types: fresh.capabilities.supported_node_types,
      };
      const fingerprint = JSON.stringify({ version: fresh.version.id, input });
      if (pending.current?.fingerprint !== fingerprint)
        pending.current = { fingerprint, key: newIdempotencyKey() };
      const created = await agentRequest<AuthoringRun>("/sandbox/runs", {
        agent_key: ARCHITECT_KEY,
        agent_version_id: fresh.version.id,
        input,
        evidence_fixture: [],
        idempotency_key: pending.current.key,
      });
      pending.current = null;
      setRun(created);
      setSubmitted(input.requirement);
      setValidated(null);
      setPollEpoch((v) => v + 1);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setSending(false);
    }
  }
  async function reopen(id: string) {
    setError("");
    try {
      const value = await getAuthoringRun(id);
      setRun(value);
      onBrief(value.input.requirement ?? "");
      setSubmitted(value.input.requirement ?? "");
      setValidated(null);
      setPollEpoch((v) => v + 1);
    } catch (e) {
      setError(errorText(e));
    }
  }
  async function validate() {
    if (!draft.definition || draft.issues.length) return;
    setSaving(true);
    setError("");
    try {
      setValidated({
        text: jsonText,
        report: await validateDocument(draft.definition),
      });
    } catch (e) {
      setError(errorText(e));
    } finally {
      setSaving(false);
    }
  }
  async function apply() {
    if (
      !ready ||
      !run ||
      !draft.definition ||
      validated?.text !== jsonText ||
      !validated.report.valid
    )
      return;
    setSaving(true);
    setError("");
    try {
      const fresh = await loadArchitect();
      if (!sameScope(ready, fresh))
        throw new Error(
          "Workspace identity changed; the proposal was not applied.",
        );
      const latest = await getAuthoringRun(run.id);
      parseProposal(latest);
      if (solutionContext && latest.input.requirement !== brief.trim())
        throw new Error("The saved Solution requirement differs from this run. Review or generate a matching blueprint before applying.");
      await onBeforeApply?.();
      const report = await validateDocument(draft.definition);
      setValidated({ text: jsonText, report });
      if (!report.valid)
        throw new Error(
          "The server requires corrections before this draft can be created.",
        );
      const fingerprint = JSON.stringify(draft.definition);
      if (creation.current?.fingerprint !== fingerprint)
        creation.current = { fingerprint, key: newIdempotencyKey() };
      const saved = await createDefinition(
        {
          key: draft.definition.key,
          name: draft.definition.name,
          description: draft.definition.description ?? "",
          definition: draft.definition,
        },
        creation.current.key,
      );
      if (onCreated) await onCreated({key: saved.version.workflow_key, version: saved.version.version});
      else navigate(`/connected/projects/${encodeURIComponent(saved.version.workflow_key)}/${saved.version.version}`);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setSaving(false);
    }
  }
  const append = (text: string) =>
    onBrief([brief.trim(), text].filter(Boolean).join("\n\n"));
  function refreshReadiness() {
    setReady(null);
    setReadinessError("");
    setRun(null);
    setHistory([]);
    setEpoch((v) => v + 1);
  }
  async function cancel() {
    if (!run) return;
    try {
      await agentRequest(`/runs/${run.id}/cancel`, {});
      setPollEpoch((v) => v + 1);
    } catch (e) {
      setError(errorText(e));
    }
  }
  const canCreate = !!ready?.flowcase.roles.includes("workflow_designer");
  return (
    <section className="page architect-page">
      <header className="page-heading">
        <div>
          <span className="eyebrow">AI-ASSISTED SOLUTION ENGINEERING</span>
          <h1>AI Solution Architect</h1>
          <p>From business intent to a governed, working solution.</p>
        </div>
        <Button onClick={onBuild}>
          Open Workflow Studio <ArrowRight size={16} />
        </Button>
      </header>
      {error && <Notice error>{error}</Notice>}
      <div className="architect-columns">
        <section
          className="surface architect-conversation"
          aria-label="Solution conversation"
        >
          <header>
            <div>
              <Sparkles size={18} />
              <h2>Solution conversation</h2>
            </div>
            <Badge tone={ready ? "green" : "amber"}>
              {ready ? "AI sandbox" : "Local planning"}
            </Badge>
          </header>
          <div
            className="conversation-history"
            role="log"
            aria-label="Requirement history"
            ref={conversation}
            tabIndex={0}
          >
            <div className="conversation-intro">
              <span className="architect-avatar">
                <Sparkles size={19} />
              </span>
              <div>
                <strong>Start with the outcome.</strong>
                <p>
                  Describe the trigger, inputs, decisions, review owner and
                  intended outcome. Add constraints, then generate a proposal
                  for review.
                </p>
              </div>
            </div>
            {submitted && (
              <div className="requirement-message">
                <span className="eyebrow">YOUR REQUIREMENT</span>
                <p>{submitted}</p>
                <small>
                  <Check size={13} />{" "}
                  {run
                    ? "Saved in the sandbox run"
                    : "Captured for this session"}
                </small>
              </div>
            )}
            {result.proposal && (
              <div className="architect-answer">
                <Badge>
                  {run?.pinned?.model?.target?.provider_model_id ??
                    ready?.model}{" "}
                  · draft
                </Badge>
                <h3>{result.proposal.title}</h3>
                <p>{result.proposal.summary}</p>
                {result.proposal.questions.length > 0 && (
                  <>
                    <h4>Clarify next</h4>
                    <ol>
                      {result.proposal.questions.map((q) => (
                        <li key={q}>{q}</li>
                      ))}
                    </ol>
                  </>
                )}
              </div>
            )}
            {run && (
              <div className="authoring-status" role="status">
                <strong>{run.status.replaceAll("_", " ")}</strong>
                <span>
                  Run {run.id.slice(0, 8)} · {run.tokens_in + run.tokens_out}{" "}
                  reported tokens
                </span>
                {run.status === "failed" && (
                  <Notice error>
                    Generation failed: {run.error_code ?? "runtime error"}. No
                    workflow was changed.
                  </Notice>
                )}
              </div>
            )}
            {pollError && (
              <Notice error>
                {pollError}
                <Button onClick={() => setPollEpoch((v) => v + 1)}>
                  Resume checking
                </Button>
              </Notice>
            )}
            {result.error && <Notice error>{result.error}</Notice>}
          </div>
          <form
            className="architect-composer"
            onSubmit={(e) => {
              e.preventDefault();
              if (ready) void generate();
              else if (brief.trim()) setSubmitted(brief.trim());
            }}
          >
            <div className="scenario-chips" aria-label="Add a constraint">
              <button
                type="button"
                onClick={() =>
                  append(
                    "Require independent human approval before any controlled action.",
                  )
                }
              >
                Add approval
              </button>
              <button
                type="button"
                onClick={() =>
                  append(
                    "Run in the customer environment with approved data residency and endpoints.",
                  )
                }
              >
                Customer-hosted
              </button>
              <button
                type="button"
                onClick={() =>
                  append(
                    "Reuse approved workflows, agents and capabilities before creating new ones.",
                  )
                }
              >
                Reuse capabilities
              </button>
            </div>
            <label className="sr-only" htmlFor="architect-requirement">
              Business requirement
            </label>
            <textarea
              id="architect-requirement"
              rows={4}
              maxLength={12000}
              value={brief}
              onChange={(e) => onBrief(e.target.value)}
              placeholder="Describe your requirement or refine the solution…"
            />
            <div>
              <span>
                {ready
                  ? "Send to the approved model · billable sandbox run"
                  : "Capture this brief locally until services are ready"}
              </span>
              <Button
                type="submit"
                variant="primary"
                disabled={
                  ready
                    ? brief.trim().length < 10 || sending || working
                    : !brief.trim()
                }
                aria-label={ready ? "Send requirement" : "Capture requirement"}
              >
                <ArrowUp size={17} />
              </Button>
            </div>
          </form>
        </section>
        <section
          className="architect-discovery"
          aria-label="Solution discovery"
        >
          <div className="surface architect-blueprint">
            <span className="eyebrow">SOLUTION DISCOVERY</span>
            <h2>
              {result.proposal
                ? result.proposal.title
                : submitted
                  ? "Your requirement is ready to refine"
                  : "Start with a requirement"}
            </h2>
            {ready ? (
              <p>
                Connected to {ready.model}. A bounded, tool-free sandbox run
                produces a draft proposal. Generating sends this requirement and
                the workspace capability catalog to the approved model and may
                use billable tokens.
              </p>
            ) : (
              <Notice>
                Blueprint authoring is not connected or ready.{" "}
                {readinessError ||
                  "Checking verified sessions, agent contract and model approval…"}
              </Notice>
            )}
            {!ready && (
              <Button onClick={refreshReadiness}>
                <RefreshCw size={14} /> Check readiness
              </Button>
            )}
            <div className="blueprint-actions">
              <Button
                variant="primary"
                disabled={
                  !ready || brief.trim().length < 10 || working || sending
                }
                onClick={generate}
              >
                {sending
                  ? "Submitting…"
                  : working
                    ? "Generating…"
                    : "Generate blueprint"}
              </Button>
              {working && ready?.agent.permissions.includes("run.cancel") && (
                <Button onClick={cancel}>Cancel generation</Button>
              )}
              <Button variant="quiet" onClick={onBuild}>
                Explore example <ArrowRight size={14} />
              </Button>
            </div>
            {ready && (
              <small className="authoring-identity">
                {ready.agent.identity_mode === "development"
                  ? "Development identities"
                  : "Verified workspace identity"}{" "}
                · {ready.agent.subject} · agent v{ready.version.semver}
              </small>
            )}
            {result.proposal && (
              <div className="proposal-details">
                <h3>Assumptions to review</h3>
                <ul>
                  {result.proposal.assumptions.map((a) => (
                    <li key={a}>{a}</li>
                  ))}
                </ul>
                <h3>Minimum viable agents</h3>
                {result.proposal.agents.length ? (
                  result.proposal.agents.map((a) => (
                    <p key={a.role}>
                      <strong>{a.role}</strong> · {a.purpose}
                      <small>
                        {a.reuse_key
                          ? `Reuse ${a.reuse_key}`
                          : "Proposed capability · not created"}
                      </small>
                    </p>
                  ))
                ) : (
                  <p>No new agents proposed.</p>
                )}
                <h3>Dependencies</h3>
                {result.proposal.dependencies.map((d) => (
                  <p key={`${d.kind}:${d.name}`}>
                    <Badge tone={d.status === "required" ? "amber" : "neutral"}>
                      {d.status === "available" ? "Proposed reuse" : "Required"}
                    </Badge>{" "}
                    <strong>{d.name}</strong> · {d.reason}
                  </p>
                ))}
                {workflow && (
                  <>
                    <h3>
                      <GitBranch size={15} /> Workflow proposal
                    </h3>
                    <p>
                      {workflow.nodes.length} nodes ·{" "}
                      {processEdges(workflow).length} explicit connections
                    </p>
                    <ol className="proposal-nodes">
                      {workflow.nodes.map((n) => (
                        <li key={n.id}>
                          <strong>{n.name}</strong>
                          <small>
                            {n.type} → {n.next.join(", ") || "complete"}
                          </small>
                        </li>
                      ))}
                    </ol>
                    <Button
                      variant="primary"
                      onClick={() => {
                        setPreview(true);
                        setError("");
                      }}
                    >
                      Preview changes <ArrowRight size={14} />
                    </Button>
                  </>
                )}
              </div>
            )}
          </div>
          {!result.proposal && (
            <CapabilityMap
              dark
              onBuild={onBuild}
              onArchitect={() =>
                document.getElementById("architect-requirement")?.focus()
              }
            />
          )}
          {!!history.length && (
            <section
              className="surface authoring-history"
              aria-label="Workspace proposal history"
            >
              <h2>Workspace proposals</h2>
              <p>Saved sandbox runs · no workflow is applied automatically</p>
              {history.map((item) => (
                <button
                  key={item.id}
                  onClick={() => reopen(item.id)}
                  disabled={sending || working}
                >
                  <span>
                    {item.input.requirement?.slice(0, 70) ||
                      item.id.slice(0, 8)}
                  </span>
                  <small>
                    {item.status.replaceAll("_", " ")} ·{" "}
                    {new Date(item.created_at).toLocaleString()}
                  </small>
                </button>
              ))}
            </section>
          )}
        </section>
      </div>
      {preview && workflow && (
        <DialogPanel
          title="Review workflow changes"
          description="Create a new server draft from this proposal. Edit the canonical definition, validate against the server, then explicitly confirm creation."
          onClose={() => {
            if (!saving) setPreview(false);
          }}
        >
          <Notice>
            New workflow · {draft.definition?.nodes.length ?? 0} nodes. Model
            output is untrusted draft content. Existing workflows, publication
            and production execution are not changed by this action.
          </Notice>
          <label className="e-field">
            Workflow definition
            <textarea
              className="architect-json"
              aria-label="Proposed workflow JSON"
              value={jsonText}
              readOnly={saving}
              onChange={(e) => {
                if (run) setEdit({ run: run.id, text: e.target.value });
              }}
              spellCheck={false}
            />
          </label>
          {draft.error && <Notice error>{draft.error}</Notice>}
          {draft.issues.map((issue, index) => (
            <Notice key={index} error>
              {issue.node}: {issue.message}
            </Notice>
          ))}
          {draft.definition && (
            <p className="proposal-diff">
              Compared with AI proposal:{" "}
              {definitionDiff(workflow, draft.definition).added.length} added
              nodes ·{" "}
              {definitionDiff(workflow, draft.definition).removed.length}{" "}
              removed ·{" "}
              {definitionDiff(workflow, draft.definition).changed.length}{" "}
              changed. Workflow key: {draft.definition.key}
              <br />
              Workflow fields changed:{" "}
              {definitionDiff(workflow, draft.definition).fields.join(", ") ||
                "none"}
            </p>
          )}
          {validated?.text === jsonText && (
            <Notice error={!validated.report.valid}>
              <strong>
                {validated.report.valid
                  ? "Server validation passed."
                  : "Server validation requires corrections."}
              </strong>
              {validated.report.diagnostics.map((d, i) => (
                <p key={i}>
                  {d.path}: {d.message}
                </p>
              ))}
            </Notice>
          )}
          {error && <Notice error>{error}</Notice>}
          {!canCreate && (
            <Notice>Your Flowcase role cannot create workflow drafts.</Notice>
          )}
          <div className="blueprint-actions">
            <Button
              disabled={saving || !draft.definition || !!draft.issues.length}
              onClick={validate}
            >
              Validate proposal
            </Button>
            <Button
              variant="primary"
              disabled={
                saving ||
                !canCreate ||
                !draft.definition ||
                !!draft.issues.length ||
                validated?.text !== jsonText ||
                !validated.report.valid
              }
              onClick={apply}
            >
              {saving ? "Checking…" : "Confirm create draft"}
            </Button>
          </div>
        </DialogPanel>
      )}
    </section>
  );
}
