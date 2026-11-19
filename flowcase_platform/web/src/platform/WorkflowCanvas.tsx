import { useEffect, useMemo, useState } from "react";
import {
  Background,
  Controls,
  Handle,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import {
  ArrowRight,
  CheckCheck,
  GitBranch,
  GitMerge,
  Layers,
  Play,
  ShieldCheck,
  Timer,
  Workflow,
  X,
} from "lucide-react";
import type { NodeType, WorkflowDefinition } from "../api/workflows";
import { Button } from "../../../../packages/ui/primitives";
import { connect, processEdges, type Positions } from "./graphModel";
import { labels } from "./nodeLabels";
import "@xyflow/react/dist/style.css";

const icons = {
  start: Play,
  end: CheckCheck,
  agent_task: Layers,
  service_task: Workflow,
  human_task: ShieldCheck,
  human_approval: ShieldCheck,
  condition: GitBranch,
  parallel_split: GitBranch,
  parallel_join: GitMerge,
  timer: Timer,
  subworkflow: Workflow,
};
type CanvasNode = Node<
  {
    name: string;
    kind: NodeType;
    issue: boolean;
    mode: "fixture" | "connected";
  },
  "process"
>;
function ProcessNode({ data, selected }: NodeProps<CanvasNode>) {
  const Icon = icons[data.kind];
  return (
    <div
      className={`process-node ${data.kind} ${selected ? "selected" : ""} ${data.issue ? "invalid" : ""}`}
    >
      {data.kind !== "start" && (
        <Handle type="target" position={Position.Left} id="in" />
      )}
      <div className="node-top">
        <span className={`node-glyph ${data.kind}`}>
          {data.kind === "agent_task" ? (
            <span
              className={`graph-agent-object ${data.name.toLowerCase().includes("exception") ? "exception" : "document"}`}
            />
          ) : (
            <Icon size={19} />
          )}
        </span>
        <span>{labels[data.kind]}</span>
        {data.issue && <b aria-label="Validation issue">!</b>}
      </div>
      <strong>{data.name}</strong>
      <div className="node-foot">
        {data.kind === "agent_task"
          ? "Versioned agent binding"
          : data.kind === "human_approval"
            ? "Independent decision"
            : data.kind === "condition"
              ? "Explicit true / false"
              : data.kind === "parallel_join"
                ? "Wait for both branches"
                : data.mode === "fixture"
                  ? "Design fixture"
                  : "Process step"}
      </div>
      {data.kind === "condition" ? (
        <>
          <Handle
            type="source"
            position={Position.Right}
            id="true"
            style={{ top: "32%" }}
          />
          <Handle
            type="source"
            position={Position.Right}
            id="false"
            style={{ top: "72%" }}
          />
        </>
      ) : (
        data.kind !== "end" && (
          <Handle type="source" position={Position.Right} id="out" />
        )
      )}
    </div>
  );
}
const nodeTypes = { process: ProcessNode };

interface Props {
  mode?: "fixture" | "connected";
  definition: WorkflowDefinition;
  positions: Positions;
  selected: string | null;
  invalidIds: string[];
  editable: boolean;
  onChange: (d: WorkflowDefinition) => void;
  onPositions: (p: Positions) => void;
  onSelect: (id: string | null) => void;
  onEdge: (id: string) => void;
  onLayout: () => void;
}
function Canvas(props: Props) {
  const {
    definition,
    positions,
    selected,
    invalidIds,
    editable,
    onChange,
    onPositions,
    onSelect,
    onEdge,
    onLayout,
    mode = "fixture",
  } = props;
  const flow = useReactFlow();
  const [drag, setDrag] = useState<Positions>({});
  const nodes: CanvasNode[] = useMemo(
    () =>
      definition.nodes.map((n, i) => ({
        id: n.id,
        type: "process",
        width: 238,
        height: 125,
        measured: { width: 238, height: 125 },
        position: drag[n.id] ?? positions[n.id] ?? { x: i * 320, y: 160 },
        selected: n.id === selected,
        data: {
          name: n.name,
          kind: n.type,
          issue: invalidIds.includes(n.id),
          mode,
        },
      })),
    [definition, positions, drag, selected, invalidIds, mode],
  );
  const edges = useMemo(
    () =>
      processEdges(definition).map((e) => ({
        ...e,
        sourceHandle:
          definition.nodes.find((n) => n.id === e.source)?.type === "condition"
            ? e.label === "false"
              ? "false"
              : "true"
            : "out",
        targetHandle: "in",
        type: "smoothstep",
        style: {
          stroke:
            e.label === "false"
              ? "#b18b59"
              : e.label === "true"
                ? "#4d9689"
                : "#97a9c4",
          strokeWidth: 1.6,
        },
        labelStyle: { fontSize: 13, fill: "#42566f" },
        labelBgStyle: { fill: "#f8fafc" },
        markerEnd: {
          type: "arrowclosed" as const,
          width: 15,
          height: 15,
          color: "#97a9c4",
        },
      })),
    [definition],
  );
  const ready = Object.keys(positions).length > 0;
  useEffect(() => {
    if (ready) {
      const timer = setTimeout(
        () =>
          void flow.fitView({
            padding: 0.13,
            minZoom: 0.85,
            maxZoom: 0.85,
            duration: 0,
          }),
        80,
      );
      return () => clearTimeout(timer);
    }
  }, [ready, flow]);
  useEffect(() => {
    if (selected) {
      void flow.fitView({
        nodes: [{ id: selected }],
        padding: 0.5,
        minZoom: 0.85,
        maxZoom: 1,
        duration: 0,
      });
    }
  }, [selected, flow]);
  return (
    <div className="workflow-canvas" aria-label="Workflow process canvas">
      <ReactFlow<CanvasNode>
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        minZoom={0.25}
        maxZoom={1.5}
        fitView
        nodesDraggable={editable}
        nodesConnectable={editable}
        deleteKeyCode={null}
        onNodeClick={(_, n) => onSelect(n.id)}
        onPaneClick={() => onSelect(null)}
        onEdgeClick={(_, e) => onEdge(e.id)}
        onConnect={(c) => {
          if (editable && c.source && c.target)
            onChange(
              connect(
                definition,
                c.source,
                c.target,
                c.sourceHandle ?? undefined,
              ),
            );
        }}
        onNodesChange={(changes) => {
          const updates: Positions = {};
          for (const c of changes)
            if (c.type === "position" && c.position) updates[c.id] = c.position;
          if (Object.keys(updates).length)
            setDrag((prev) => ({ ...prev, ...updates }));
        }}
        onNodeDragStop={(_, n) => {
          onPositions({ ...positions, [n.id]: n.position });
          setDrag({});
        }}
      >
        <Background color="#cfd8e6" gap={22} size={1} />
        <Controls showInteractive={false} />
        <MiniMap
          nodeColor={(n) => (n.id === selected ? "#2864ec" : "#d9e2f2")}
          pannable
          zoomable
        />
      </ReactFlow>
      <div className="canvas-caption">
        <span className="canvas-dot" />
        Process connections <ArrowRight size={13} /> No execution telemetry
      </div>
      <Button
        className="layout-button"
        onClick={() => {
          onLayout();
          setTimeout(
            () => void flow.fitView({ padding: 0.13, maxZoom: 1, duration: 0 }),
            300,
          );
        }}
      >
        <Layers size={15} /> Auto layout
      </Button>
    </div>
  );
}
export function WorkflowCanvas(props: Props) {
  return (
    <ReactFlowProvider>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}
export function CloseInspector({ onClick }: { onClick: () => void }) {
  return (
    <Button variant="quiet" aria-label="Close inspector" onClick={onClick}>
      <X size={17} />
    </Button>
  );
}
