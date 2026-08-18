"use client";

import clsx from "clsx";
import { useState } from "react";
import { IconButton, JsonView, StatusDot, StatusPill } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { useEditorStore } from "@/lib/editorStore";
import { clockTime, duration } from "@/lib/format";
import type { NodeExecution } from "@/lib/types";

type Tab = "log" | "data";

/** Shared by the header row and every body row so the columns line up. */
const COLUMNS = "grid-cols-[22px_1.4fr_1fr_70px_70px_96px]";

/**
 * The bottom panel: one row per node execution, live from the stream. Clicking a
 * row selects the node so the log and the canvas stay one conversation rather
 * than two views of the same run.
 */
export function ExecutionLogPanel() {
  const execution = useEditorStore((s) => s.execution);
  const nodeExecutions = useEditorStore((s) => s.nodeExecutions);
  const selectedNodeId = useEditorStore((s) => s.selectedNodeId);
  const select = useEditorStore((s) => s.select);

  const [collapsed, setCollapsed] = useState(false);
  const [tab, setTab] = useState<Tab>("log");

  const selected = nodeExecutions.find((n) => n.nodeId === selectedNodeId);

  return (
    <section
      aria-label="Execution log"
      className="flex shrink-0 flex-col border-t border-line bg-panel"
      style={{ height: collapsed ? 38 : 208 }}
    >
      <div
        className={clsx(
          "flex shrink-0 items-center gap-4 px-4.5",
          collapsed ? "h-[38px]" : "h-[46px]",
        )}
      >
        <div role="tablist" aria-label="Bottom panel" className="flex gap-[5px]">
          {(
            [
              { id: "log" as Tab, label: "Execution log" },
              { id: "data" as Tab, label: "Data" },
            ]
          ).map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={tab === t.id}
              onClick={() => {
                setTab(t.id);
                setCollapsed(false);
              }}
              className={clsx(
                "rounded-lg px-3.5 py-1.5 text-[12.5px]",
                tab === t.id ? "bg-white/7 font-semibold text-ink" : "text-ink-4 hover:text-ink-2",
              )}
            >
              {t.label}
            </button>
          ))}
        </div>

        {execution ? (
          <>
            <StatusPill status={execution.status} />
            <span className="font-mono text-[11px] text-ink-5">
              {duration(execution.durationMs)} · {shortId(execution.id)}
            </span>
          </>
        ) : (
          <span className="text-[12px] text-ink-5">No run yet</span>
        )}

        <div className="grow" />
        <IconButton
          aria-label={collapsed ? "Expand the execution log" : "Collapse the execution log"}
          aria-expanded={!collapsed}
          onClick={() => setCollapsed((c) => !c)}
        >
          {collapsed ? <Icons.chevronDown size={15} /> : <Icons.chevronUp size={15} />}
        </IconButton>
      </div>

      {!collapsed && tab === "log" && (
        <div className="flex min-h-0 grow flex-col overflow-y-auto">
          <div
            className={clsx(
              "grid shrink-0 px-4.5 py-1.5 text-[10.5px] font-bold tracking-[0.06em] text-ink-5",
              COLUMNS,
            )}
          >
            <span />
            <span>NODE</span>
            <span>TYPE</span>
            <span>ITEMS</span>
            <span>TIME</span>
            <span>STARTED</span>
          </div>

          {nodeExecutions.length === 0 && (
            <p className="px-4.5 py-5 text-xs text-ink-4">
              Press Run to execute the workflow — each node appears here as it finishes.
            </p>
          )}

          {nodeExecutions.map((ne) => (
            <LogRow
              key={ne.nodeId}
              execution={ne}
              selected={ne.nodeId === selectedNodeId}
              onSelect={() => select(ne.nodeId)}
            />
          ))}
        </div>
      )}

      {!collapsed && tab === "data" && (
        <div className="min-h-0 grow overflow-y-auto px-4.5 pb-4">
          {selected ? (
            <div className="flex flex-col gap-2">
              <span className="font-mono text-[10.5px] text-ink-4">
                {selected.nodeName} · output
              </span>
              <JsonView value={outputItems(selected)} />
            </div>
          ) : (
            <p className="py-5 text-xs text-ink-4">
              Select a node to inspect the items it produced.
            </p>
          )}
        </div>
      )}
    </section>
  );
}

function LogRow({
  execution,
  selected,
  onSelect,
}: {
  execution: NodeExecution;
  selected: boolean;
  onSelect: () => void;
}) {
  const skipped = execution.status === "skipped";
  const items = execution.output?.main?.length;

  return (
    <button
      type="button"
      onClick={onSelect}
      className={clsx(
        "grid items-center border-l-2 px-4.5 py-2 text-left text-[12.5px]",
        COLUMNS,
        selected ? "border-accent bg-accent/9" : "border-transparent hover:bg-white/3",
        skipped && "text-ink-5",
      )}
    >
      <StatusDot status={execution.status} />
      <span className={clsx("truncate pr-2", selected && "font-semibold")}>
        {execution.nodeName}
      </span>
      <span className={clsx("truncate pr-2 font-mono text-[11px]", skipped ? "" : "text-ink-4")}>
        {execution.nodeType}
      </span>
      <span className={clsx("font-mono text-[11px]", skipped ? "" : "text-ink-2")}>
        {skipped || items === undefined ? "—" : items}
      </span>
      <span className={clsx("font-mono text-[11px]", skipped ? "" : "text-ink-2")}>
        {skipped ? "skipped" : duration(execution.durationMs)}
      </span>
      <span className={clsx("font-mono text-[11px]", skipped ? "" : "text-ink-5")}>
        {skipped ? "—" : clockTime(execution.startedAt)}
      </span>
    </button>
  );
}

function outputItems(execution: NodeExecution): unknown {
  if (execution.error) return execution.error;
  const outputs = execution.output;
  if (!outputs) return [];
  const handles = Object.keys(outputs);
  if (handles.length === 1) return (outputs[handles[0]] ?? []).map((i) => i.json);
  return Object.fromEntries(handles.map((h) => [h, (outputs[h] ?? []).map((i) => i.json)]));
}

/** Execution ids are uuids; the log only needs enough to quote in a bug report. */
function shortId(id: string): string {
  return `exec_${id.replace(/-/g, "").slice(0, 5)}`;
}
