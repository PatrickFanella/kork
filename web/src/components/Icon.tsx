// Minimal stroke icon set (24px grid, 1.75 stroke). Decorative by default.

const PATHS = {
  check: "M5 12.5l4.5 4.5L19 7.5",
  copy: "M9 9h10v10H9zM5 15V5h10",
  image: "M4 5h16v14H4zM4 16l5-5 4 4 3-3 4 4M15.5 9.5h.01",
  upload: "M12 16V4M7 9l5-5 5 5M5 20h14",
  trash: "M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3",
  ban: "M12 3a9 9 0 100 18 9 9 0 000-18zM5.6 5.6l12.8 12.8",
  plus: "M12 5v14M5 12h14",
  up: "M12 19V5M6 11l6-6 6 6",
  down: "M12 5v14M6 13l6 6 6-6",
  edit: "M4 20h4L19 9l-4-4L4 16zM13.5 6.5l4 4",
  eye: "M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12zM12 9a3 3 0 100 6 3 3 0 000-6z",
  external: "M14 4h6v6M20 4l-9 9M18 14v6H4V6h6",
  search: "M11 4a7 7 0 100 14 7 7 0 000-14zM20 20l-4-4",
  link: "M10 14a4 4 0 005.7 0l3-3a4 4 0 00-5.7-5.7l-1 1M14 10a4 4 0 00-5.7 0l-3 3a4 4 0 005.7 5.7l1-1",
  close: "M6 6l12 12M18 6L6 18",
  back: "M15 5l-7 7 7 7",
  logout: "M15 4h4v16h-4M10 8l-4 4 4 4M6 12h11",
  globe: "M12 3a9 9 0 100 18 9 9 0 000-18zM3 12h18M12 3c3 3.5 3 14.5 0 18M12 3c-3 3.5-3 14.5 0 18",
  chart: "M4 20V4M4 20h16M8 16v-5M12 16V8M16 16v-3",
  share: "M12 3v12M8 7l4-4 4 4M5 13v7h14v-7",
  palette: "M12 3a9 9 0 000 18c1.5 0 2-1 2-2s-1-1.5-1-2.5 1-1.5 2-1.5h2a4 4 0 004-4c0-4.5-4-8-9-8zM7.5 11h.01M10 7h.01M15 7.5h.01",
  user: "M12 4a4 4 0 100 8 4 4 0 000-8zM4 20c1.5-4 4.5-6 8-6s6.5 2 8 6",
  blocks: "M4 5h16v4H4zM4 11h16v4H4zM4 17h10v2H4z",
  heading: "M6 4v16M18 4v16M6 12h12",
  text: "M4 6h16M4 11h16M4 16h10",
  divider: "M3 12h18",
  qr: "M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h2v2h-2zM18 14h2M14 18h2v2M18 18h2v2h-2",
  map: "M12 21s-7-6-7-11a7 7 0 0114 0c0 5-7 11-7 11zM12 7.5a2.5 2.5 0 100 5 2.5 2.5 0 000-5z",
  contact: "M4 5h16v14H4zM9 9.5a2 2 0 100 4 2 2 0 000-4zM6 16.5c.7-1.5 1.8-2.2 3-2.2s2.3.7 3 2.2M14 10h4M14 13h3",
  play: "M4 5h16v14H4zM10 9l5 3-5 3z",
  at: "M16 12a4 4 0 11-8 0 4 4 0 018 0zM16 12v1.5a2.5 2.5 0 005 0V12a9 9 0 10-3.5 7.1",
  shield: "M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z",
  refresh: "M20 11a8 8 0 10-2.3 5.7M20 4v7h-7",
  menu: "M4 7h16M4 12h16M4 17h16",
  sun: "M12 8a4 4 0 100 8 4 4 0 000-8z",
} as const;

export type IconName = keyof typeof PATHS;

export function Icon({ name, size = 18, title }: { name: IconName; size?: number; title?: string }) {
  return (
    <svg
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden={title ? undefined : true}
      role={title ? "img" : undefined}
    >
      {title && <title>{title}</title>}
      <path d={PATHS[name]} />
    </svg>
  );
}

export const BLOCK_ICONS: Record<string, IconName> = {
  service: "at",
  link: "link",
  heading: "heading",
  text: "text",
  divider: "divider",
  image: "image",
  embed: "play",
  qr: "qr",
  copy: "copy",
  contact: "contact",
  map: "map",
};
