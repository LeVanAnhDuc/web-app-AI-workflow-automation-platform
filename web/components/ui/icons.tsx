import type React from "react";

/* One stroke-based icon per node type, on a 24px grid. Keys match the Icon
   field of the Go descriptors so a new node picks up an icon by naming it. */

type IconProps = { size?: number; className?: string };

const base = (size: number): React.SVGProps<SVGSVGElement> => ({
  width: size,
  height: size,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
});

export const Icons = {
  logo: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2} className={className}>
      <rect x="3" y="3" width="7" height="7" rx="2" />
      <rect x="14" y="14" width="7" height="7" rx="2" />
      <path d="M10 6.5h4a3.5 3.5 0 0 1 3.5 3.5v4" />
    </svg>
  ),
  bolt: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M13 2L5 13h6l-2 9 8-11h-6z" />
    </svg>
  ),
  webhook: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M12 3v6M12 21a4 4 0 0 0 4-4M7 12a5 5 0 0 1 10 0" />
      <circle cx="12" cy="16" r="2" />
    </svg>
  ),
  clock: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </svg>
  ),
  globe: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3c2.5 3 2.5 15 0 18M12 3c-2.5 3-2.5 15 0 18" />
    </svg>
  ),
  code: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M9 6l-5 6 5 6M15 6l5 6-5 6" />
    </svg>
  ),
  branch: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M4 6h6l4 6h6M14 12l-4 6H4M20 12l-3-3M20 12l-3 3" />
    </svg>
  ),
  table: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <rect x="4" y="5" width="16" height="14" rx="2" />
      <path d="M4 10h16M9 10v9" />
    </svg>
  ),
  merge: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M5 5v4a4 4 0 0 0 4 4h10M5 19v-4a4 4 0 0 1 4-4M16 9l3 4-3 4" />
    </svg>
  ),
  sparkle: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M12 3l2.2 5.3L20 10l-5.8 1.7L12 17l-2.2-5.3L4 10l5.8-1.7z" />
    </svg>
  ),
  robot: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <rect x="4" y="8" width="16" height="12" rx="3" />
      <path d="M12 4v4M9 14h.01M15 14h.01" />
    </svg>
  ),
  search: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2} className={className}>
      <circle cx="11" cy="11" r="7" />
      <path d="M20 20l-3.5-3.5" />
    </svg>
  ),
  plus: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2.4} className={className}>
      <path d="M12 5v14M5 12h14" />
    </svg>
  ),
  close: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2} className={className}>
      <path d="M6 6l12 12M18 6L6 18" />
    </svg>
  ),
  play: ({ size = 12, className }: IconProps) => (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className}>
      <path d="M6 3l14 9-14 9z" />
    </svg>
  ),
  stop: ({ size = 12, className }: IconProps) => (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className}>
      <rect x="5" y="5" width="14" height="14" rx="2" />
    </svg>
  ),
  retry: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2.2} className={className}>
      <path d="M20 12a8 8 0 1 1-2.4-5.7" />
      <path d="M20 4v5h-5" />
    </svg>
  ),
  expand: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M4 14h6v6M20 10h-6V4M14 10l6-6M10 14l-6 6" />
    </svg>
  ),
  chevronDown: ({ size = 12, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2} className={className}>
      <path d="M6 9l6 6 6-6" />
    </svg>
  ),
  chevronRight: ({ size = 12, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2} className={className}>
      <path d="M9 6l6 6-6 6" />
    </svg>
  ),
  chevronLeft: ({ size = 12, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2} className={className}>
      <path d="M15 6l-6 6 6 6" />
    </svg>
  ),
  chevronUp: ({ size = 12, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2} className={className}>
      <path d="M6 15l6-6 6 6" />
    </svg>
  ),
  dots: ({ size = 16, className }: IconProps) => (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className}>
      <circle cx="12" cy="5" r="1.6" />
      <circle cx="12" cy="12" r="1.6" />
      <circle cx="12" cy="19" r="1.6" />
    </svg>
  ),
  mail: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <path d="M3 7l9 6 9-6" />
    </svg>
  ),
  lock: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <rect x="4" y="10" width="16" height="10" rx="2" />
      <path d="M8 10V7a4 4 0 0 1 8 0v3" />
    </svg>
  ),
  eye: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M2 12s3.8-6 10-6 10 6 10 6-3.8 6-10 6-10-6-10-6z" />
      <circle cx="12" cy="12" r="2.6" />
    </svg>
  ),
  calendar: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <rect x="3" y="5" width="18" height="16" rx="2" />
      <path d="M3 10h18M8 3v4M16 3v4" />
    </svg>
  ),
  info: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5M12 8v.01" />
    </svg>
  ),
  alert: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} strokeWidth={2.2} className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 8v5M12 16.5v.01" />
    </svg>
  ),
  trash: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13" />
    </svg>
  ),
  copy: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M15 9V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3" />
    </svg>
  ),
  respond: ({ size = 16, className }: IconProps) => (
    <svg {...base(size)} className={className}>
      <path d="M4 12h11M15 7l5 5-5 5M4 6v12" />
    </svg>
  ),
} satisfies Record<string, (p: IconProps) => React.ReactElement>;

export type IconName = keyof typeof Icons;

/** Resolves a descriptor's Icon field, falling back to a neutral glyph. */
export function NodeIcon({
  name,
  size = 16,
  className,
}: {
  name: string;
  size?: number;
  className?: string;
}) {
  const Cmp = (Icons as Record<string, ((p: IconProps) => React.ReactElement) | undefined>)[name];
  if (!Cmp) return <Icons.table size={size} className={className} />;
  return <Cmp size={size} className={className} />;
}
