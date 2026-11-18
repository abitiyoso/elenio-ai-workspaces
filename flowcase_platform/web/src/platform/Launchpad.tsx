import { useState } from "react";
import { Link } from "react-router-dom";
import {
  ArrowRight,
  Boxes,
  GitBranch,
  Layers,
  ShieldCheck,
  Sparkles,
  Workflow,
} from "lucide-react";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";

const examples = [
  [
    "Vendor payments",
    "Build a vendor payment workflow: invoices arrive via email, extract details, match against purchase orders, flag discrepancies, route to Finance for approval, then post to ERP.",
  ],
  [
    "Recruitment",
    "Create a recruitment workflow: receive CVs from email, evaluate against job requirements, send recommendations to HR, and schedule interviews after human review.",
  ],
  [
    "Contract review",
    "Review contracts against approved company policies, identify deviations with cited evidence, and route to Legal for a human decision.",
  ],
  [
    "SIM tender",
    "Collect RFP requirements, assess fit-gap, prepare solution design and cost evidence, then route the proposal through independent review.",
  ],
];
const capabilities = [
  {
    name: "AI Architect",
    kind: "architect",
    description:
      "Translate business requirements into an editable blueprint. A model connection is required for generation.",
  },
  {
    name: "Workflow",
    kind: "workflow",
    description:
      "Compose deterministic steps, agents and human decisions using explicit process connections.",
  },
  {
    name: "Agents",
    kind: "agents",
    description:
      "Reusable intelligence with model, skill, tool and scoped knowledge bindings. Connected catalog follows in Phase 2.",
  },
  {
    name: "API / MCP",
    kind: "integrations",
    description:
      "Connect approved enterprise systems. Generic connector registration and execution are not active in this preview.",
  },
  {
    name: "Human review",
    kind: "review",
    description:
      "Independent human authority before controlled actions. No approval decisions are simulated.",
  },
  {
    name: "Company KMS",
    kind: "knowledge",
    description:
      "Versioned documents, scoped access and governed evidence. Explore the document workspace.",
  },
];
export function Launchpad({
  onOpen,
  onBuild,
  onArchitect,
}: {
  onOpen: () => void;
  onBuild: () => void;
  onArchitect: (brief: string) => void;
}) {
  const [brief, setBrief] = useState(""),
    [selected, setSelected] = useState(0),
    [modal, setModal] = useState<"manual" | "templates" | "agent" | null>(null);
  return (
    <section className="page launchpad">
      <header className="launch-welcome">
        <div>
          <span className="eyebrow">YOUR ENGINEERING SPACE</span>
          <h1>Build AI that works.</h1>
          <p>
            Your engineering environment for customer-ready agentic systems.
          </p>
        </div>
        <Badge>Local workspace · 1 project</Badge>
      </header>
      <div className="launch-main">
        <section className="launch-copy">
          <span className="eyebrow">
            <Sparkles size={16} /> AI SOLUTION ARCHITECT
          </span>
          <h2>
            What are we
            <br />
            building <em>today?</em>
          </h2>
          <p>
            Start with a business process. Turn it into agents, workflows and a
            solution your team can run.
          </p>
          <div className="requirement-composer">
            <textarea
              aria-label="Solution brief"
              placeholder="Describe a process you want to automate…"
              value={brief}
              onChange={(e) => setBrief(e.target.value)}
            />
            <div>
              <span>
                <Sparkles size={14} />
                Requirement to blueprint
              </span>
              <Button variant="primary" onClick={() => onArchitect(brief)}>
                Create with AI <ArrowRight size={16} />
              </Button>
            </div>
          </div>
          <div className="scenario-chips">
            {examples.map(([name, text]) => (
              <button
                key={name}
                aria-pressed={brief === text}
                onClick={() => setBrief(text ?? "")}
              >
                {name}
              </button>
            ))}
          </div>
        </section>
        <section className="capability-map">
          <header>
            <span className="eyebrow">LIVING INTELLIGENCE</span>
            <span>SYSTEM MAP</span>
          </header>
          <div className="capability-world">
            <svg viewBox="0 0 560 350" aria-hidden="true">
              <defs>
                <radialGradient id="capability-halo">
                  <stop stopColor="#b1ccfc" stopOpacity=".65" />
                  <stop offset="1" stopColor="#f5f8fc" stopOpacity="0" />
                </radialGradient>
              </defs>
              <ellipse
                cx="280"
                cy="175"
                rx="240"
                ry="170"
                fill="url(#capability-halo)"
              />
              <ellipse
                cx="280"
                cy="175"
                rx="190"
                ry="115"
                fill="none"
                stroke="#bdcce2"
                strokeDasharray="3 7"
              />
              {[
                [95, 90],
                [280, 45],
                [465, 90],
                [465, 260],
                [280, 305],
                [95, 260],
              ].map(([x, y], i) => (
                <path
                  key={i}
                  d={`M280 175 Q${x} 175 ${x} ${y}`}
                  fill="none"
                  stroke={selected === i ? "#4c7cce" : "#c9d7e9"}
                  strokeWidth={selected === i ? 2 : 1}
                />
              ))}
            </svg>
            <div className="capability-core">
              <img src="./brand/elenio-icon.png" alt="Elenio" />
              <span>ORCHESTRATOR</span>
            </div>
            {capabilities.map((c, i) => (
              <button
                className={`capability-orbit capability-${c.kind} ${selected === i ? "selected" : ""}`}
                key={c.kind}
                aria-pressed={selected === i}
                onClick={() => setSelected(i)}
              >
                <span className={`capability-symbol ${c.kind}`}>
                  <i />
                  <i />
                </span>
                <strong>{c.name}</strong>
              </button>
            ))}
          </div>
          <footer>
            <strong>{capabilities[selected]?.name}</strong>
            <p>{capabilities[selected]?.description}</p>
            {selected === 1 ? (
              <button onClick={onBuild}>
                Open Workflow Studio <ArrowRight size={13} />
              </button>
            ) : selected === 5 ? (
              <Link to="/knowledge">
                Open Knowledge Hub <ArrowRight size={13} />
              </Link>
            ) : selected === 0 ? (
              <button onClick={() => onArchitect(brief)}>
                Open AI Architect <ArrowRight size={13} />
              </button>
            ) : (
              <span>Connection required · later phase</span>
            )}
          </footer>
        </section>
      </div>
      <section className="creation-paths" aria-label="Start a solution">
        <button onClick={() => setModal("manual")}>
          <Workflow size={22} />
          <span>
            <strong>Build manually</strong>
            <small>Start with a blank workflow</small>
          </span>
          <ArrowRight size={16} />
        </button>
        <button onClick={() => setModal("templates")}>
          <Layers size={22} />
          <span>
            <strong>Use a template</strong>
            <small>Reuse a solution blueprint</small>
          </span>
          <ArrowRight size={16} />
        </button>
        <Link to="/connected/catalog">
          <Boxes size={22} />
          <span>
            <strong>Create an agent</strong>
            <small>Open the governed capability workspace</small>
          </span>
          <ArrowRight size={16} />
        </Link>
      </section>
      <div className="section-title">
        <div>
          <h2>Pick up where you left off</h2>
          <p>Your solutions, ready to refine.</p>
        </div>
        <Link to="/projects">
          All projects <ArrowRight size={14} />
        </Link>
      </div>
      <button className="recent-project" onClick={onOpen}>
        <span className="project-icon">
          <GitBranch size={24} />
        </span>
        <div>
          <div className="project-name">
            Vendor payment assurance <Badge tone="amber">Draft</Badge>
          </div>
          <p>
            Automate invoice verification, independent approval and ERP posting.
          </p>
        </div>
        <ArrowRight size={18} />
      </button>
      {modal && (
        <DialogPanel
          title={
            modal === "manual"
              ? "Build manually"
              : modal === "templates"
                ? "Template Repository"
                : "Create a reusable agent"
          }
          description={
            modal === "agent"
              ? "Agent Studio owns versioning, model bindings and independent release review."
              : "Choose a starting point for the local review workspace."
          }
          onClose={() => setModal(null)}
        >
          {modal === "agent" ? (
            <>
              <Notice>
                Agent authoring and release integration are scheduled for Phase
                2–3. No agent is created by this preview.
              </Notice>
              <div className="dependency-summary">
                <ShieldCheck size={19} />
                <p>
                  Role & instructions · input/output schema · model · skills &
                  tools · knowledge scope · evaluation
                </p>
              </div>
              <Button
                onClick={() => {
                  setModal(null);
                  onBuild();
                }}
              >
                Inspect workflow agent bindings
              </Button>
            </>
          ) : (
            <>
              <Notice>
                {modal === "manual"
                  ? "Create a blank server draft with a verified workspace session, or continue exploring the local example offline."
                  : "Browse templates returned by the authorized Flowcase registry. Template dependencies are validated by the server; local examples remain available offline."}
              </Notice>
              <Link
                className="e-button primary"
                to={
                  modal === "manual"
                    ? "/connected?create=blank"
                    : "/connected?create=template"
                }
              >
                {" "}
                {modal === "manual"
                  ? "Create a blank server draft"
                  : "Browse server templates"}
                <ArrowRight size={15} />
              </Link>
              <div className="template-preview">
                <GitBranch size={24} />
                <div>
                  <h3>Vendor Payment Automation</h3>
                  <p>
                    Parallel verification → vendor match → exception review →
                    independent Finance approval.
                  </p>
                </div>
                <Badge>Design example</Badge>
              </div>
              <Button
                variant="primary"
                onClick={() => {
                  setModal(null);
                  onBuild();
                }}
              >
                Open editable example <ArrowRight size={15} />
              </Button>
            </>
          )}
        </DialogPanel>
      )}
    </section>
  );
}

