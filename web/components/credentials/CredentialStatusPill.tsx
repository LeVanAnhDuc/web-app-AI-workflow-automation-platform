"use client";

import clsx from "clsx";
import type { CredentialStatus, StatusTone } from "./status";

/* The visual half of the status rule. Kept apart from `status.ts` so that file
   stays pure and testable without a DOM. */

const toneClass: Record<StatusTone, { bg: string; dot: string; text: string }> = {
  success: { bg: "bg-success/13", dot: "bg-success", text: "text-success" },
  warning: { bg: "bg-warning/13", dot: "bg-warning", text: "text-warning" },
  danger: { bg: "bg-danger/15", dot: "bg-danger", text: "text-danger" },
  neutral: { bg: "bg-white/6", dot: "bg-ink-5", text: "text-ink-3" },
};

export function CredentialStatusPill({
  status,
  className,
}: {
  status: CredentialStatus;
  className?: string;
}) {
  const tone = toneClass[status.tone];
  return (
    <span
      // The detail is the tooltip as well as the subline: in the node drawer
      // there is no room for a second line.
      title={status.detail}
      className={clsx(
        "inline-flex w-fit items-center gap-[7px] rounded-full px-2.5 py-1",
        tone.bg,
        className,
      )}
    >
      <span className={clsx("h-1.5 w-1.5 shrink-0 rounded-full", tone.dot)} />
      <span className={clsx("text-[11.5px] font-bold", tone.text)}>{status.label}</span>
    </span>
  );
}

/** The status as one line: pill plus its explanation, for the node drawer. */
export function CredentialStatusLine({ status }: { status: CredentialStatus }) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <CredentialStatusPill status={status} />
      {status.detail && (
        <span className="truncate text-[11px] text-ink-4">{status.detail}</span>
      )}
    </div>
  );
}
