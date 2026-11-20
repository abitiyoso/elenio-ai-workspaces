import { useEffect, useRef, useState } from "react";
import { BindingEvidence } from "./BindingEvidence";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";
import { newIdempotencyKey } from "../api/client";
import {
  createCandidate,
  listCandidates,
  type Solution,
  type SolutionCandidate,
  type SolutionSession,
} from "./solutionApi";

export function PublisherCandidates({
  plan,
  session,
  onClose,
}: {
  plan: Solution;
  session: SolutionSession;
  onClose: () => void;
}) {
  const [items, setItems] = useState<SolutionCandidate[]>([]),
    [cursor, setCursor] = useState(""),
    [loading, setLoading] = useState(true),
    [saving, setSaving] = useState(false),
    [version, setVersion] = useState("1.0.0"),
    [error, setError] = useState("");
  const request = useRef<{ version: string; key: string }>();
  useEffect(() => {
    const controller = new AbortController();
    listCandidates(session, plan, "", controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        setItems(page.items);
        setCursor(page.next_cursor);
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setError(
            e instanceof Error ? e.message : "Candidate history unavailable.",
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [plan, session]);
  async function create() {
    setSaving(true);
    setError("");
    if (!request.current || request.current.version !== version)
      request.current = { version, key: newIdempotencyKey() };
    try {
      const item = await createCandidate(
        session,
        plan,
        version,
        request.current.key,
      );
      setItems((old) => [item, ...old.filter((i) => i.id !== item.id)]);
      request.current = undefined;
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : "Unable to create candidate. Retry with the same version.",
      );
    } finally {
      setSaving(false);
    }
  }
  async function more() {
    setLoading(true);
    try {
      const page = await listCandidates(session, plan, cursor);
      setItems((old) => [
        ...old,
        ...page.items.filter(
          (i) => !old.some((existing) => existing.id === i.id),
        ),
      ]);
      setCursor(page.next_cursor);
    } catch (e) {
      setError(e instanceof Error ? e.message : "History unavailable.");
    } finally {
      setLoading(false);
    }
  }
  function download(item: SolutionCandidate) {
    const blob = new Blob([JSON.stringify(item, null, 2)], {
        type: "application/json",
      }),
      url = URL.createObjectURL(blob),
      link = document.createElement("a");
    link.href = url;
    link.download = `${plan.definition.key}-candidate-${item.version}.json`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  return (
    <DialogPanel
      title="Publisher candidates"
      description="Immutable planning snapshots for review. Candidates are not approved releases or installable deployment packages."
      onClose={() => {
        if (!saving) onClose();
      }}
      wide
    >
      <Notice>
        Creating a candidate rechecks current workflow evidence and stores the
        exact plan revision with its blockers. It does not publish, execute or
        deploy.
      </Notice>
      {error && <Notice error>{error}</Notice>}
      <form
        className="publisher-create"
        onSubmit={(e) => {
          e.preventDefault();
          void create();
        }}
      >
        <label className="e-field">
          Candidate version
          <input
            required
            pattern="(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
            maxLength={32}
            disabled={
              saving || !session.permissions.includes("solution:author")
            }
            value={version}
            onChange={(e) => setVersion(e.target.value)}
          />
        </label>
        <Button
          type="submit"
          variant="primary"
          disabled={
            saving ||
            loading ||
            !session.permissions.includes("solution:author")
          }
        >
          {saving ? "Capturing server snapshot…" : "Create review candidate"}
        </Button>
      </form>
      {loading && <p role="status">Loading candidate history…</p>}
      {!loading && !items.length && (
        <p>No candidates exist for this solution.</p>
      )}
      <div className="publisher-history">
        {items.map((item) => (
          <article key={item.id}>
            <Badge>Candidate {item.version}</Badge>
            <h3>{item.snapshot.plan.definition.name}</h3>
            <p>
              Plan revision {item.plan_revision} ·{" "}
              {new Date(item.created_at).toLocaleString()}
            </p>
            <p className="publisher-hash">
              Checksum: <code>{item.content_hash}</code>
            </p>
            <Notice>
              Not approved for deployment.{" "}
              {item.snapshot.readiness.blockers.join(" ")}
            </Notice>
            <ul>
              {item.snapshot.readiness.application_issues?.map((reason, i) => (
                <li key={i}>{reason}</li>
              ))}
            </ul>
            <Button onClick={() => download(item)}>
              Download candidate JSON
            </Button>
            {item.snapshot.readiness.workflows.map(workflow => (
              <BindingEvidence key={workflow.key} workflow={workflow} />
            ))}
          </article>
        ))}
      </div>
      {cursor && (
        <Button disabled={loading || saving} onClick={() => void more()}>
          Load more candidates
        </Button>
      )}
    </DialogPanel>
  );
}
