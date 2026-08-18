"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { executions as executionsApi } from "@/lib/api";
import type { Execution, Status } from "@/lib/types";
import { Badge, Button, Spinner } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { absoluteTime, duration, triggerLabel } from "@/lib/format";
import { elapsedMs, useNow } from "./useTicker";

export function ExecutionHeaderStrip({
  execution,
  workflowName,
  nodeCount,
  completedCount,
  onError,
}: {
  execution: Execution;
  workflowName: string;
  nodeCount: number;
  completedCount: number;
  onError: (message: string) => void;
}) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const live = execution.status === "running" || execution.status === "queued";
  const now = useNow(execution.status === "running");

  const retry = useMutation({
    mutationFn: () => executionsApi.retry(execution.id),
    onSuccess: ({ execution: next }) => {
      void queryClient.invalidateQueries({ queryKey: ["executions"] });
      router.push(`/executions/${next.id}`);
    },
    onError: (err: Error) => onError(err.message),
  });

  const cancel = useMutation({
    mutationFn: () => executionsApi.cancel(execution.id),
    onSuccess: () => {
      // The engine notices the cancel flag on its next node boundary, so the
      // authoritative row comes from a refetch rather than this response.
      void queryClient.invalidateQueries({ queryKey: ["execution", execution.id] });
      void queryClient.invalidateQueries({ queryKey: ["executions"] });
    },
    onError: (err: Error) => onError(err.message),
  });

  const ms = execution.status === "running"
    ? elapsedMs(execution.startedAt, now)
    : execution.durationMs;

  return (
    <div className="flex h-[84px] shrink-0 items-center gap-[22px] border-b border-line bg-panel px-[26px]">
      <StatusChip status={execution.status} />

      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex items-center gap-[9px]">
          <h1 className="truncate text-[17px] font-bold tracking-[-0.02em]">{workflowName}</h1>
          {execution.version !== undefined && <Badge>v{execution.version}</Badge>}
        </div>
        <div className="flex items-center gap-[9px] text-xs text-ink-4">
          <span>{triggerLabel(execution.triggerType)}</span>
          <Dot />
          <span>{absoluteTime(execution.startedAt ?? execution.createdAt)}</span>
          <Dot />
          <span className="font-mono">{duration(ms)}</span>
          <Dot />
          <span>
            {completedCount} of {nodeCount} nodes completed
          </span>
        </div>
      </div>

      <div className="grow" />

      {/* A link, not a Button: the editor should be openable in a new tab, and
          the primitive set has no anchor-shaped button. */}
      <Link
        href={`/workflows/${execution.workflowId}`}
        className={clsx(
          "inline-flex items-center gap-2 rounded-[10px] border border-line-strong bg-white/5 px-4 py-2.5",
          "text-[13px] font-semibold text-ink-2 hover:bg-white/10 focus-visible:outline-2 focus-visible:outline-accent",
        )}
      >
        <Icons.expand size={14} />
        Open in editor
      </Link>

      {execution.status === "failed" && (
        <Button variant="primary" disabled={retry.isPending} onClick={() => retry.mutate()}>
          {retry.isPending ? <Spinner /> : <Icons.retry size={14} />}
          Retry from failed node
        </Button>
      )}

      {live && (
        <Button variant="danger" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
          {cancel.isPending ? <Spinner /> : <Icons.stop size={12} />}
          Cancel
        </Button>
      )}
    </div>
  );
}

function Dot() {
  return (
    <span className="text-ink-6" aria-hidden>
      ·
    </span>
  );
}

const chipTone: Record<Status, { label: string; text: string; bg: string }> = {
  queued: { label: "Queued", text: "text-accent-3", bg: "bg-accent/15" },
  running: { label: "Running", text: "text-accent-3", bg: "bg-accent/15" },
  succeeded: { label: "Succeeded", text: "text-success", bg: "bg-success/15" },
  failed: { label: "Failed", text: "text-danger", bg: "bg-danger/15" },
  skipped: { label: "Skipped", text: "text-ink-3", bg: "bg-white/6" },
  cancelled: { label: "Cancelled", text: "text-warning", bg: "bg-warning/14" },
};

function StatusChip({ status }: { status: Status }) {
  const tone = chipTone[status] ?? chipTone.queued;
  return (
    <span
      className={clsx(
        "flex shrink-0 items-center gap-2 rounded-full px-[13px] py-1.5",
        tone.bg,
        tone.text,
      )}
    >
      <ChipIcon status={status} />
      <span className="text-[12.5px] font-bold">{tone.label}</span>
    </span>
  );
}

function ChipIcon({ status }: { status: Status }) {
  if (status === "failed") return <Icons.alert size={14} />;
  if (status === "running") return <Spinner className="h-3.5 w-3.5" />;
  if (status === "queued") return <Icons.clock size={14} />;
  if (status === "cancelled") return <Icons.stop size={12} />;
  if (status === "skipped") return <Icons.close size={14} />;
  // The shared icon set has no check glyph and icons.tsx belongs to another
  // surface, so the success tick is drawn locally on the same 24px grid.
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M5 13l4 4L19 7" />
    </svg>
  );
}
