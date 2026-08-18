import { applyEdgeChanges, applyNodeChanges } from "@xyflow/react";
import type { Connection, EdgeChange, NodeChange, XYPosition } from "@xyflow/react";
import { create } from "zustand";
import {
  newEdgeId,
  nodeFromDescriptor,
  runtimeFromExecutions,
  toFlow,
  uniqueNodeName,
  type FlowEdge,
  type FlowNode,
  type NodeRuntime,
} from "./graph";
import type {
  Execution,
  ExecutionStreamEvent,
  Graph,
  GraphNode,
  NodeDescriptor,
  NodeExecution,
  NodeTestResponse,
} from "./types";

/* ---------------------------------------------------------------------------
   Editor state.

   The stored `graph` is the single source of truth: it is what gets saved, what
   validation reads, and what expressions resolve against. `flowNodes` /
   `flowEdges` are a cache of `toFlow(graph)`, rebuilt inside every mutating
   action rather than in a component memo, so React Flow receives stable object
   identities and does not re-measure cards on unrelated renders.
   --------------------------------------------------------------------------- */

const emptyGraph: Graph = { nodes: [], edges: [] };

export interface EditorState {
  graph: Graph;
  descriptors: Record<string, NodeDescriptor>;
  descriptorList: NodeDescriptor[];

  flowNodes: FlowNode[];
  flowEdges: FlowEdge[];

  selectedNodeId: string | null;
  selectedEdgeIds: string[];

  dirty: boolean;
  version: number;

  runtime: Record<string, NodeRuntime>;
  nodeExecutions: NodeExecution[];
  currentExecutionId: string | null;
  execution: Execution | null;
  testResults: Record<string, NodeTestResponse>;

  load: (loaded: { graph: Graph; version: number }) => void;
  setDescriptors: (list: NodeDescriptor[]) => void;

  setNodes: (changes: NodeChange<FlowNode>[]) => void;
  setEdges: (changes: EdgeChange<FlowEdge>[]) => void;

  addNode: (type: string, position?: XYPosition) => string | null;
  updateNode: (id: string, patch: Partial<GraphNode>) => void;
  setParam: (id: string, name: string, value: unknown) => void;
  renameNode: (id: string, name: string) => void;
  deleteSelection: () => void;
  connect: (connection: Connection) => void;
  select: (id: string | null) => void;

  markSaved: (version: number) => void;
  startRun: (execution: Execution) => void;
  applyStreamEvent: (event: ExecutionStreamEvent) => void;
  setTestResult: (nodeId: string, result: NodeTestResponse) => void;
}

