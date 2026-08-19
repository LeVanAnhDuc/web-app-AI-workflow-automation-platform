"use client";

import clsx from "clsx";
import { Icons, NodeIcon } from "@/components/ui/icons";

/* The credential registry names icons the shared node icon set does not have
   yet ("slack", "google"). Falling through to NodeIcon would draw a spreadsheet
   glyph next to "Slack", so the provider marks live here — stroke-based on the
   same 24px grid as the rest of the set. */

type Mark = (p: { size?: number; className?: string }) => React.ReactElement;

const extra: Record<string, Mark> = {
  slack: ({ size = 16, className }) => (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      className={className}
    >
      <rect x="9.5" y="3" width="5" height="9" rx="2.5" />
      <rect x="9.5" y="12" width="5" height="9" rx="2.5" />
      <rect x="3" y="9.5" width="9" height="5" rx="2.5" />
      <rect x="12" y="9.5" width="9" height="5" rx="2.5" />
    </svg>
  ),
  google: ({ size = 16, className }) => (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
    >
      <path d="M20.5 12a8.5 8.5 0 1 0-2.6 6.1" />
      <path d="M12 12h8.5" />
    </svg>
  ),
};

export function TypeIcon({
  name,
  size = 16,
  className,
}: {
  name: string;
  size?: number;
  className?: string;
}) {
  const Mark = extra[name];
  if (Mark) return <Mark size={size} className={className} />;
  // Everything the node set already knows ("lock", "sparkle", …) draws from
  // there, so a credential and its node look alike.
  if (name in Icons) return <NodeIcon name={name} size={size} className={className} />;
  return <Icons.lock size={size} className={className} />;
}

/** The rounded chip the tables and the picker put the mark in. */
export function TypeIconChip({
  icon,
  size = 32,
  tone = "default",
  className,
}: {
  icon: string;
  size?: number;
  tone?: "default" | "accent";
  className?: string;
}) {
  return (
    <span
      style={{ width: size, height: size, borderRadius: Math.round(size / 3.4) }}
      className={clsx(
        "flex shrink-0 items-center justify-center",
        tone === "accent" ? "bg-accent/18 text-accent-3" : "bg-white/6 text-ink-2",
        className,
      )}
    >
      <TypeIcon name={icon} size={Math.round(size * 0.5)} />
    </span>
  );
}
