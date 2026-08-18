"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, executions as executionsApi, nodeTypes as nodeTypesApi } from "@/lib/api";
import { runtimeFromExecutions } from "@/lib/graph";
import type { NodeDescriptor } from "@/lib/types";
import { Button, EmptyState, ErrorNotice, Spinner } from "@/components/ui";
import { DataViewer } from "./DataViewer";
import { DetailTopbar } from "./DetailTopbar";
import { ExecutionHeaderStrip } from "./ExecutionHeaderStrip";
import { GraphReplay } from "./GraphReplay";
import { NodeTimeline } from "./NodeTimeline";
import { buildTimeline, defaultSelection } from "./timeline";
import { mergeNodeExecutions, useExecutionStream } from "./useExecutionStream";

const LIVE_POLL_MS = 3_000;

export function ExecutionDetail({ id }: { id: string }) {
  const queryClient = useQueryClient();
  const [pickedNodeId, setPickedNodeId] = useState<string | undefined>(undefined);
  const [actionError, setActionError] = useState<string | null>(null);

  const query = useQuery({
    queryKey: ["execution", id],
    queryFn: () => executionsApi.get(id),
    // A safety net under the SSE stream: if the browser cannot hold the stream
    // open, the screen still advances while the run is in flight.
    refetchInterval: (q) => {
      const status = q.state.data?.execution.status;
      return status === "running" || status === "queued" ? LIVE_POLL_MS : false;
    },
  });

  const descriptorQuery = useQuery({
    queryKey: ["node-types"],
    queryFn: nodeTypesApi.list,
    staleTime: 5 * 60_000,
    retry: false,
  });

  const descriptors = useMemo(() => {
    const out: Record<string, NodeDescriptor> = {};
    for (const d of descriptorQuery.data?.nodeTypes ?? []) out[d.Type] = d;
    return out;
  }, [descriptorQuery.data]);

  const detail = query.data;
  const polledStatus = detail?.execution.status;
  const live = polledStatus === "running" || polledStatus === "queued";

  const stream = useExecutionStream(id, live, () =>
    queryClient.invalidateQueries({ queryKey: ["execution", id] }),
  );

  const execution = stream.execution ?? detail?.execution;

  const nodeExecutions = useMemo(
    () => (detail ? mergeNodeExecutions(detail.nodeExecutions, stream.nodes) : []),
    [detail, stream.nodes],
  );

  const timeline = useMemo(
    () => (detail ? buildTimeline(detail.graph, nodeExecutions) : []),
    [detail, nodeExecutions],
  );

  const runtime = useMemo(() => runtimeFromExecutions(nodeExecutions), [nodeExecutions]);

  // Until the reader picks a node, follow the run: the failed node, else the
  // last one that ran.
  const autoNodeId = defaultSelection(timeline);
  const selectedNodeId =
    pickedNodeId && timeline.some((r) => r.node.id === pickedNodeId) ? pickedNodeId : autoNodeId;

  const selectedIndex = timeline.findIndex((r) => r.node.id === selectedNodeId);
  const selectedRow = selectedIndex >= 0 ? timeline[selectedIndex] : undefined;
  const completedBefore = timeline
    .slice(0, Math.max(0, selectedIndex))
    .filter((r) => r.exec?.status === "succeeded").length;

  return (
    <div className="flex h-dvh flex-col overflow-hidden">
      <DetailTopbar executionId={id} />

      {query.isPending && (
        <div className="flex grow items-center justify-center gap-2.5 text-[13px] text-ink-4">
          <Spinner className="text-accent-2" />
          Loading execution…
        </div>
      )}

      {query.isError && (
        <div className="flex grow flex-col items-center justify-center gap-4 px-6">
          {query.error instanceof ApiError && query.error.status === 404 ? (
            <EmptyState
              title="That execution no longer exists"
              description="It may have been removed, or the id in the address is wrong."
              action={
                <Link href="/executions">
                  <span className="text-[13px] font-semibold">Back to executions</span>
                </Link>
              }
            />
          ) : (
            <>
              <ErrorNotice>
                {query.error instanceof Error
                  ? query.error.message
                  : "The execution could not be loaded."}
              </ErrorNotice>
              <Button onClick={() => void query.refetch()}>Try again</Button>
            </>
          )}
        </div>
      )}

      {detail && execution && (
        <>
          <ExecutionHeaderStrip
            execution={execution}
            workflowName={execution.workflowName ?? execution.workflowId}
            nodeCount={detail.graph.nodes.length}
            completedCount={nodeExecutions.filter((ne) => ne.status === "succeeded").length}
            onError={setActionError}
          />

          {actionError && (
            <div className="shrink-0 border-b border-line bg-panel px-[26px] py-2.5">
              <ErrorNotice>{actionError}</ErrorNotice>
            </div>
          )}

          <GraphReplay
            graph={detail.graph}
            runtime={runtime}
            descriptors={descriptors}
            version={execution.version}
            selectedNodeId={selectedNodeId}
            onSelect={setPickedNodeId}
          />

          <div className="flex min-h-0 grow border-t border-line bg-panel">
            <NodeTimeline
              rows={timeline}
              selectedNodeId={selectedNodeId}
              onSelect={setPickedNodeId}
            />
            <DataViewer
              row={selectedRow}
              graph={detail.graph}
              completedBefore={completedBefore}
            />
          </div>
        </>
      )}
    </div>
  );
}