export const useEditorStore = create<EditorState>()((set, get) => ({
  graph: emptyGraph,
  descriptors: {},
  descriptorList: [],
  flowNodes: [],
  flowEdges: [],
  selectedNodeId: null,
  selectedEdgeIds: [],
  dirty: false,
  version: 0,
  runtime: {},
  nodeExecutions: [],
  currentExecutionId: null,
  execution: null,
  testResults: {},

  load: ({ graph, version }) =>
    set((s) => ({
      version,
      dirty: false,
      selectedNodeId: null,
      selectedEdgeIds: [],
      runtime: {},
      nodeExecutions: [],
      currentExecutionId: null,
      execution: null,
      testResults: {},
      graph,
      ...rebuild(graph, s.descriptors, {}, null, [], [], []),
    })),

  setDescriptors: (list) =>
    set((s) => {
      const descriptors: Record<string, NodeDescriptor> = {};
      for (const d of list) descriptors[d.Type] = d;
      return {
        descriptors,
        descriptorList: list,
        ...rebuild(
          s.graph,
          descriptors,
          s.runtime,
          s.selectedNodeId,
          s.selectedEdgeIds,
          s.flowNodes,
          s.flowEdges,
        ),
      };
    }),

  setNodes: (changes) =>
    set((s) => {
      const nextNodes = applyNodeChanges<FlowNode>(changes, s.flowNodes);

      let selectedNodeId = s.selectedNodeId;
      for (const c of changes) {
        if (c.type === "select") selectedNodeId = c.selected ? c.id : null;
        if (c.type === "remove" && c.id === selectedNodeId) selectedNodeId = null;
      }

      const graph = pruneEdges({
        nodes: nextNodes.map((n) => ({
          ...n.data.node,
          position: { x: Math.round(n.position.x), y: Math.round(n.position.y) },
        })),
        edges: s.graph.edges,
      });

      return {
        graph,
        dirty: s.dirty || changes.some(isGraphChange),
        selectedNodeId,
        ...rebuild(
          graph,
          s.descriptors,
          s.runtime,
          selectedNodeId,
          s.selectedEdgeIds,
          nextNodes,
          s.flowEdges,
        ),
      };
    }),

  setEdges: (changes) =>
    set((s) => {
      const nextEdges = applyEdgeChanges<FlowEdge>(changes, s.flowEdges);
      const selectedEdgeIds = nextEdges.filter((e) => e.selected).map((e) => e.id);
      const graph: Graph = {
        nodes: s.graph.nodes,
        edges: nextEdges.map((e) => ({
          id: e.id,
          source: e.source,
          sourceHandle: e.sourceHandle || "main",
          target: e.target,
          targetHandle: e.targetHandle || "main",
        })),
      };
      return {
        graph,
        dirty: s.dirty || changes.some(isGraphChange),
        selectedEdgeIds,
        ...rebuild(
          graph,
          s.descriptors,
          s.runtime,
          s.selectedNodeId,
          selectedEdgeIds,
          s.flowNodes,
          nextEdges,
        ),
      };
    }),

  addNode: (type, position) => {
    const s = get();
    const descriptor = s.descriptors[type];
    if (!descriptor) return null;

    const node = nodeFromDescriptor(s.graph, descriptor, position);
    const graph: Graph = { nodes: [...s.graph.nodes, node], edges: s.graph.edges };
    set({
      graph,
      dirty: true,
      selectedNodeId: node.id,
      ...rebuild(
        graph,
        s.descriptors,
        s.runtime,
        node.id,
        s.selectedEdgeIds,
        s.flowNodes,
        s.flowEdges,
      ),
    });
    return node.id;
  },

  updateNode: (id, patch) =>
    set((s) => {
      const graph: Graph = {
        nodes: s.graph.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)),
        edges: s.graph.edges,
      };
      return {
        graph,
        dirty: true,
        ...rebuild(
          graph,
          s.descriptors,
          s.runtime,
          s.selectedNodeId,
          s.selectedEdgeIds,
          s.flowNodes,
          s.flowEdges,
        ),
      };
    }),

  setParam: (id, name, value) => {
    const node = get().graph.nodes.find((n) => n.id === id);
    if (!node) return;
    get().updateNode(id, { params: { ...node.params, [name]: value } });
  },

  renameNode: (id, name) => {
    const s = get();
    const trimmed = name.trim();
    const node = s.graph.nodes.find((n) => n.id === id);
    if (!node || !trimmed || trimmed === node.name) return;
    // Names are the handle expressions use, so uniqueness is enforced here
    // rather than left to the save-time validation.
    const others: Graph = { nodes: s.graph.nodes.filter((n) => n.id !== id), edges: [] };
    s.updateNode(id, { name: uniqueNodeName(others, trimmed) });
  },

  deleteSelection: () =>
    set((s) => {
      const nodeIds = new Set(s.selectedNodeId ? [s.selectedNodeId] : []);
      const edgeIds = new Set(s.selectedEdgeIds);
      if (nodeIds.size === 0 && edgeIds.size === 0) return {};

      const graph = pruneEdges({
        nodes: s.graph.nodes.filter((n) => !nodeIds.has(n.id)),
        edges: s.graph.edges.filter((e) => !edgeIds.has(e.id)),
      });
      return {
        graph,
        dirty: true,
        selectedNodeId: null,
        selectedEdgeIds: [],
        ...rebuild(graph, s.descriptors, s.runtime, null, [], s.flowNodes, s.flowEdges),
      };
    }),

  connect: (connection) =>
    set((s) => {
      const { source, target } = connection;
      if (!source || !target || source === target) return {};
      const sourceHandle = connection.sourceHandle || "main";
      const targetHandle = connection.targetHandle || "main";
      const exists = s.graph.edges.some(
        (e) =>
          e.source === source &&
          e.target === target &&
          e.sourceHandle === sourceHandle &&
          e.targetHandle === targetHandle,
      );
      if (exists) return {};

      const graph: Graph = {
        nodes: s.graph.nodes,
        edges: [
          ...s.graph.edges,
          { id: newEdgeId(s.graph), source, sourceHandle, target, targetHandle },
        ],
      };
      return {
        graph,
        dirty: true,
        ...rebuild(
          graph,
          s.descriptors,
          s.runtime,
          s.selectedNodeId,
          s.selectedEdgeIds,
          s.flowNodes,
          s.flowEdges,
        ),
      };
    }),

  select: (id) =>
    set((s) => {
      if (id === s.selectedNodeId) return {};
      return {
        selectedNodeId: id,
        ...rebuild(
          s.graph,
          s.descriptors,
          s.runtime,
          id,
          s.selectedEdgeIds,
          s.flowNodes,
          s.flowEdges,
        ),
      };
    }),

  markSaved: (version) => set({ dirty: false, version }),

  startRun: (execution) =>
    set((s) => ({
      currentExecutionId: execution.id,
      execution,
      nodeExecutions: [],
      runtime: {},
      testResults: {},
      ...rebuild(
        s.graph,
        s.descriptors,
        {},
        s.selectedNodeId,
        s.selectedEdgeIds,
        s.flowNodes,
        s.flowEdges,
      ),
    })),

  applyStreamEvent: (event) =>
    set((s) => {
      let nodeExecutions = s.nodeExecutions;
      if (event.node) {
        const incoming = event.node;
        const at = nodeExecutions.findIndex((n) => n.nodeId === incoming.nodeId);
        nodeExecutions =
          at === -1
            ? [...nodeExecutions, incoming]
            : nodeExecutions.map((n, i) => (i === at ? incoming : n));
      }

      const execution = event.execution ?? s.execution;
      if (nodeExecutions === s.nodeExecutions) return { execution };

      const runtime = runtimeFromExecutions(nodeExecutions);
      return {
        execution,
        nodeExecutions,
        runtime,
        ...rebuild(
          s.graph,
          s.descriptors,
          runtime,
          s.selectedNodeId,
          s.selectedEdgeIds,
          s.flowNodes,
          s.flowEdges,
        ),
      };
    }),

  setTestResult: (nodeId, result) =>
    set((s) => ({ testResults: { ...s.testResults, [nodeId]: result } })),
}));

