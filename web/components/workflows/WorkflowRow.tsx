"use client";

import clsx from "clsx";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { StatusDot, Toggle } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { relativeTime, triggerLabel } from "@/lib/format";
import type { TriggerType, WorkflowSummary } from "@/lib/types";
import { RowMenu } from "./RowMenu";
import { SuccessBar } from "./SuccessBar";
import { ROW_GRID } from "./grid";

const triggerIcon: Record<TriggerType, (p: { size?: number; className?: string }) => React.ReactElement> = {
  manual: Icons.bolt,
  webhook: Icons.webhook,
  schedule: Icons.clock,
};

export function WorkflowRow({
  workflow,
  onToggleActive,
  onDelete,
  deleting,
}: {
  workflow: WorkflowSummary;
  onToggleActive: (active: boolean) => void;
  onDelete: () => void;
  deleting?: boolean;
}) {
  const router = useRouter();
  const href = `/workflows/${workflow.id}`;
  const TriggerIcon = triggerIcon[workflow.triggerType] ?? Icons.bolt;

  return (
    <div
      className={clsx(
        ROW_GRID,
        "group border-b border-line px-5 py-[15px] last:border-b-0",
        "cursor-pointer hover:bg-accent/6",
      )}
      onClick={(e) => {
        // The name is a real link; let it navigate on its own so middle-click
        // and modifier-click keep working.
        if ((e.target as HTMLElement).closest("a")) return;
        router.push(href);
      }}
    >
      <div className="flex min-w-0 items-center gap-3">
        <span
          className={clsx(
            "flex h-8 w-8 shrink-0 items-center justify-center rounded-[9px] bg-white/6",
            "group-hover:bg-accent/15 group-hover:text-accent-2",
            workflow.active ? "text-ink-2" : "text-ink-4",
          )}
        >
          <TriggerIcon size={15} />
        </span>
        <span className="flex min-w-0 flex-col gap-0.5">
          <Link
            href={href}
            className={clsx(
              "truncate text-[13.5px] font-semibold hover:text-ink",
              "rounded focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
              workflow.active ? "text-ink" : "text-ink-2",
            )}
          >
            {workflow.name}
          </Link>
          <span className="truncate font-mono text-[10.5px] lowercase text-ink-4">
            {triggerLabel(workflow.triggerType, workflow.triggerDetail)} ·{" "}
            {workflow.nodeCount} {workflow.nodeCount === 1 ? "node" : "nodes"}
          </span>
        </span>
      </div>

      {/* The toggle owns its click: flipping a workflow must not open it. */}
      <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
        <Toggle
          checked={workflow.active}
          onChange={onToggleActive}
          tone="success"
          label={`${workflow.active ? "Deactivate" : "Activate"} ${workflow.name}`}
        />
        <span
          className={clsx(
            "text-[12px]",
            workflow.active ? "font-semibold text-success" : "text-ink-4",
          )}
        >
          {workflow.active ? "Active" : "Inactive"}
        </span>
      </div>

      <LastRun workflow={workflow} />

      <SuccessBar rate={workflow.successRate7d} />

      <span className="text-[12.5px] text-ink-3">{relativeTime(workflow.updatedAt)}</span>

      <RowMenu
        workflowName={workflow.name}
        onOpen={() => router.push(href)}
        onDelete={onDelete}
        deleting={deleting}
      />
    </div>
  );
}

function LastRun({ workflow }: { workflow: WorkflowSummary }) {
  const run = workflow.lastRun;
  if (!run) return <span className="text-[12.5px] text-ink-5">Never run</span>;

  const when = relativeTime(run.at);
  const label =
    run.status === "failed"
      ? `Failed ${when}`
      : run.status === "running"
        ? "Running now"
        : when;

  return (
    <span className="flex items-center gap-2">
      <StatusDot status={run.status} />
      <span
        className={clsx(
          "truncate text-[12.5px]",
          run.status === "failed"
            ? "font-semibold text-danger"
            : run.status === "running"
              ? "font-semibold text-accent-3"
              : "text-ink-2",
        )}
      >
        {label}
      </span>
    </span>
  );
}
