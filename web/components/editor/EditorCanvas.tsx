"use client";

// base.css, not style.css: it carries only the structural rules plus React
// Flow's `--xy-*` theme variables, which we set from our own tokens below.
// style.css would additionally ship the library's light default look, and being
// a component stylesheet it loads *after* globals.css — so its greys would win
// over the overrides there.
import "@xyflow/react/dist/base.css";

import { Controls, MiniMap, Panel, ReactFlow, useReactFlow, useViewport } from "@xyflow/react";
import type { CSSProperties } from "react";
import { useEffect, useMemo, useRef } from "react";
import { canvasNodeTypes } from "@/components/canvas/WorkflowNode";
import { Icons } from "@/components/ui/icons";
import { Kbd } from "@/components/ui";
import { useEditorStore } from "@/lib/editorStore";
import type { FlowNode } from "@/lib/graph";
import { NODE_DND_MIME } from "./NodePalette";

/** React Flow's theme hooks, bound to the Studio dark tokens. */
const canvasTheme = {
  "--xy-background-color": "var(--color-surface)",
  "--xy-edge-stroke": "var(--color-accent)",
  "--xy-edge-stroke-width": "2",
  "--xy-edge-stroke-selected": "var(--color-accent-3)",
  "--xy-connectionline-stroke": "var(--color-accent-2)",
  "--xy-connectionline-stroke-width": "2",
  "--xy-handle-background-color": "var(--color-ink-5)",
  "--xy-minimap-background-color": "var(--color-panel)",
  "--xy-selection-background-color": "color-mix(in oklab, var(--color-accent) 12%, transparent)",
  "--xy-selection-border": "1px dotted var(--color-accent-2)",
} as CSSProperties;

const branchLabelStyle: CSSProperties = { fontWeight: 700, fontSize: 10 };

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

  // The `true` / `false` branch labels come from `toFlow`; only their paint
  // belongs here, since the tokens are a frontend concern.
  const edges = useMemo(
    () =>
      flowEdges.map((edge) =>
        edge.label
          ? {
              ...edge,
              labelBgPadding: [6, 3] as [number, number],
              labelBgBorderRadius: 8,
              labelBgStyle: { fill: "var(--color-raised)" },
              labelStyle: {
                ...branchLabelStyle,
                fill:
                  edge.label === "false" ? "var(--color-ink-4)" : "var(--color-accent-2)",
              },
            }
          : edge,
      ),
    [flowEdges],
  );

  return (
    <ReactFlow<FlowNode>
      nodes={flowNodes}
      edges={edges}
      style={canvasTheme}
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
        // A mask tint would need a colour outside the palette, and the mockup's
        // minimap has none: the viewport is read from the node blocks alone.
        maskColor="transparent"
        nodeStrokeWidth={0}
        nodeBorderRadius={3}
        nodeColor={minimapNodeColor}
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

/** Returned as an inline `fill`, so it beats the library's own stylesheet. */
function minimapNodeColor(node: FlowNode): string {
  if (node.selected) return "var(--color-accent)";
  if (node.data.status === "failed") return "var(--color-danger)";
  if (node.data.status === "succeeded") return "var(--color-success)";
  if (node.data.status === "skipped") return "var(--color-ink-6)";
  return "color-mix(in oklab, var(--color-accent) 40%, transparent)";
}
