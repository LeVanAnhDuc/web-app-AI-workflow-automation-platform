"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { executions as executionsApi, workflows as workflowsApi } from "@/lib/api";
import type { Execution } from "@/lib/types";
import { PageHeader, Topbar } from "@/components/layout/Topbar";
import { Button, Card, EmptyState, ErrorNotice, Segmented, StatusPill } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { duration, errorSubline, relativeTime, triggerLabel } from "@/lib/format";
import { ExecutionRowMenu } from "./ExecutionRowMenu";
import { elapsedMs, useNow } from "./useTicker";

/** Column template from the approved mockup, shared by the head and the rows. */
const GRID = "grid grid-cols-[124px_2.3fr_1.15fr_1.25fr_92px_74px_132px_38px] items-center";

const PAGE_SIZE = 25;
const LIVE_INTERVAL_MS = 3_000;

type StatusFilter = "all" | "succeeded" | "failed" | "running";

const statusOptions: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "succeeded", label: "Succeeded" },
  { value: "failed", label: "Failed" },
  { value: "running", label: "Running" },
];

export function ExecutionsScreen() {
  const router = useRouter();
  const [workflowId, setWorkflowId] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [live, setLive] = useState(true);
  const [actionError, setActionError] = useState<string | null>(null);

  // Cursors used to reach the current page. Empty means "first page"; the API
  // only hands out a forward cursor, so Newer has to walk back down this stack.
  const [cursorStack, setCursorStack] = useState<string[]>([]);
  const cursor = cursorStack.at(-1);

  const refilter = (apply: () => void) => {
    apply();
    setCursorStack([]);
  };

  const workflowList = useQuery({
    queryKey: ["workflows", "for-filter"],
    queryFn: () => workflowsApi.list({ sort: "name" }),
    staleTime: 60_000,
  });

  const query = useQuery({
    queryKey: ["executions", { workflowId, status, cursor }],
    queryFn: () =>
      executionsApi.list({
        workflowId: workflowId || undefined,
        status,
        limit: PAGE_SIZE,
        cursor,
      }),
    refetchInterval: live ? LIVE_INTERVAL_MS : false,
    placeholderData: keepPreviousData,
  });

  const rows = query.data?.executions ?? [];
  const nextCursor = query.data?.nextCursor;
  const failedCount = rows.filter((e) => e.status === "failed").length;
  const anyRunning = rows.some((e) => e.status === "running");
  const now = useNow(anyRunning);

  // The list has no total count in the API, so the only honest subtitle counts
  // what is actually on screen.
  const subtitle = query.isPending
    ? "Loading runs…"
    : `${rows.length} run${rows.length === 1 ? "" : "s"} on this page · ${failedCount} failed`;

  return (
    <div className="flex h-full flex-col">
      <Topbar />

      <div className="flex min-h-0 grow flex-col gap-[22px] px-10 py-8">
        <PageHeader
          title="Executions"
          subtitle={subtitle}
          action={
            <button
              type="button"
              aria-pressed={live}
              onClick={() => setLive((on) => !on)}
              title={
                live
                  ? `The list refreshes every ${LIVE_INTERVAL_MS / 1000} seconds. Click to stop.`
                  : "Click to refresh the list automatically."
              }
              className={clsx(
                "flex items-center gap-[9px] rounded-[10px] border px-[15px] py-[9px] text-[12.5px] font-semibold",
                "focus-visible:outline-2 focus-visible:outline-accent",
                live
                  ? "border-accent/30 bg-accent/12 text-accent-3"
                  : "border-line bg-white/4 text-ink-4 hover:text-ink-3",
              )}
            >
              <span
                className={clsx(
                  "h-[7px] w-[7px] rounded-full",
                  live ? "animate-pulse bg-accent-2" : "bg-ink-6",
                )}
              />
              Live updates {live ? "on" : "off"}
            </button>
          }
        />

        <div className="flex items-center gap-3">
          <label className="flex items-center gap-2.5 rounded-[10px] border border-line-strong bg-panel px-[13px] py-2.5">
            <span className="text-[12.5px] text-ink-3">Workflow</span>
            <span className="relative flex items-center">
              <select
                value={workflowId}
                onChange={(e) => refilter(() => setWorkflowId(e.target.value))}
                className="appearance-none bg-transparent pr-5 text-[12.5px] font-semibold text-ink outline-none focus-visible:outline-2 focus-visible:outline-accent"
              >
                <option value="">All workflows</option>
                {(workflowList.data?.workflows ?? []).map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.name}
                  </option>
                ))}
              </select>
              <Icons.chevronDown className="pointer-events-none absolute right-0 text-ink-4" />
            </span>
          </label>

          <Segmented
            value={status}
            options={statusOptions}
            onChange={(next) => refilter(() => setStatus(next))}
          />

          {/* No date-range parameter exists on GET /executions yet, so the
              control states the fixed server-side window instead of pretending
              to filter. */}
          <span
            className="flex items-center gap-[9px] rounded-[10px] border border-line bg-panel px-[13px] py-2.5 opacity-60"
            title="Server-side date filtering arrives with the next API change; the list currently shows the newest runs the API returns."
          >
            <Icons.calendar size={14} className="text-ink-4" />
            <select
              aria-label="Date range"
              disabled
              value="24h"
              className="cursor-not-allowed appearance-none bg-transparent text-[12.5px] font-semibold text-ink-2 outline-none"
            >
              <option value="24h">Last 24 hours</option>
            </select>
            <Icons.chevronDown className="text-ink-5" />
          </span>
        </div>

        {actionError && <ErrorNotice>{actionError}</ErrorNotice>}

        <Card className="flex flex-col">
          <div
            className={clsx(
              GRID,
              "border-b border-line px-5 py-3 text-[11px] font-bold tracking-[0.06em] text-ink-5",
            )}
          >
            <span>STATUS</span>
            <span>WORKFLOW</span>
            <span>TRIGGER</span>
            <span>STARTED</span>
            <span>DURATION</span>
            <span>ITEMS</span>
            <span>EXECUTION</span>
            <span />
          </div>

          {query.isPending && <SkeletonRows />}

          {query.isError && (
            <div className="flex flex-col items-center gap-3 px-5 py-12">
              <ErrorNotice>
                {query.error instanceof Error
                  ? query.error.message
                  : "The execution list could not be loaded."}
              </ErrorNotice>
              <Button onClick={() => void query.refetch()}>Try again</Button>
            </div>
          )}

          {!query.isPending && !query.isError && rows.length === 0 && (
            <EmptyState
              title="No executions match these filters"
              description="Runs appear here as soon as a workflow is triggered manually, by webhook or on a schedule."
              action={<Button onClick={() => router.push("/workflows")}>Go to workflows</Button>}
            />
          )}

          {rows.map((execution, i) => (
            <ExecutionRow
              key={execution.id}
              execution={execution}
              now={now}
              last={i === rows.length - 1}
              onOpen={() => router.push(`/executions/${execution.id}`)}
              onActionError={setActionError}
            />
          ))}
        </Card>

        <div className="flex items-center gap-3">
          <span
            className="text-[12.5px] text-ink-4"
            title={
              nextCursor || cursor
                ? "The API paginates by cursor and does not return a total count."
                : undefined
            }
          >
            {nextCursor || cursor
              ? `Showing ${rows.length} of …`
              : `Showing ${rows.length} of ${rows.length}`}
          </span>
          <div className="grow" />
          <PagerButton
            label="Newer"
            side="left"
            disabled={cursorStack.length === 0 || query.isFetching}
            onClick={() => setCursorStack((stack) => stack.slice(0, -1))}
          />
          <PagerButton
            label="Older"
            side="right"
            disabled={!nextCursor || query.isFetching}
            onClick={() => setCursorStack((stack) => (nextCursor ? [...stack, nextCursor] : stack))}
          />
        </div>
      </div>
    </div>
  );
}

