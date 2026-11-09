import type { AppSummary } from "../api/types";
import { AppIcon, Icon } from "./Icons";
import { HealthPill, IntegrationPill } from "./ui";

const tones: Record<string, "alt" | "amber" | undefined> = { business: "alt", admin: "amber" };

export function AppCard({
  app,
  onLaunch,
  busy,
}: {
  app: AppSummary;
  onLaunch: (app: AppSummary) => void;
  busy: boolean;
}) {
  return (
    <article className="appcard">
      <div className="appcardtop">
        <AppIcon icon={app.icon} tone={tones[app.category]} />
        <div className="appbadges">
          <HealthPill health={app.health} />
          <IntegrationPill integration={app.integration} />
        </div>
      </div>
      <h3>{app.display_name}</h3>
      <p>{app.description}</p>
      <div className="appfooter">
        <span>
          {app.owner_team} · v{app.version}
        </span>
        <button
          type="button"
          className="btn sm soft"
          onClick={() => onLaunch(app)}
          disabled={busy}
          aria-label={`Launch ${app.display_name}`}
        >
          {busy ? "Checking…" : "Launch"} <Icon name="external" size="sm" />
        </button>
      </div>
    </article>
  );
}
