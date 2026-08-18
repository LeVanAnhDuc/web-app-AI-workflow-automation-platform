import type { Graph, GraphNode, NodeExecution } from "@/lib/types";

export interface TimelineRow {
  node: GraphNode;
  /** Absent when the node never ran, which the rail shows as "not executed". */
  exec?: NodeExecution;
}

/**
 * Node executions in start order, with the nodes that never ran appended in
 * graph order. Start order — not graph order — because that is the sequence a
 * person is trying to reconstruct when reading a failure.
 */
export function buildTimeline(graph: Graph, nodeExecutions: NodeExecution[]): TimelineRow[] {
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));
  const started = (ne: NodeExecution) =>
    ne.startedAt ? new Date(ne.startedAt).getTime() : Number.MAX_SAFE_INTEGER;

  const ran = [...nodeExecutions]
    .filter((ne) => byId.has(ne.nodeId))
    .sort((a, b) => started(a) - started(b));

  const seen = new Set(ran.map((ne) => ne.nodeId));

  return [
    ...ran.map((exec) => ({ node: byId.get(exec.nodeId) as GraphNode, exec })),
    ...graph.nodes.filter((n) => !seen.has(n.id)).map((node) => ({ node })),
  ];
}

/** The failed node if there is one, otherwise the last node that ran. */
export function defaultSelection(rows: TimelineRow[]): string | undefined {
  const failed = rows.find((r) => r.exec?.status === "failed");
  if (failed) return failed.node.id;
  const ranRows = rows.filter((r) => r.exec);
  return ranRows.at(-1)?.node.id ?? rows[0]?.node.id;
}
