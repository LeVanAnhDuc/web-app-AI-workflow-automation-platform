"use client";

import { useEffect, useRef } from "react";
import { executions } from "@/lib/api";
import type { Execution, ExecutionStreamEvent, NodeExecution } from "@/lib/types";

/**
 * Subscribes to `GET /executions/{id}/stream` while an execution is live.
 *
 * The server closes the stream once the execution is terminal, but EventSource
 * treats any close as a hiccup and reconnects for ever. So we close explicitly
 * on `done`, on unmount, and on the first transport error — a dead API must not
 * turn into a reconnect loop behind the editor.
 */
export function useExecutionStream(
  executionId: string | null,
  onEvent: (event: ExecutionStreamEvent) => void,
  options: { onError?: () => void } = {},
): void {
  const onEventRef = useRef(onEvent);
  const onErrorRef = useRef(options.onError);

  useEffect(() => {
    onEventRef.current = onEvent;
    onErrorRef.current = options.onError;
  }, [onEvent, options.onError]);

  useEffect(() => {
    if (!executionId) return;

    const source = new EventSource(executions.streamUrl(executionId));
    let finished = false;

    const listen = (name: ExecutionStreamEvent["type"]) => {
      const handler = (raw: MessageEvent<string>) => {
        const event = parseEvent(name, raw.data);
        if (event) onEventRef.current(event);
        if (name === "done") {
          finished = true;
          source.close();
        }
      };
      source.addEventListener(name, handler as EventListener);
    };

    listen("execution");
    listen("node");
    listen("done");

    source.addEventListener("error", () => {
      source.close();
      // The server ending a finished stream is not a failure worth reporting.
      if (!finished) onErrorRef.current?.();
    });

    return () => source.close();
  }, [executionId]);
}

/**
 * The handler names its events, so the payload may be either the envelope from
 * `ExecutionStreamEvent` or the bare resource. Accept both.
 */
function parseEvent(
  name: ExecutionStreamEvent["type"],
  data: string,
): ExecutionStreamEvent | null {
  if (!data) return null;
  let raw: unknown;
  try {
    raw = JSON.parse(data);
  } catch {
    return null;
  }
  if (typeof raw !== "object" || raw === null) return null;

  const body = raw as Record<string, unknown>;
  const type = typeof body.type === "string" ? (body.type as ExecutionStreamEvent["type"]) : name;

  if (name === "node") {
    const node = ("node" in body ? body.node : body) as NodeExecution | undefined;
    return node && node.nodeId ? { type, node } : null;
  }

  const execution = ("execution" in body ? body.execution : body) as Execution | undefined;
  return { type, execution: execution && execution.id ? execution : undefined };
}
