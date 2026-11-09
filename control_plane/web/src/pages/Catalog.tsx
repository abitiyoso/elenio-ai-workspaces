import { useId, useState } from "react";
import { Link } from "react-router-dom";
import { useSession } from "../session/context";
import { useApi } from "../lib/useApi";
import { api } from "../api/endpoints";
import type { AppSummary, RegistryApp } from "../api/types";
import { AppCard } from "../components/AppCard";
import { useLauncher } from "../components/Launcher";
import { AppIcon, Icon } from "../components/Icons";
import { Async, EmptyState, HealthPill, IntegrationPill, PageHead, Pill, Skeleton, StatusPill, Tabs } from "../components/ui";

const CATEGORIES = ["all", "platform", "experience", "business", "admin"] as const;
type Category = (typeof CATEGORIES)[number];

function matches(q: string, ...fields: (string | null | undefined)[]) {
  const needle = q.trim().toLowerCase();
  if (!needle) return true;
  return fields.some((f) => f?.toLowerCase().includes(needle));
}

export function CatalogPage() {
  const { me } = useSession();
  const canSeeRegistry = me.capabilities.manage_registry;
  const [tab, setTab] = useState<"available" | "all">("available");
  const [q, setQ] = useState("");
  const [cat, setCat] = useState<Category>("all");
  const searchId = useId();
  const catId = useId();
  const apps = useApi<{ items: AppSummary[] }>(api.apps);
  const registry = useApi<{ items: RegistryApp[] }>(canSeeRegistry && tab === "all" ? api.admin.apps : null);
  const { launch, busyKey, dialogs } = useLauncher();

  return (
    <>
      <PageHead eyebrow="WORKSPACE" title="App Catalog">
        Apps published and entitled to you in {me.workspace?.name ?? "your active workspace"}. Availability is decided by
        the server for every launch.
      </PageHead>

      <div className="toolbar">
        {canSeeRegistry ? (
          <Tabs
            label="Catalog view"
            value={tab}
            onChange={setTab}
            tabs={[
              { key: "available", label: "Available to me" },
              { key: "all", label: "All registered" },
            ]}
          />
        ) : (
          <span />
        )}
        <div className="toolbarright">
          <label className="search" htmlFor={searchId}>
            <Icon name="search" size="sm" />
            <span className="sr-only">Search apps</span>
            <input id={searchId} type="search" placeholder="Search apps" value={q} onChange={(e) => setQ(e.target.value)} />
          </label>
          {tab === "available" && (
            <>
              <label className="sr-only" htmlFor={catId}>
                Category
              </label>
              <select id={catId} className="select narrow" value={cat} onChange={(e) => setCat(e.target.value as Category)}>
                {CATEGORIES.map((c) => (
                  <option key={c} value={c}>
                    {c === "all" ? "All categories" : c}
                  </option>
                ))}
              </select>
            </>
          )}
        </div>
      </div>

      {tab === "available" ? (
        <Async state={apps} skeleton={<Skeleton lines={6} height={40} />} title="Could not load the catalog">
          {({ items }) => {
            const shown = items.filter(
              (a) => (cat === "all" || a.category === cat) && matches(q, a.display_name, a.description, a.app_key, a.owner_team),
            );
            if (items.length === 0)
              return (
                <EmptyState title="No apps available" icon="grid">
                  Nothing is published and entitled to you in this workspace.
                </EmptyState>
              );
            if (shown.length === 0) return <EmptyState title="No apps match your filters" icon="search" />;
            return (
              <div className="appgrid catalog">
                {shown.map((a) => (
                  <AppCard key={a.app_key} app={a} onLaunch={launch} busy={busyKey === a.app_key} />
                ))}
              </div>
            );
          }}
        </Async>
      ) : (
        <Async state={registry} skeleton={<Skeleton lines={6} height={40} />} title="Could not load the registry">
          {({ items }) => {
            const availableKeys = new Set(apps.data?.items.map((a) => a.app_key) ?? []);
            const shown = items.filter((a) => matches(q, a.display_name, a.app_key, a.owner_team));
            if (shown.length === 0) return <EmptyState title="No registered apps match" icon="box" />;
            return (
              <>
                <p className="notice">
                  Admin view of every registered app. Launching is only possible from <em>Available to me</em>; manage
                  lifecycle in the <Link to="/registry">Mini-App Registry</Link>.
                </p>
                <div className="appgrid catalog spaced">
                  {shown.map((a) => {
                    const available = availableKeys.has(a.app_key);
                    return (
                      <article key={a.app_key} className={`appcard${available ? "" : " locked"}`}>
                        <div className="appcardtop">
                          <AppIcon icon={a.versions.find((v) => v.version === a.latest_version)?.manifest.icon ?? "default"} />
                          <div className="appbadges">
                            <StatusPill value={a.status} />
                            <IntegrationPill integration={a.integration} />
                          </div>
                        </div>
                        <h3>{a.display_name}</h3>
                        <p>
                          <code>{a.route_base}</code>
                        </p>
                        <div className="appfooter">
                          <span>
                            {a.owner_team} · {a.published_version ? `v${a.published_version}` : "unpublished"}
                          </span>
                          {available ? (
                            <Pill tone="ok">available to you</Pill>
                          ) : (
                            <span className="lockedlabel">
                              <Icon name="lock" size="sm" /> not launchable
                            </span>
                          )}
                        </div>
                        <div className="appfoot2">
                          <HealthPill health={a.health} />
                          {a.disabled_reason && <span className="tiny muted">Disabled: {a.disabled_reason}</span>}
                        </div>
                      </article>
                    );
                  })}
                </div>
              </>
            );
          }}
        </Async>
      )}
      {dialogs}
    </>
  );
}