export function ArchitectBrief({
  brief,
  onBrief,
  onBuild,
}: {
  brief: string;
  onBrief: (value: string) => void;
  onBuild: () => void;
}) {
  return (
    <section className="page architect-page">
      <header className="page-heading">
        <div>
          <span className="eyebrow">AI-ASSISTED SOLUTION ENGINEERING</span>
          <h1>AI Solution Architect</h1>
          <p>
            From an unstructured requirement to a solution your team can
            actually build.
          </p>
        </div>
        <Button onClick={onBuild}>
          Open Workflow Studio <ArrowRight size={16} />
        </Button>
      </header>
      <div className="architect-columns">
        <section className="surface architect-requirement">
          <h2>Define the business outcome</h2>
          <label className="e-field">
            Business requirement
            <textarea
              aria-label="Business requirement"
              rows={9}
              value={brief}
              onChange={(e) => onBrief(e.target.value)}
              placeholder="Describe the process, its inputs, decisions and intended outcome…"
            />
          </label>
          <h3>Clarify before generating</h3>
          <ul>
            <li>Which event starts the process?</li>
            <li>Who owns review and final approval?</li>
            <li>Which systems and knowledge are permitted?</li>
            <li>What baseline cost, volume and quality define success?</li>
          </ul>
        </section>
        <section className="surface architect-blueprint">
          <Badge tone="amber">Model not configured</Badge>
          <h2>Your solution blueprint</h2>
          <p>
            The blueprint will bring together requirements, workflow topology,
            minimal agents, knowledge, integrations, human controls and
            economics assumptions.
          </p>
          <Notice>
            No AI output has been generated. An approved model and authorized
            draft API must be connected before proposing or applying changes.
          </Notice>
          <Button disabled>Generate blueprint</Button>
          <small>Requires a configured model connection</small>
          <Button onClick={onBuild}>Explore the Vendor Payment example</Button>
        </section>
      </div>
    </section>
  );
}
