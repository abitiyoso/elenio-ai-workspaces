import { BrowserRouter, Route, Routes } from "react-router-dom";
import { SessionProvider } from "./session/SessionProvider";
import { Shell } from "./components/Shell";
import { ToastProvider } from "./components/ui";
import { HomePage } from "./pages/Home";
import { CatalogPage } from "./pages/Catalog";
import { PeoplePage } from "./pages/People";
import { WorkspacesPage } from "./pages/Workspaces";
import { AccessPage } from "./pages/Access";
import { RegistryPage } from "./pages/Registry";
import { IntegrationsPage } from "./pages/Integrations";
import { AuditPage } from "./pages/Audit";
import { SignedOutPage } from "./pages/SignedOut";
import { NotFoundPage } from "./pages/NotPermitted";

function Authenticated() {
  return (
    <SessionProvider>
      <Routes>
        <Route element={<Shell />}>
          <Route index element={<HomePage />} />
          <Route path="catalog" element={<CatalogPage />} />
          <Route path="people" element={<PeoplePage />} />
          <Route path="workspaces" element={<WorkspacesPage />} />
          <Route path="access" element={<AccessPage />} />
          <Route path="registry" element={<RegistryPage />} />
          <Route path="integrations" element={<IntegrationsPage />} />
          <Route path="audit" element={<AuditPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Routes>
    </SessionProvider>
  );
}

export function App() {
  return (
    <BrowserRouter>
      <ToastProvider>
        <Routes>
          <Route path="/signed-out" element={<SignedOutPage />} />
          <Route path="/*" element={<Authenticated />} />
        </Routes>
      </ToastProvider>
    </BrowserRouter>
  );
}
