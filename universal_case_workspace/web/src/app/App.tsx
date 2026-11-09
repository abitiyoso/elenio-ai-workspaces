import { Navigate, Route, Routes } from "react-router-dom";
import { ToastProvider } from "../components/Toast";
import { MyCasesPage } from "../features/cases/MyCases";
import { NewCasePage } from "../features/intake/NewCase";
import { PackDetailPage, PackLibraryPage } from "../features/packs/PackLibrary";
import { CaseWorkspacePage } from "../features/workspace/CaseWorkspace";
import { CaseNavProvider } from "./caseNav";
import { SessionProvider } from "./session";
import { Shell } from "./Shell";

export function App() {
  return (
    <ToastProvider>
      <SessionProvider>
        <CaseNavProvider>
          <Shell>
            <Routes>
              <Route path="/" element={<MyCasesPage />} />
              <Route path="/cases" element={<Navigate to="/" replace />} />
              <Route path="/cases/new" element={<NewCasePage />} />
              <Route path="/cases/:id" element={<CaseWorkspacePage />} />
              <Route path="/cases/:id/:navId" element={<CaseWorkspacePage />} />
              <Route path="/packs" element={<PackLibraryPage />} />
              <Route path="/packs/:key/:version" element={<PackDetailPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Shell>
        </CaseNavProvider>
      </SessionProvider>
    </ToastProvider>
  );
}
