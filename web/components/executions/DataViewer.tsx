"use client";

import { useEffect, useState } from "react";
import clsx from "clsx";
import { JsonView, SectionLabel, Segmented } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { clockTime } from "@/lib/format";
import type { Graph, GraphNode, Item, NodeExecution } from "@/lib/types";
import type { TimelineRow } from "./timeline";

type View = "table" | "json";

const viewOptions: { value: View; label: string }[] = [
  { value: "table", label: "Table" },
  { value: "json", label: "JSON" },
];

/**
 * Input on the left, Output (or Error) on the right — two panes side by side
 * rather than tabs, so the shape going in and the shape coming out can be
 * compared without switching. That was a deliberate design decision.
 */
export function DataViewer({
  row,
  graph,
  completedBefore,
}: {
  row?: TimelineRow;
  graph: Graph;
  completedBefore: number;
}) {
  if (!row) {
    return (
      <div className="flex grow items-center justify-center text-[13px] text-ink-4">
        Select a node to inspect its data.
      </div>
    );
  }

  const { node, exec } = row;
  const input = exec?.input ?? [];
  const sources = sourceNames(graph, node.id);

  return (
    <div className="grid min-w-0 grow grid-cols-2">
      <ItemsPane
        title="Input"
        meta={
          input.length
            ? `${input.length} item${input.length === 1 ? "" : "s"}${
                sources.length ? ` from ${sources.join(", ")}` : ""
              }`
            : exec
              ? sources.length
                ? `no items from ${sources.join(", ")}`
                : "no input items"
              : "not executed"
        }
        items={input}
        resetKey={`${node.id}:input:${input.length}`}
        borderRight
      />

      {exec?.status === "failed" ? (
        <ErrorPane exec={exec} node={node} completedBefore={completedBefore} />
      ) : (
        <OutputPane exec={exec} />
      )}
    </div>
  );
}

function OutputPane({ exec }: { exec?: NodeExecution }) {
  const handles = Object.keys(exec?.output ?? {});
  const [handle, setHandle] = useState<string | undefined>(undefined);
  const active = handle && handles.includes(handle) ? handle : (handles.includes("main") ? "main" : handles[0]);
  const items = (active && exec?.output?.[active]) || [];

  const meta = !exec
    ? "not executed"
    : exec.status === "running"
      ? "still running"
      : items.length
        ? `${items.length} item${items.length === 1 ? "" : "s"}${active && active !== "main" ? ` on “${active}”` : ""}`
        : "no output produced";

  return (
    <ItemsPane
      title="Output"
      meta={meta}
      items={items}
      resetKey={`${exec?.id ?? "none"}:${active ?? "none"}:${items.length}`}
      extra={
        handles.length > 1 ? (
          <span className="flex items-center gap-1">
            {handles.map((h) => (
              <button
                key={h}
                type="button"
                onClick={() => setHandle(h)}
                aria-pressed={h === active}
                className={clsx(
                  "rounded-full px-2 py-0.5 font-mono text-[10.5px] focus-visible:outline-2 focus-visible:outline-accent",
                  h === active ? "bg-accent/18 text-accent-3" : "bg-white/6 text-ink-4 hover:text-ink-3",
                )}
              >
                {h}
              </button>
            ))}
          </span>
        ) : undefined
      }
    />
  );
}

