"use client";

import clsx from "clsx";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { TOOL_HANDLE, type FlowNode } from "@/lib/graph";
import { NodeIcon } from "@/components/ui/icons";
import { duration } from "@/lib/format";

/**
 * The node card on the canvas, shared by the editor and the read-only
 * execution replay. Its anatomy is the approved mockup: icon chip, name, type
 * in mono, and a footer strip that only appears once the node has run.
 */
export function WorkflowNode({ data, selected }: NodeProps<FlowNode>) {
  const { node, descriptor, status, itemCount, durationMs, error, readOnly } = data;

  // On a replay, a node the engine never reached has no row at all — the
  // untaken branch of an IF is `skipped`, but everything downstream of a
  // failure simply never happened. Both should read as inert on the canvas.
  const notExecuted = readOnly === true && status === undefined;

  const outputs = descriptor?.Outputs?.length ? descriptor.Outputs : [{ Name: "main", Label: "Output" }];
  const declaredInputs = descriptor?.IsTrigger
    ? []
    : descriptor?.Inputs?.length
      ? descriptor.Inputs
      : [{ Name: "main", Label: "Input" }];

  // Tools hang below the node that calls them, so their socket goes on the
  // bottom edge. Keeping them out of the left-edge list also stops a two-input
  // agent from spreading its data handle off-centre.
  const inputs = declaredInputs.filter((h) => h.Name !== TOOL_HANDLE);
  const toolInputs = declaredInputs.filter((h) => h.Name === TOOL_HANDLE);

  const ring =
    status === "failed"
      ? "border-danger shadow-[0_0_0_4px_rgba(248,113,113,0.14),0_12px_30px_rgba(0,0,0,0.5)]"
      : selected
        ? "border-accent shadow-[0_0_0_4px_rgba(139,92,246,0.16),0_10px_28px_rgba(0,0,0,0.45)]"
        : status === "succeeded"
          ? "border-success/35 shadow-[var(--shadow-panel)]"
          : status === "skipped" || notExecuted
            ? "border-dashed border-[#2b3040]"
            : "border-line-strong shadow-[var(--shadow-panel)]";

  const bg =
    status === "failed"
      ? "bg-[#1d1620]"
      : status === "skipped" || notExecuted
        ? "bg-panel"
        : "bg-raised";

  const chip =
    status === "failed"
      ? "bg-danger/16 text-danger"
      : status === "succeeded"
        ? "bg-success/13 text-success"
        : status === "skipped" || notExecuted
          ? "bg-white/4 text-ink-5"
          : descriptor?.IsTrigger
            ? "bg-accent/15 text-accent-2"
            : "bg-white/6 text-ink-2";

  return (
    <div
      className={clsx(
        "w-[176px] rounded-[13px] border",
        ring,
        bg,
        node.disabled && "opacity-50",
      )}
    >
      {inputs.map((h, i) => (
        <Handle
          key={h.Name}
          type="target"
          id={h.Name}
          position={Position.Left}
          isConnectable={!readOnly}
          style={{ top: handleOffset(i, inputs.length) }}
        />
      ))}

      <div className="flex items-center gap-[11px] px-[13px] py-3">
        <div className={clsx("flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-[9px]", chip)}>
          <NodeIcon name={descriptor?.Icon ?? "table"} size={15} />
        </div>
        <div className="flex min-w-0 flex-col gap-0.5">
          <div
            className={clsx(
              "truncate text-[12.5px]",
              status === "failed" ? "font-bold" : "font-semibold",
              (status === "skipped" || notExecuted) && "text-ink-4",
            )}
            title={node.name}
          >
            {node.name}
          </div>
          <div
            className={clsx(
              "truncate font-mono text-[10px]",
              status === "failed"
                ? "text-danger-2"
                : status === "skipped" || notExecuted
                  ? "text-ink-6"
                  : "text-ink-4",
            )}
          >
            {subline({ status, itemCount, durationMs, error, type: node.type, notExecuted })}
          </div>
        </div>
      </div>

      {status === "failed" && error?.message && (
        <div className="border-t border-danger/22 px-[13px] py-2 font-mono text-[10px] text-danger-3">
          <span className="line-clamp-1">{error.message}</span>
        </div>
      )}

      {status === "succeeded" && itemCount !== undefined && (
        <div className="flex items-center gap-[7px] border-t border-line px-[13px] py-2">
          <span className="h-1.5 w-1.5 rounded-full bg-success" />
          <span className="text-[11px] text-ink-3">
            {itemCount} item{itemCount === 1 ? "" : "s"}
          </span>
          <span className="ml-auto font-mono text-[10px] text-ink-4">{duration(durationMs)}</span>
        </div>
      )}

      {outputs.map((h, i) => (
        <Handle
          key={h.Name}
          type="source"
          id={h.Name}
          position={Position.Right}
          isConnectable={!readOnly}
          style={{ top: handleOffset(i, outputs.length) }}
        />
      ))}

      {toolInputs.map((h) => (
        <Handle
          key={h.Name}
          type="target"
          id={h.Name}
          position={Position.Bottom}
          isConnectable={!readOnly}
          className="is-tool-handle"
          title={`${h.Label} — connect nodes here to let this node call them as tools`}
          aria-label={`${h.Label} input: connect nodes here to let ${node.name} call them as tools`}
        />
      ))}
    </div>
  );
}

/** Spreads several handles down the card's right or left edge. */
function handleOffset(index: number, count: number): number {
  if (count <= 1) return 29;
  const spacing = 22;
  const start = 29 - ((count - 1) * spacing) / 2;
  return start + index * spacing;
}

function subline({
  status,
  itemCount,
  error,
  type,
  notExecuted,
}: {
  status?: string;
  itemCount?: number;
  durationMs?: number;
  error?: { message: string; status?: number; attempts?: number };
  type: string;
  notExecuted?: boolean;
}): string {
  if (notExecuted) return "not executed";
  if (status === "failed") {
    const code = error?.status ? `HTTP ${error.status}` : "Failed";
    return error?.attempts && error.attempts > 1 ? `${code} · ${error.attempts} attempts` : code;
  }
  if (status === "skipped") return `${type} · skipped`;
  if (status === "running") return "running…";
  if (status === "succeeded" && itemCount !== undefined) return type;
  return type;
}

export const canvasNodeTypes = { workflow: WorkflowNode };
