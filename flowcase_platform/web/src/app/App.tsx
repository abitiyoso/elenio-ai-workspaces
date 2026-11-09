import { Link, Route, Routes } from "react-router-dom";
import { EmptyState, PageHead } from "../components/States";
import { SessionProvider } from "../features/session/SessionContext";
import { ToastProvider } from "../components/Toast";
import { AuditPage } from "../pages/Audit";
import { DesignerPage, DesignerPickerPage } from "../pages/Designer";
import { IntegrationsPage } from "../pages/Integrations";
import { OverviewPage } from "../pages/Overview";
import { ApprovalsPage } from "../pages/Approvals";
import { CaseDetailPage } from "../pages/CaseDetail";
import { CasesPage } from "../pages/Cases";
import { TasksPage } from "../pages/Tasks";
import { WorkflowDetailPage } from "../pages/WorkflowDetail";
import { WorkflowLibraryPage } from "../pages/WorkflowLibrary";
import { Shell } from "./Shell";

function NotFound() {
  return (
    <>
      <PageHead title="Page not found" subtitle="This address does not match any Flowcase screen." />
      <div className="panel panel-pad">
        <EmptyState icon="file" title="Nothing here">Return to the <Link to="/">overview</Link>.</EmptyState>
      </div>
    </>
  );
}

export function App() {
  return (
    <SessionProvider>
      <ToastProvider>
      <Shell>
        <Routes>
          <Route path="/" element={<OverviewPage />} />
          <Route path="/workflows" element={<WorkflowLibraryPage />} />
          <Route path="/workflows/:key" element={<WorkflowDetailPage />} />
          <Route path="/designer" element={<DesignerPickerPage />} />
          <Route path="/designer/:key/:version" element={<DesignerPage />} />
          <Route path="/cases" element={<CasesPage />} />
          <Route path="/cases/:id" element={<CaseDetailPage />} />
          <Route path="/tasks" element={<TasksPage />} />
          <Route path="/approvals" element={<ApprovalsPage />} />
          <Route path="/integrations" element={<IntegrationsPage />} />
          <Route path="/audit" element={<AuditPage />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </Shell>
      </ToastProvider>
    </SessionProvider>
  );
}
