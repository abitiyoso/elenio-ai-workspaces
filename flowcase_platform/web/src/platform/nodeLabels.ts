import type { NodeType } from "../api/workflows";
export const labels: Record<NodeType, string> = {
  start: "Trigger",
  end: "Complete",
  agent_task: "Agent",
  service_task: "Service",
  human_task: "Human task",
  human_approval: "Approval",
  condition: "Condition",
  parallel_split: "Parallel split",
  parallel_join: "Join · all",
  timer: "Timer",
  subworkflow: "Unavailable",
};