function ExecutionRow({
  execution,
  now,
  last,
  onOpen,
  onActionError,
}: {
  execution: Execution;
  now: Date;
  last: boolean;
  onOpen: () => void;
  onActionError: (message: string) => void;
}) {
  const running = execution.status === "running";
  const failed = execution.status === "failed";
  const ms = running ? elapsedMs(execution.startedAt, now) : execution.durationMs;

  return (
    <div
      role="link"
      tabIndex={0}
      aria-label={`Open execution ${execution.id}`}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      className={clsx(
        GRID,
        "cursor-pointer px-5 py-[13px] hover:bg-white/3",
        "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent",
        !last && "border-b border-white/4",
        failed && "bg-danger/5",
      )}
    >
      <StatusPill status={execution.status} />

      <div className="flex min-w-0 flex-col gap-[3px] pr-4">
        <span className="truncate text-[13px] font-semibold">
          {execution.workflowName ?? execution.workflowId}
        </span>
        {failed && execution.error && (
          <span className="truncate font-mono text-[10.5px] text-danger-2">
            {errorSubline(execution.error)}
          </span>
        )}
      </div>

      <div className="flex items-center gap-[7px] text-ink-3">
        <TriggerIcon type={execution.triggerType} />
        <span className="text-[12.5px]">{triggerLabel(execution.triggerType)}</span>
      </div>

      <span className="text-[12.5px] text-ink-2">
        {relativeTime(execution.startedAt ?? execution.createdAt, now)}
      </span>

      <span className={clsx("font-mono text-xs", running ? "text-accent-3" : "text-ink-2")}>
        {duration(ms)}
      </span>

      {/* The list endpoint carries no per-execution item count. */}
      <span
        className="font-mono text-xs text-ink-5"
        title="The execution list does not report an item count; open the run to see per-node items."
      >
        —
      </span>

      <span className="truncate font-mono text-[11.5px] text-ink-4">{execution.id}</span>

      <ExecutionRowMenu execution={execution} onError={onActionError} />
    </div>
  );
}

