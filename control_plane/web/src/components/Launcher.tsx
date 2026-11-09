import { useState } from "react";
import { openLaunchRoute, requestLaunch } from "../api/launch";
import { toApiError, type ApiRequestError } from "../api/client";
import type { AppSummary, LaunchResult } from "../api/types";
import { ErrorState, Modal } from "./ui";
import { Icon } from "./Icons";

type Pending = { app: Pick<AppSummary, "app_key" | "display_name">; result: LaunchResult };

/**
 * Launch flow: ask the server (GET /apps/{key}/launch), validate route_base, then navigate.
 * For integration "pending" apps, explain first and let the user open the route anyway
 * (the server renders an honest "integration pending" page).
 */
export function useLauncher() {
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [failure, setFailure] = useState<{ name: string; error: ApiRequestError } | null>(null);

  const launch = async (app: Pick<AppSummary, "app_key" | "display_name">) => {
    setBusyKey(app.app_key);
    try {
      const result = await requestLaunch(app.app_key);
      if (result.integration === "pending") {
        setPending({ app, result });
      } else {
        openLaunchRoute(result);
      }
    } catch (err) {
      setFailure({ name: app.display_name, error: toApiError(err) });
    } finally {
      setBusyKey(null);
    }
  };

  const dialogs = (
    <>
      <Modal
        open={pending !== null}
        title={`${pending?.app.display_name ?? ""}: integration pending`}
        onClose={() => setPending(null)}
        footer={
          <>
            <button type="button" className="btn" onClick={() => setPending(null)}>
              Close
            </button>
            <button
              type="button"
              className="btn primary"
              onClick={() => {
                if (pending) openLaunchRoute(pending.result);
              }}
            >
              <Icon name="external" size="sm" /> Open route anyway
            </button>
          </>
        }
      >
        <div className="notice warning">
          <strong>This service has not adopted the Control Plane identity contract yet.</strong> Your access to the route{" "}
          <code>{pending?.result.route_base}</code> is authorised, but the control plane does not forward requests to the
          service until it accepts audience-bound tokens. Opening the route shows the server's integration-pending page; it
          does not open a working app.
        </div>
      </Modal>
      <Modal open={failure !== null} title={`Could not open ${failure?.name ?? "app"}`} onClose={() => setFailure(null)}>
        {failure && <ErrorState error={failure.error} title="Launch refused" />}
      </Modal>
    </>
  );

  return { launch, busyKey, dialogs };
}
