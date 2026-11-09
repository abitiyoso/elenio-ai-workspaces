// Hand-written inline SVG icons (no icon CDN). Stroke style matches the prototype.
import type { ReactElement } from "react";

const paths: Record<string, ReactElement> = {
  home: (
    <>
      <path d="M3 10.5 12 3l9 7.5" />
      <path d="M5 9.5V21h5v-6h4v6h5V9.5" />
    </>
  ),
  grid: (
    <>
      <rect x="3" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="3" width="7" height="7" rx="1.5" />
      <rect x="3" y="14" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
    </>
  ),
  users: (
    <>
      <circle cx="9" cy="8" r="3.5" />
      <path d="M2.5 20c.6-3.6 3.2-5.5 6.5-5.5s5.9 1.9 6.5 5.5" />
      <path d="M16 4.6a3.5 3.5 0 0 1 0 6.8M18 14.8c2 .7 3.2 2.4 3.5 5.2" />
    </>
  ),
  layers: (
    <>
      <path d="m12 3 9 5-9 5-9-5 9-5Z" />
      <path d="m3 13 9 5 9-5" />
    </>
  ),
  shield: (
    <>
      <path d="M12 3 4.5 6v5.5c0 4.6 3.1 8.2 7.5 9.5 4.4-1.3 7.5-4.9 7.5-9.5V6L12 3Z" />
      <path d="m9 12 2 2 4-4" />
    </>
  ),
  box: (
    <>
      <path d="m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3Z" />
      <path d="m4 7.5 8 4.5 8-4.5M12 12v9" />
    </>
  ),
  plug: (
    <>
      <path d="M9 3v5M15 3v5" />
      <path d="M6.5 8h11v3a5.5 5.5 0 0 1-11 0V8Z" />
      <path d="M12 16.5V21" />
    </>
  ),
  scroll: (
    <>
      <path d="M7 3h11a2 2 0 0 1 2 2v12" />
      <path d="M7 3a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-2H9" />
      <path d="M9 8h7M9 12h7" />
    </>
  ),
  menu: <path d="M4 6h16M4 12h16M4 18h16" />,
  "chevron-down": <path d="m6 9 6 6 6-6" />,
  "chevron-right": <path d="m9 6 6 6-6 6" />,
  search: (
    <>
      <circle cx="11" cy="11" r="6.5" />
      <path d="m20 20-4.2-4.2" />
    </>
  ),
  x: <path d="M6 6l12 12M18 6 6 18" />,
  logout: (
    <>
      <path d="M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3" />
      <path d="M10 16l-4-4 4-4M6 12h10" />
    </>
  ),
  external: (
    <>
      <path d="M14 4h6v6M20 4l-9 9" />
      <path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
    </>
  ),
  check: <path d="m5 12.5 4.5 4.5L19 7.5" />,
  alert: (
    <>
      <path d="M12 3.5 2.5 20h19L12 3.5Z" />
      <path d="M12 10v4.5M12 17.5v.01" />
    </>
  ),
  lock: (
    <>
      <rect x="5" y="10.5" width="14" height="10" rx="2" />
      <path d="M8 10.5V8a4 4 0 0 1 8 0v2.5" />
    </>
  ),
  refresh: (
    <>
      <path d="M20 11a8 8 0 0 0-14.6-4.5L4 8" />
      <path d="M4 4v4h4M4 13a8 8 0 0 0 14.6 4.5L20 16" />
      <path d="M20 20v-4h-4" />
    </>
  ),
  plus: <path d="M12 5v14M5 12h14" />,
  clock: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5V12l3 2" />
    </>
  ),
  key: (
    <>
      <circle cx="8" cy="15" r="4" />
      <path d="m11 12 8.5-8.5M16 7l2.5 2.5M14 9l2 2" />
    </>
  ),
  "book-open": (
    <>
      <path d="M3 5.5c3-1 6-.8 9 1v13c-3-1.8-6-2-9-1v-13Z" />
      <path d="M21 5.5c-3-1-6-.8-9 1v13c3-1.8 6-2 9-1v-13Z" />
    </>
  ),
  workflow: (
    <>
      <rect x="3" y="3.5" width="7" height="6" rx="1.5" />
      <rect x="14" y="14.5" width="7" height="6" rx="1.5" />
      <path d="M6.5 9.5v3.5a2 2 0 0 0 2 2H14" />
    </>
  ),
  bot: (
    <>
      <rect x="4" y="8" width="16" height="11" rx="3" />
      <path d="M12 8V4.5M9 13v1.5M15 13v1.5M2 13.5v2M22 13.5v2" />
      <circle cx="12" cy="3.5" r="1" />
    </>
  ),
  "panels-top-left": (
    <>
      <rect x="3" y="3.5" width="18" height="17" rx="2" />
      <path d="M3 9.5h18M9.5 9.5v11" />
    </>
  ),
  app: (
    <>
      <rect x="4" y="4" width="16" height="16" rx="4" />
      <path d="M9 12h6M12 9v6" />
    </>
  ),
  info: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 11v5M12 8v.01" />
    </>
  ),
  trash: (
    <>
      <path d="M4 7h16M10 11v6M14 11v6" />
      <path d="M6 7l1 13h10l1-13M9 7V4h6v3" />
    </>
  ),
};

export type IconName = keyof typeof paths;

export function Icon({ name, size, label }: { name: string; size?: "sm" | "lg"; label?: string }) {
  const body = paths[name] ?? paths.app;
  return (
    <svg
      className={`icon${size ? ` ${size}` : ""}`}
      viewBox="0 0 24 24"
      aria-hidden={label ? undefined : true}
      role={label ? "img" : undefined}
      aria-label={label}
      focusable="false"
    >
      {body}
    </svg>
  );
}

const APP_ICONS = new Set(["book-open", "workflow", "bot", "panels-top-left"]);

/** Map a manifest `icon` value to a known icon; anything else gets the default glyph. */
export function AppIcon({ icon, tone }: { icon: string; tone?: "alt" | "amber" }) {
  return (
    <div className={`appico${tone ? ` ${tone}` : ""}`} aria-hidden="true">
      <Icon name={APP_ICONS.has(icon) ? icon : "app"} size="lg" />
    </div>
  );
}
