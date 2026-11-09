import type { VersionStatus } from "../api/workflows";

const cls: Record<VersionStatus, string> = {
  published: "pill published",
  draft: "pill draft",
  validated: "pill active",
  deprecated: "pill",
};

export function StatusPill({ status, version }: { status: VersionStatus; version?: number }) {
  const label = status[0]?.toUpperCase() + status.slice(1);
  return <span className={cls[status]}>{version ? `${label} v${version}` : label}</span>;
}