/* --- helpers ---------------------------------------------------------------- */

/** True while the current execution is still producing events. */
export function isRunning(state: EditorState): boolean {
  const status = state.execution?.status;
  return status === "running" || status === "queued";
}

/** The node execution for a node id, for the drawer's Output tab. */
export function nodeExecutionFor(
  state: EditorState,
  nodeId: string | null,
): NodeExecution | undefined {
  if (!nodeId) return undefined;
  return state.nodeExecutions.find((n) => n.nodeId === nodeId);
}

/** Selection and dimension changes must not make a saved graph look dirty. */
function isGraphChange(change: NodeChange<FlowNode> | EdgeChange<FlowEdge>): boolean {
  return change.type !== "select" && change.type !== "dimensions";
}

function pruneEdges(graph: Graph): Graph {
  const ids = new Set(graph.nodes.map((n) => n.id));
  return {
    nodes: graph.nodes,
    edges: graph.edges.filter((e) => ids.has(e.source) && ids.has(e.target)),
  };
}

function rebuild(
  graph: Graph,
  descriptors: Record<string, NodeDescriptor>,
  runtime: Record<string, NodeRuntime>,
  selectedNodeId: string | null,
  selectedEdgeIds: string[],
  prevNodes: FlowNode[],
  prevEdges: FlowEdge[],
): { flowNodes: FlowNode[]; flowEdges: FlowEdge[] } {
  const { nodes, edges } = toFlow(graph, { descriptors, runtime });

  const prevNodeById = new Map(prevNodes.map((n) => [n.id, n]));
  const flowNodes = nodes.map((n) => {
    const prev = prevNodeById.get(n.id);
    // `measured` is written by React Flow's resize observer; carrying it over
    // stops every graph edit from re-measuring every card.
    const next: FlowNode = { ...n, selected: n.id === selectedNodeId, measured: prev?.measured };
    return prev && sameNode(prev, next) ? prev : next;
  });

  const selected = new Set(selectedEdgeIds);
  const prevEdgeById = new Map(prevEdges.map((e) => [e.id, e]));
  const flowEdges = edges.map((e) => {
    const prev = prevEdgeById.get(e.id);
    const next: FlowEdge = { ...e, selected: selected.has(e.id) };
    return prev && sameEdge(prev, next) ? prev : next;
  });

  return { flowNodes, flowEdges };
}

function sameNode(a: FlowNode, b: FlowNode): boolean {
  return (
    a.position.x === b.position.x &&
    a.position.y === b.position.y &&
    a.selected === b.selected &&
    a.draggable === b.draggable &&
    a.data.node === b.data.node &&
    a.data.descriptor === b.data.descriptor &&
    a.data.status === b.data.status &&
    a.data.itemCount === b.data.itemCount &&
    a.data.durationMs === b.data.durationMs &&
    a.data.error === b.data.error
  );
}

function sameEdge(a: FlowEdge, b: FlowEdge): boolean {
  return (
    a.source === b.source &&
    a.target === b.target &&
    a.sourceHandle === b.sourceHandle &&
    a.targetHandle === b.targetHandle &&
    a.className === b.className &&
    a.label === b.label &&
    a.selected === b.selected
  );
}