function TriggerIcon({ type }: { type: Execution["triggerType"] }) {
  if (type === "webhook") return <Icons.webhook size={13} className="text-ink-4" />;
  if (type === "schedule") return <Icons.clock size={13} className="text-ink-4" />;
  return <Icons.bolt size={13} className="text-ink-4" />;
}

function SkeletonRows() {
  return (
    <div aria-busy="true" aria-label="Loading executions">
      {Array.from({ length: 8 }).map((_, i) => (
        <div key={i} className={clsx(GRID, "border-b border-white/4 px-5 py-[13px]")}>
          <span className="h-[22px] w-[86px] animate-pulse rounded-full bg-white/6" />
          <span className="mr-6 h-3 animate-pulse rounded bg-white/6" />
          <span className="mr-6 h-3 animate-pulse rounded bg-white/5" />
          <span className="mr-6 h-3 animate-pulse rounded bg-white/5" />
          <span className="mr-4 h-3 animate-pulse rounded bg-white/5" />
          <span className="mr-4 h-3 animate-pulse rounded bg-white/5" />
          <span className="mr-4 h-3 animate-pulse rounded bg-white/5" />
          <span />
        </div>
      ))}
    </div>
  );
}

function PagerButton({
  label,
  side,
  disabled,
  onClick,
}: {
  label: string;
  side: "left" | "right";
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={clsx(
        "flex items-center gap-[7px] rounded-[9px] border bg-panel px-3.5 py-2 text-[12.5px]",
        "focus-visible:outline-2 focus-visible:outline-accent",
        disabled
          ? "cursor-not-allowed border-line text-ink-5"
          : "border-line-strong font-semibold text-ink-2 hover:bg-white/6",
      )}
    >
      {side === "left" && <Icons.chevronLeft size={13} />}
      {label}
      {side === "right" && <Icons.chevronRight size={13} />}
    </button>
  );
}
