"use client";

import { useEffect, useRef, useState } from "react";
import { executions as executionsApi } from "@/lib/api";
import type { Execution, ExecutionStreamEvent, NodeExecution } from "@/lib/types";

export interface ExecutionStreamState {
  /** The latest execution row the server pushed, if any arrived yet. */
  execution?: Execution;
  /** Node executions seen on the wire, keyed by nodeId so re-sends replace. */
  nodes: Record<string, NodeExecution>;
  /** True once a `done` event closed the stream. */
  done: boolean;
  connected: boolean;
}

const idle: ExecutionStreamState = { nodes: {}, done: false, connected: false };

/**
 * Subscribes to `GET /executions/{id}/stream` and accumulates what it pushes.
 * The caller overlays this on top of the fetched detail, so a dropped or
 * unsupported stream degrades to whatever the initial GET returned rather than
 * blanking the screen.
 */
export function useExecutionStream(
  id: string,
  enabled: boolean,
  onDone?: () => void,
): ExecutionStreamState {
  const [state, setState] = useState<ExecutionStreamState>(idle);

  // Held in a ref so a new callback identity does not tear down the stream.
  const doneRef = useRef(onDone);
  doneRef.current = onDone;

  useEffect(() => {
    if (!enabled) return;

    // Reset between subscriptions: stale node rows from a previous execution
    // must never bleed into this one.
    setState(idle);

    const source = new EventSource(executionsApi.streamUrl(id), { withCredentials: true });

    const handle = (raw: MessageEvent<string>) => {
      let event: ExecutionStreamEvent;
      try {
        event = JSON.parse(raw.data) as ExecutionStreamEvent;
      } catch {
        return; // A malformed frame is not worth breaking the screen over.
      }

      setState((prev) => ({
        ...prev,
        connected: true,
        execution: event.execution ?? prev.execution,
        nodes: event.node ? { ...prev.nodes, [event.node.nodeId]: event.node } : prev.nodes,
        done: prev.done || event.type === "done",
      }));

      if (event.type === "done") {
        source.close();
        doneRef.current?.();
      }
    };

    source.addEventListener("execution", handle);
    source.addEventListener("node", handle);
    source.addEventListener("done", handle);
    source.onopen = () => setState((prev) => ({ ...prev, connected: true }));
    source.onerror = () => setState((prev) => ({ ...prev, connected: false }));

    return () => source.close();
  }, [id, enabled]);

  return state;
}

/** Merges streamed node rows over the fetched ones, replacing by nodeId. */
export function mergeNodeExecutions(
  base: NodeExecution[],
  streamed: Record<string, NodeExecution>,
): NodeExecution[] {
  const byNodeId = new Map(base.map((ne) => [ne.nodeId, ne]));
  for (const [nodeId, ne] of Object.entries(streamed)) byNodeId.set(nodeId, ne);
  return [...byNodeId.values()];
}
