"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, workflows as workflowsApi } from "@/lib/api";
import { Button, Card, EmptyState, ErrorNotice } from "@/components/ui";
import { Icons } from "@/components/ui/icons";
import { PageHeader, Topbar } from "@/components/layout/Topbar";
import {
  FilterBar,
  type SortKey,
  type StatusFilter,
} from "@/components/workflows/FilterBar";
import { SkeletonRows, TableHead } from "@/components/workflows/WorkflowTable";
import { WorkflowRow } from "@/components/workflows/WorkflowRow";
import { useDebouncedValue } from "@/components/workflows/useDebouncedValue";
import type { WorkflowSummary } from "@/lib/types";

interface WorkflowList {
  workflows: WorkflowSummary[];
}

function message(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  return "Could not reach the API. Check that the server is running.";
}

export default function WorkflowsPage() {
  const router = useRouter();
  const qc = useQueryClient();

  const [q, setQ] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [sort, setSort] = useState<SortKey>("updated");
  const debouncedQ = useDebouncedValue(q, 250);

  const listKey = ["workflows", { q: debouncedQ, status, sort }] as const;
  const list = useQuery({
    queryKey: listKey,
    queryFn: () => workflowsApi.list({ q: debouncedQ, status, sort }),
  });

  const setActive = useMutation({
    mutationFn: ({ id, active }: { id: string; active: boolean }) =>
      workflowsApi.patch(id, { active }),
    // Optimistic: the switch has to feel instant, and activation also
    // reconciles webhooks/schedules server-side so the round trip is not short.
    onMutate: async ({ id, active }) => {
      await qc.cancelQueries({ queryKey: listKey });
      const previous = qc.getQueryData<WorkflowList>(listKey);
      if (previous) {
        qc.setQueryData<WorkflowList>(listKey, {
          workflows: previous.workflows.map((w) => (w.id === id ? { ...w, active } : w)),
        });
      }
      return { previous };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) qc.setQueryData(listKey, ctx.previous);
    },
    // The status filter may now exclude the row, so refetch rather than trust
    // the local patch.
    onSettled: () => qc.invalidateQueries({ queryKey: ["workflows"] }),
  });

  const remove = useMutation({
    mutationFn: (id: string) => workflowsApi.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workflows"] }),
  });

  const create = useMutation({
    mutationFn: () => workflowsApi.create("Untitled workflow"),
    onSuccess: (created) => {
      qc.invalidateQueries({ queryKey: ["workflows"] });
      router.push(`/workflows/${created.workflow.id}`);
    },
  });

  const rows = list.data?.workflows ?? [];
  const activeCount = rows.filter((w) => w.active).length;
  const filtered = debouncedQ.trim().length > 0 || status !== "all";

  const newWorkflowButton = (
    <Button
      variant="primary"
      onClick={() => create.mutate()}
      disabled={create.isPending}
    >
      <Icons.plus size={14} />
      New workflow
    </Button>
  );

  return (
    <div className="flex min-h-screen flex-col bg-surface">
      <Topbar />

      <div className="flex min-h-0 grow flex-col gap-[22px] px-10 py-8">
        <PageHeader
          title="Workflows"
          subtitle={
            list.data
              ? `${rows.length} workflow${rows.length === 1 ? "" : "s"} · ${activeCount} active`
              : undefined
          }
          action={newWorkflowButton}
        />

        <FilterBar
          q={q}
          onQChange={setQ}
          status={status}
          onStatusChange={setStatus}
          sort={sort}
          onSortChange={setSort}
        />

        {create.isError && <ErrorNotice>{message(create.error)}</ErrorNotice>}
        {setActive.isError && (
          <ErrorNotice>Could not change the status — {message(setActive.error)}</ErrorNotice>
        )}
        {remove.isError && (
          <ErrorNotice>Could not delete the workflow — {message(remove.error)}</ErrorNotice>
        )}

        {list.isError ? (
          <Card className="p-5">
            <div className="flex flex-col items-start gap-3">
              <ErrorNotice>{message(list.error)}</ErrorNotice>
              <Button onClick={() => list.refetch()} disabled={list.isFetching}>
                <Icons.retry size={14} />
                {list.isFetching ? "Retrying…" : "Try again"}
              </Button>
            </div>
          </Card>
        ) : (
          <Card>
            <TableHead />

            {list.isPending ? (
              <SkeletonRows />
            ) : rows.length === 0 ? (
              <EmptyState
                title={filtered ? "No workflows match these filters" : "No workflows yet"}
                description={
                  filtered
                    ? "Try a different name, or switch the status filter back to All."
                    : "A workflow is a graph of nodes: a trigger, then the steps it runs. Create one to get started."
                }
                action={
                  <div className="flex items-center gap-2">
                    {filtered && (
                      <Button
                        onClick={() => {
                          setQ("");
                          setStatus("all");
                        }}
                      >
                        Clear filters
                      </Button>
                    )}
                    {newWorkflowButton}
                  </div>
                }
              />
            ) : (
              rows.map((w) => (
                <WorkflowRow
                  key={w.id}
                  workflow={w}
                  onToggleActive={(active) => setActive.mutate({ id: w.id, active })}
                  onDelete={() => remove.mutate(w.id)}
                  deleting={remove.isPending && remove.variables === w.id}
                />
              ))
            )}
          </Card>
        )}
      </div>
    </div>
  );
}