function ItemsPane({
  title,
  meta,
  items,
  resetKey,
  extra,
  borderRight,
}: {
  title: string;
  meta: string;
  items: Item[];
  /** Identity of what is being paged; changing it rewinds to the first item. */
  resetKey: string;
  extra?: React.ReactNode;
  borderRight?: boolean;
}) {
  const [view, setView] = useState<View>("table");
  const [index, setIndex] = useState(0);

  // A different node — or a live update — can shrink the item list underneath
  // the pager, so the cursor is pulled back into range.
  useEffect(() => setIndex(0), [resetKey]);
  const safeIndex = Math.min(index, Math.max(0, items.length - 1));
  const item = items[safeIndex];

  return (
    <section
      className={clsx("flex min-w-0 flex-col overflow-hidden", borderRight && "border-r border-line")}
    >
      <PaneHeader title={title} meta={meta} extra={extra} view={view} onView={setView} />

      <div className="min-w-0 grow overflow-auto px-[18px] py-3">
        {!item ? (
          <p className="py-6 text-center text-xs text-ink-5">Nothing to show.</p>
        ) : view === "table" ? (
          <FieldTable item={item} />
        ) : (
          <JsonView value={item.json} />
        )}

        {items.length > 1 && (
          <div className="flex items-center gap-2 pt-3">
            <PagerIcon
              label="Previous item"
              disabled={safeIndex === 0}
              onClick={() => setIndex(safeIndex - 1)}
            >
              <Icons.chevronLeft size={13} />
            </PagerIcon>
            <span className="font-mono text-[11px] text-ink-4">
              Item {safeIndex + 1} of {items.length}
            </span>
            <PagerIcon
              label="Next item"
              disabled={safeIndex >= items.length - 1}
              onClick={() => setIndex(safeIndex + 1)}
            >
              <Icons.chevronRight size={13} />
            </PagerIcon>
          </div>
        )}
      </div>
    </section>
  );
}

function ErrorPane({
  exec,
  node,
  completedBefore,
}: {
  exec: NodeExecution;
  node: GraphNode;
  completedBefore: number;
}) {
  const [view, setView] = useState<View>("json");
  const error = exec.error ?? { code: "unknown", message: "The node failed without a reason." };
  const attempts = Math.max(exec.attempt || 1, error.attempts ?? 1);
  const errorFields: Record<string, unknown> = { ...error };

  return (
    <section className="flex min-w-0 flex-col overflow-hidden">
      <PaneHeader
        title="Error"
        tone="danger"
        meta="no output produced"
        view={view}
        onView={setView}
      />

      <div className="flex min-w-0 grow flex-col gap-3.5 overflow-auto px-[18px] py-3.5">
        {view === "json" ? (
          <JsonView value={error} className="border-danger/22!" />
        ) : (
          <FieldTable item={{ json: errorFields }} />
        )}

        <div className="flex flex-col gap-[9px]">
          <SectionLabel>RETRY ATTEMPTS</SectionLabel>
          {attemptRows(exec, node, attempts).map((attempt) => (
            <div key={attempt.n} className="flex items-center gap-[11px]">
              <span className="w-[64px] shrink-0 font-mono text-[11px] text-ink-4">
                {attempt.at}
              </span>
              <span className="grow text-[11.5px] text-ink-3">{attempt.label}</span>
              <span className="rounded-[5px] bg-danger/12 px-2 py-0.5 font-mono text-[11px] text-danger-3">
                {error.status ?? error.code}
              </span>
            </div>
          ))}
          <p className="text-[11px] text-ink-5">
            The API records only the final attempt, so the earlier rows are spaced by this node’s
            configured wait of {Math.round(node.settings.waitBetweenTriesMs / 1000)}s and share its
            error.
          </p>
        </div>

        <div className="flex items-start gap-2.5 rounded-[10px] border border-accent/22 bg-accent/9 px-3.5 py-3">
          <Icons.info size={15} className="mt-px shrink-0 text-accent-2" />
          <span className="text-xs leading-[1.55] text-ink-2">
            Retrying resumes at this node with its stored input.
            {completedBefore > 0
              ? ` The ${completedBefore} completed node${
                  completedBefore === 1 ? "" : "s"
                } above will not run again.`
              : ""}
          </span>
        </div>
      </div>
    </section>
  );
}

