"use client";

import clsx from "clsx";
import { SectionLabel, StatusDot } from "@/components/ui";
import { clockTime, duration } from "@/lib/format";
import type { TimelineRow } from "./timeline";

export function NodeTimeline({
  rows,
  selectedNodeId,
  onSelect,
}: {
  rows: TimelineRow[];
  selectedNodeId?: string;
  onSelect: (nodeId: string) => void;
}) {
  return (
    <div className="flex w-[250px] shrink-0 flex-col overflow-y-auto border-r border-line px-2.5 py-3.5">
      <div className="px-3 pb-2.5 pt-1">
        <SectionLabel>NODE TIMELINE</SectionLabel>
      </div>

      <ul className="flex flex-col">
        {rows.map(({ node, exec }) => {
          const failed = exec?.status === "failed";
          const selected = node.id === selectedNodeId;
          return (
            <li key={node.id}>
              <button
                type="button"
                onClick={() => onSelect(node.id)}
                aria-current={selected}
                className={clsx(
                  "flex w-full items-center gap-[11px] rounded-[9px] px-3 py-2.5 text-left",
                  "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent",
                  failed
                    ? "border border-danger/26 bg-danger/10"
                    : selected
                      ? "bg-white/7"
                      : "border border-transparent hover:bg-white/4",
                  failed && selected && "border-danger/50 bg-danger/14",
                )}
              >
                <StatusDot status={exec?.status ?? "skipped"} />

                <span className="flex min-w-0 grow flex-col gap-0.5">
                  <span
                    className={clsx(
                      "truncate text-[12.5px]",
                      failed ? "font-bold text-ink" : exec ? "text-ink-2" : "text-ink-4",
                    )}
                  >
                    {node.name}
                  </span>
                  <span
                    className={clsx(
                      "truncate font-mono text-[10px]",
                      failed ? "text-danger-2" : exec ? "text-ink-5" : "text-ink-6",
                    )}
                  >
                    {exec
                      ? `${clockTime(exec.startedAt)}${
                          exec.attempt > 1 ? ` · ${exec.attempt} attempts` : ""
                        }`
                      : "not executed"}
                  </span>
                </span>

                {exec?.durationMs !== undefined && (
                  <span
                    className={clsx(
                      "font-mono text-[10.5px]",
                      failed ? "text-danger-2" : "text-ink-4",
                    )}
                  >
                    {duration(exec.durationMs)}
                  </span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
