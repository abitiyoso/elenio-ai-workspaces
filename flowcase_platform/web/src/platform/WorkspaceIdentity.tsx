import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import { controlPlaneGet } from "./controlPlaneApps";

export function WorkspaceIdentity() {
  const [name, setName] = useState("Workspace");
  const [user, setUser] = useState("Loading session…");
  useEffect(() => {
    const controller = new AbortController();
    controlPlaneGet<{ workspace: { name: string } | null; user: { display_name: string } }>(
      "/api/v1/me", controller.signal,
    ).then(data => {
      if (controller.signal.aborted) return;
      setName(typeof data.workspace?.name === "string" ? data.workspace.name : "Choose workspace");
      setUser(typeof data.user?.display_name === "string" ? data.user.display_name : "Workspace user");
    }, () => {
      if (!controller.signal.aborted) {
        setName("Workspace sign-in");
        setUser("Session unavailable");
      }
    });
    return () => controller.abort();
  }, []);
  // Leave the editor before switching. The existing CP UI owns the session mutation,
  // CSRF check and membership selection; existing unload guards protect dirty drafts.
  return (
    <a className="workspace-switch" href="/" aria-label={`Workspace: ${name}. Open Control Plane to change workspace`}>
      <span className="workspace-symbol">E</span>
      <span><strong>{name}</strong><small>{user}</small></span>
      <ChevronRight size={15} />
    </a>
  );
}
