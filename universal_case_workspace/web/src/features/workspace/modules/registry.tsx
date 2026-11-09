import type { ComponentType } from "react";
import { AiActivityModule } from "./AiActivity";
import { ApprovalsModule } from "./Approvals";
import { ArtifactListModule } from "./ArtifactList";
import { DocumentsModule } from "./Documents";
import { OverviewModule } from "./Overview";
import { TasksModule } from "./Tasks";
import { TimelineModule } from "./Timeline";
import type { ModuleProps } from "./types";

// Allowlisted view kinds. A manifest can only pick from these; anything else renders the unsupported card.
export const modules: Record<string, ComponentType<ModuleProps>> = {
  overview: OverviewModule,
  artifact_list: ({ ctx, nav }) => <ArtifactListModule ctx={ctx} nav={nav} />,
  tasks: TasksModule,
  approvals: ApprovalsModule,
  documents: DocumentsModule,
  ai_activity: AiActivityModule,
  timeline: TimelineModule,
};

