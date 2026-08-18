"use client";

import { useMemo } from "react";
import { ReactFlow } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { canvasNodeTypes } from "@/components/canvas/WorkflowNode";
import { toFlow, type NodeRuntime } from "@/lib/graph";
import type { Graph, NodeDescriptor } from "@/lib/types";
import { SectionLabel } from "@/components/ui";

/**
 * The read-only replay of the run. Same node cards as the editor canvas, so a
 * failed node looks identical in both places; only the interactions differ —
 * pan, zoom and selection stay, everything that mutates the graph is off.
 */
export function GraphReplay({
  graph,
  runtime,
  descriptors,
  version,
  selectedNodeId,
  onSelect,
}: {
  graph: Graph;
  runtime: Record<string, NodeRuntime>;
  descriptors: Record<string, NodeDescriptor>;
  version?: number;
  selectedNodeId?: string;
  onSelect: (nodeId: string) => void;
}) {
  const flow = useMemo(
    () => toFlow(graph, { descriptors, runtime, readOnly: true }),
    [graph, descriptors, runtime],
  );

  const nodes = useMemo(
    () => flow.nodes.map((n) => ({ ...n, selected: n.id === selectedNodeId })),
    [flow.nodes, selectedNodeId],
  );

  return (
    <div className="canvas-dots relative h-[348px] shrink-0 overflow-hidden">
      <ReactFlow
        nodes={nodes}
        edges={flow.edges}
        nodeTypes={canvasNodeTypes}
        onNodeClick={(_, node) => onSelect(node.id)}
        fitView
        fitViewOptions={{ padding: 0.22, maxZoom: 1 }}
        minZoom={0.3}
        maxZoom={1.6}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={false}
        elementsSelectable
        deleteKeyCode={null}
        proOptions={{ hideAttribution: true }}
        style={{ background: "transparent" }}
      />

      <div className="pointer-events-none absolute left-[26px] top-[18px] flex items-center gap-[9px]">
        <SectionLabel>GRAPH REPLAY</SectionLabel>
        <span className="text-[11.5px] text-ink-6">
          read-only{version !== undefined ? ` · version v${version}` : ""}
        </span>
      </div>
    </div>
  );
}
