"use client";

import "@xyflow/react/dist/style.css";

import { Controls, MiniMap, Panel, ReactFlow, useReactFlow, useViewport } from "@xyflow/react";
import { useEffect, useRef } from "react";
import { canvasNodeTypes } from "@/components/canvas/WorkflowNode";
import { Icons } from "@/components/ui/icons";
import { Kbd } from "@/components/ui";
import { useEditorStore } from "@/lib/editorStore";
import type { FlowNode } from "@/lib/graph";
import { NODE_DND_MIME } from "./NodePalette";

export function EditorCanvas({ onOpenPicker }: { onOpenPicker: () => void }) {
  const flowNodes = useEditorStore((s) => s.flowNodes);
  const flowEdges = useEditorStore((s) => s.flowEdges);
  const setNodes = useEditorStore((s) => s.setNodes);
  const setEdges = useEditorStore((s) => s.setEdges);
  const connect = useEditorStore((s) => s.connect);
  const addNode = useEditorStore((s) => s.addNode);
  const select = useEditorStore((s) => s.select);

  const { screenToFlowPosition, fitView } = useReactFlow();
  const fitted = useRef(false);

  // The graph arrives after the first render, so React Flow's own `fitView` prop
  // would fit an empty canvas. Fit once, when there is something to fit.
  useEffect(() => {
    if (fitted.current || flowNodes.length === 0) return;
    fitted.current = true;
    void fitView({ padding: 0.35, maxZoom: 1, duration: 0 });
  }, [flowNodes.length, fitView]);

  return (
    <ReactFlow<FlowNode>
      nodes={flowNodes}
      edges={flowEdges}
      nodeTypes={canvasNodeTypes}
      onNodesChange={setNodes}
      onEdgesChange={setEdges}
      onConnect={connect}
      onPaneClick={() => select(null)}
      onDragOver={(e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
      }}
      onDrop={(e) => {
        const type = e.dataTransfer.getData(NODE_DND_MIME);
        if (!type) return;
        e.preventDefault();
        addNode(type, screenToFlowPosition({ x: e.clientX, y: e.clientY }));
      }}
      // Deletion is handled by the editor's own key handler so it can respect a
      // focused input; React Flow must not also claim the keys.
      deleteKeyCode={null}
      multiSelectionKeyCode="Shift"
      minZoom={0.2}
      maxZoom={2}
      proOptions={{ hideAttribution: true }}
      className="canvas-dots"
    >
      <Panel position="top-left">
        <button
          type="button"
          onClick={onOpenPicker}
          className="flex items-center gap-2 rounded-[10px] border border-line-strong bg-raised px-3 py-2 text-[12.5px] font-semibold text-ink-2 shadow-[var(--shadow-panel)] hover:bg-white/8"
        >
          <Icons.plus size={13} className="text-accent-2" />
          Add node
          <Kbd>Tab</Kbd>
        </button>
      </Panel>

      <Controls position="bottom-left" orientation="horizontal" showInteractive={false}>
        <ZoomLabel />
      </Controls>

      <MiniMap
        position="bottom-right"
        pannable
        zoomable
        ariaLabel="Workflow minimap"
        // The panel's own background comes from globals.css, so the viewport mask
        // stays transparent rather than introducing a colour off-token.
        maskColor="transparent"
        nodeStrokeWidth={0}
        nodeBorderRadius={3}
        nodeClassName={minimapNodeClass}
        style={{ width: 138, height: 82 }}
      />
    </ReactFlow>
  );
}

function ZoomLabel() {
  const { zoom } = useViewport();
  return (
    <span className="flex items-center px-2 text-[12px] font-semibold text-ink-2">
      {Math.round(zoom * 100)}%
    </span>
  );
}

function minimapNodeClass(node: FlowNode): string {
  if (node.selected) return "fill-accent";
  if (node.data.status === "failed") return "fill-danger";
  if (node.data.status === "succeeded") return "fill-success";
  if (node.data.status === "skipped") return "fill-ink-6";
  return "fill-accent/30";
}