function PaneHeader({
  title,
  meta,
  extra,
  tone = "default",
  view,
  onView,
}: {
  title: string;
  meta: string;
  extra?: React.ReactNode;
  tone?: "default" | "danger";
  view: View;
  onView: (next: View) => void;
}) {
  return (
    <div className="flex shrink-0 items-center gap-[11px] border-b border-line px-[18px] py-3">
      <h2
        className={clsx(
          "text-[12.5px] font-bold",
          tone === "danger" ? "text-danger" : "text-ink",
        )}
      >
        {title}
      </h2>
      <span className="truncate text-[11.5px] text-ink-4">{meta}</span>
      {extra}
      <div className="grow" />
      <Segmented value={view} options={viewOptions} onChange={onView} size="sm" />
    </div>
  );
}

function FieldTable({ item }: { item: Item }) {
  const entries = Object.entries(item.json);
  if (entries.length === 0) {
    return <p className="py-6 text-center text-xs text-ink-5">This item has no fields.</p>;
  }

  return (
    <div className="flex min-w-0 flex-col">
      <div className="grid grid-cols-[140px_1fr] gap-3 border-b border-white/5 pb-2.5 pt-1.5 text-[10.5px] font-bold tracking-[0.06em] text-ink-5">
        <span>FIELD</span>
        <span>VALUE</span>
      </div>
      {entries.map(([key, value], i) => (
        <div
          key={key}
          className={clsx(
            "grid min-w-0 grid-cols-[140px_1fr] gap-3 py-2.5",
            i < entries.length - 1 && "border-b border-white/4",
          )}
        >
          <span className="break-words font-mono text-[11.5px] text-json-key">{key}</span>
          <span
            className={clsx("break-words font-mono text-[11.5px] leading-[1.6]", valueTone(value))}
          >
            {formatValue(value)}
          </span>
        </div>
      ))}
    </div>
  );
}

function PagerIcon({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string;
  disabled: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className={clsx(
        "flex h-6 w-6 items-center justify-center rounded-md border border-line text-ink-3",
        "focus-visible:outline-2 focus-visible:outline-accent",
        disabled ? "cursor-not-allowed opacity-40" : "hover:bg-white/6 hover:text-ink",
      )}
    >
      {children}
    </button>
  );
}

function valueTone(value: unknown): string {
  if (typeof value === "string") return "text-json-string";
  if (typeof value === "number" || typeof value === "boolean") return "text-json-number";
  if (value === null || value === undefined) return "text-json-punct";
  return "text-ink-2";
}

function formatValue(value: unknown): string {
  if (value === undefined) return "undefined";
  return JSON.stringify(value) ?? String(value);
}

/** Which nodes fed this one, by name, for the Input pane's source line. */
function sourceNames(graph: Graph, nodeId: string): string[] {
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));
  const names = graph.edges
    .filter((e) => e.target === nodeId)
    .map((e) => byId.get(e.source)?.name)
    .filter((name): name is string => Boolean(name));
  return [...new Set(names)];
}

/**
 * A NodeExecution carries only the final attempt count and the final error, so
 * the rows are derived from those two: constant spacing by the node's
 * configured wait (the engine sleeps a fixed `waitBetweenTriesMs`, it does not
 * back off exponentially) and no invented per-attempt timestamps beyond that.
 */
function attemptRows(
  exec: NodeExecution,
  node: GraphNode,
  attempts: number,
): { n: number; at: string; label: string }[] {
  const base = exec.startedAt ? new Date(exec.startedAt).getTime() : undefined;
  const waitMs = node.settings.waitBetweenTriesMs;
  const waitLabel = `${Math.round(waitMs / 1000)}s`;

  return Array.from({ length: attempts }, (_, i) => ({
    n: i + 1,
    at:
      base === undefined || Number.isNaN(base)
        ? "—"
        : clockTime(new Date(base + i * waitMs).toISOString()),
    label: i === 0 ? "Attempt 1" : `Attempt ${i + 1} · waited ${waitLabel}`,
  }));
}
