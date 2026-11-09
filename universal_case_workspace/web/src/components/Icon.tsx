// Inline stroke icons (lucide-style). Manifest icon names map onto this allowlist; unknown names get a neutral glyph.
const paths: Record<string, string> = {
  grid: "M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z",
  "layout-dashboard": "M4 4h7v9H4zM13 4h7v5h-7zM13 11h7v9h-7zM4 15h7v5H4z",
  briefcase: "M4 8h16v11H4zM9 8V5h6v3M4 13h16",
  "list-checks": "M4 6l1.5 1.5L8 5M4 12l1.5 1.5L8 11M4 18l1.5 1.5L8 17M11 6h9M11 12h9M11 18h9",
  network: "M10 3h4v4h-4zM4 17h4v4H4zM16 17h4v4h-4zM12 7v5M6 17v-3h12v3",
  "check-square": "M4 4h16v16H4zM8 12l3 3 5-6",
  "shield-check": "M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6zM8.5 12l2.5 2.5 4.5-5",
  files: "M8 3h8l4 4v12H8zM16 3v4h4M4 7v14h12",
  file: "M6 3h8l4 4v14H6zM14 3v4h4M9 13h6M9 17h6",
  bot: "M8 8h8a3 3 0 013 3v6a3 3 0 01-3 3H8a3 3 0 01-3-3v-6a3 3 0 013-3zM12 4v4M9.5 13v1M14.5 13v1M2 14h3M19 14h3",
  "clock-3": "M12 3a9 9 0 110 18 9 9 0 010-18zM12 7v5h4",
  history: "M3 12a9 9 0 103-6.7L3 8M3 3v5h5M12 7v5l3 2",
  "alert-circle": "M12 3a9 9 0 110 18 9 9 0 010-18zM12 8v5M12 16h.01",
  "file-check-2": "M6 3h8l4 4v14H6zM14 3v4h4M9 14l2 2 4-4",
  "flask-conical": "M10 3h4M10 3v6L5 19a1 1 0 001 2h12a1 1 0 001-2l-5-10V3M7.5 15h9",
  "chart-no-axes-column": "M6 20V10M12 20V4M18 20v-7",
  blocks: "M4 4h7v7H4zM13 13h7v7h-7zM4 13h7v7H4zM15 3l5 5-5 5-5-5z",
  plus: "M12 5v14M5 12h14",
  search: "M11 4a7 7 0 110 14 7 7 0 010-14zM20 20l-4-4",
  menu: "M4 6h16M4 12h16M4 18h16",
  x: "M6 6l12 12M18 6L6 18",
  "arrow-left": "M19 12H5M11 6l-6 6 6 6",
  "arrow-right": "M5 12h14M13 6l6 6-6 6",
  external: "M14 4h6v6M20 4l-9 9M18 14v6H4V6h6",
  refresh: "M20 11a8 8 0 00-14.9-3M4 4v4h4M4 13a8 8 0 0014.9 3M20 20v-4h-4",
  check: "M5 12l4.5 4.5L19 7",
  table: "M4 5h16v14H4zM4 10h16M4 15h16M10 5v14",
  cards: "M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z",
  lock: "M6 11h12v9H6zM8 11V8a4 4 0 018 0v3",
  send: "M4 12l16-8-6 16-3-7z",
  panel: "M4 4h16v16H4zM15 4v16",
  save: "M5 4h11l3 3v13H5zM8 4v5h7M8 20v-6h8v6",
  user: "M12 4a4 4 0 110 8 4 4 0 010-8zM4 20a8 8 0 0116 0",
  calendar: "M4 6h16v14H4zM4 10h16M8 3v4M16 3v4",
  dot: "M12 9a3 3 0 110 6 3 3 0 010-6z",
  paperclip: "M20 11.5l-8.2 8.2a5 5 0 01-7.1-7.1l8.6-8.6a3.4 3.4 0 014.8 4.8l-8.6 8.6a1.7 1.7 0 01-2.4-2.4L15 7",
};

export type IconSize = "sm" | "md" | "lg";
const px: Record<IconSize, number> = { sm: 15, md: 18, lg: 23 };

export function Icon({ name, size = "md", className }: { name: string; size?: IconSize; className?: string }) {
  const d = paths[name] ?? paths.dot;
  return (
    <svg className={`svgicon${className ? ` ${className}` : ""}`} width={px[size]} height={px[size]} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d={d} />
    </svg>
  );
}
