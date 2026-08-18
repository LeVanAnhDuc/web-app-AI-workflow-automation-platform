"use client";

import clsx from "clsx";
import { percent } from "@/lib/format";

/** 7-day success rate: a 70px track plus the figure, green at 90% or better. */
export function SuccessBar({ rate }: { rate?: number | null }) {
  if (rate === undefined || rate === null) {
    return <span className="text-[12.5px] text-ink-5">—</span>;
  }

  const clamped = Math.max(0, Math.min(1, rate));

  return (
    <div className="flex items-center gap-2.5">
      <div
        className="h-[5px] w-[70px] overflow-hidden rounded-full bg-white/8"
        role="img"
        aria-label={`${percent(rate)} of runs succeeded in the last 7 days`}
      >
        <div
          className={clsx(
            "h-full rounded-full",
            clamped >= 0.9 ? "bg-success" : "bg-warning",
          )}
          style={{ width: `${clamped * 100}%` }}
        />
      </div>
      <span className="font-mono text-[11.5px] text-ink-2">{percent(rate)}</span>
    </div>
  );
}
