import type { Edge as RFEdge, Node as RFNode } from "@xyflow/react";
import type {
  Graph,
  GraphEdge,
  GraphNode,
  NodeDescriptor,
  NodeError,
  NodeExecution,
  Status,
} from "./types";
import { defaultNodeSettings } from "./types";

/* ---------------------------------------------------------------------------
   Conversion between the stored graph and what React Flow renders, plus the
   graph helpers both the editor and the execution replay need. Pure functions
   only — no React, no fetching — so they are cheap to unit-test.
   --------------------------------------------------------------------------- */

/**
 * The input handle a node declares to accept tools. An edge into it is a
 * *capability* edge, not a data edge: nothing pushes items along it, and the
 * consuming node calls the source on demand instead. Mirrors nodes.HandleTool.
 */
export const TOOL_HANDLE = "tool";

/** The type of the node that consumes tools, for the checks only it needs. */
export const AI_AGENT_TYPE = "ai.agent";

export interface FlowNodeData extends Record<string, unknown> {
  node: GraphNode;
  descriptor?: NodeDescriptor;
  status?: Status;
  itemCount?: number;
  durationMs?: number;
  error?: NodeError;
  readOnly?: boolean;
}

export type FlowNode = RFNode<FlowNodeData, "workflow">;
export type FlowEdge = RFEdge;

export interface NodeRuntime {
  status?: Status;
  itemCount?: number;
  durationMs?: number;
  error?: NodeError;
}

export interface ToFlowOptions {
  descriptors?: Record<string, NodeDescriptor>;
  runtime?: Record<string, NodeRuntime>;
  readOnly?: boolean;
}

/** Builds the React Flow node and edge arrays from a stored graph. */
export function toFlow(graph: Graph, opts: ToFlowOptions = {}): { nodes: FlowNode[]; edges: FlowEdge[] } {
  const { descriptors = {}, runtime = {}, readOnly = false } = opts;

  const nodes: FlowNode[] = graph.nodes.map((n) => {
    const rt = runtime[n.id] ?? {};
    return {
      id: n.id,
      type: "workflow" as const,
      position: n.position,
      draggable: !readOnly,
      selectable: true,
      connectable: !readOnly,
      data: {
        node: n,
        descriptor: descriptors[n.type],
        status: rt.status,
        itemCount: rt.itemCount,
        durationMs: rt.durationMs,
        error: rt.error,
        readOnly,
      },
    };
  });

  const edges: FlowEdge[] = graph.edges.map((e) => {
    const sourceStatus = runtime[e.source]?.status;
    const targetStatus = runtime[e.target]?.status;
    const tool = isToolEdge(e);
    // A tool edge never carries the run's colour: the source is called by the
    // agent, so "succeeded green pointing at the agent" would read backwards.
    const status = tool ? undefined : edgeClass(sourceStatus, targetStatus);
    return {
      id: e.id,
      source: e.source,
      sourceHandle: e.sourceHandle || "main",
      target: e.target,
      targetHandle: e.targetHandle || "main",
      className: [tool ? "is-tool" : undefined, status].filter(Boolean).join(" ") || undefined,
      label: tool ? TOOL_HANDLE : branchLabel(e.sourceHandle),
      deletable: !readOnly,
      focusable: !readOnly,
    };
  });

  return { nodes, edges };
}

/** Whether an edge grants a tool rather than carrying data. */
export function isToolEdge(edge: Pick<GraphEdge, "targetHandle">): boolean {
  return (edge.targetHandle || "main") === TOOL_HANDLE;
}

/** The nodes wired into a node's tool handle, in the order they were connected. */
export function toolProvidersOf(graph: Graph, nodeId: string): GraphNode[] {
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));
  const out: GraphNode[] = [];
  const seen = new Set<string>();
  for (const e of graph.edges) {
    if (e.target !== nodeId || !isToolEdge(e) || seen.has(e.source)) continue;
    const provider = byId.get(e.source);
    if (!provider) continue;
    seen.add(e.source);
    out.push(provider);
  }
  return out;
}

/** Whether a node type declares a tool input, so a tool edge into it does something. */
export function acceptsTools(descriptor: NodeDescriptor | undefined): boolean {
  return (descriptor?.Inputs ?? []).some((h) => h.Name === TOOL_HANDLE);
}

/**
 * A node that exists only to be called as a tool: nothing feeds it, and its
 * only outgoing edges are tool edges. The engine neither runs nor skips such a
 * node, so the editor must not treat it as a dead end either.
 */
export function isToolOnlyNode(graph: Graph, nodeId: string): boolean {
  const outgoing = graph.edges.filter((e) => e.source === nodeId);
  if (outgoing.length === 0 || !outgoing.every(isToolEdge)) return false;
  return !graph.edges.some((e) => e.target === nodeId && !isToolEdge(e));
}

function edgeClass(source?: Status, target?: Status): string | undefined {
  if (source === "failed") return "is-error";
  if (!source) return undefined;
  if (source === "succeeded" && (target === undefined || target === "skipped")) {
    return target === "skipped" ? "is-muted" : "is-success";
  }
  if (source === "skipped") return "is-muted";
  if (source === "succeeded") return "is-success";
  return undefined;
}

function branchLabel(handle: string | undefined): string | undefined {
  if (handle === "true" || handle === "false") return handle;
  return undefined;
}

/** Rebuilds the stored graph from what React Flow currently holds. */
export function fromFlow(nodes: FlowNode[], edges: FlowEdge[]): Graph {
  return {
    nodes: nodes.map((n) => ({
      ...n.data.node,
      position: { x: Math.round(n.position.x), y: Math.round(n.position.y) },
    })),
    edges: edges.map<GraphEdge>((e) => ({
      id: e.id,
      source: e.source,
      sourceHandle: e.sourceHandle || "main",
      target: e.target,
      targetHandle: e.targetHandle || "main",
    })),
  };
}

let idCounter = 0;

/** A graph-local node id. Short and readable, unlike a UUID, in saved JSON. */
export function newNodeId(graph?: Graph): string {
  const taken = new Set(graph?.nodes.map((n) => n.id) ?? []);
  do {
    idCounter += 1;
  } while (taken.has(`n${idCounter}`));
  return `n${idCounter}`;
}

export function newEdgeId(graph?: Graph): string {
  const taken = new Set(graph?.edges.map((e) => e.id) ?? []);
  let i = (graph?.edges.length ?? 0) + 1;
  while (taken.has(`e${i}`)) i += 1;
  return `e${i}`;
}

/**
 * Node names are the handle expressions use, so they must stay unique.
 * "Fetch profile" twice becomes "Fetch profile" and "Fetch profile 2".
 */
export function uniqueNodeName(graph: Graph, base: string): string {
  const taken = new Set(graph.nodes.map((n) => n.name));
  if (!taken.has(base)) return base;
  let i = 2;
  while (taken.has(`${base} ${i}`)) i += 1;
  return `${base} ${i}`;
}

/** Where a newly added node lands: to the right of the rightmost node. */
export function nextPosition(graph: Graph): { x: number; y: number } {
  if (graph.nodes.length === 0) return { x: 80, y: 160 };
  const right = graph.nodes.reduce((acc, n) => (n.position.x > acc.position.x ? n : acc));
  return { x: right.position.x + 240, y: right.position.y };
}

/** Builds a node from a descriptor, filling parameter defaults. */
export function nodeFromDescriptor(
  graph: Graph,
  descriptor: NodeDescriptor,
  position?: { x: number; y: number },
): GraphNode {
  const params: Record<string, unknown> = {};
  for (const p of descriptor.Params ?? []) {
    if (p.Type === "notice") continue;
    if (p.Default !== undefined && p.Default !== null) params[p.Name] = p.Default;
  }
  return {
    id: newNodeId(graph),
    type: descriptor.Type,
    name: uniqueNodeName(graph, descriptor.Name),
    position: position ?? nextPosition(graph),
    params,
    credentialId: null,
    settings: { ...defaultNodeSettings },
  };
}

/** Problems worth blocking a save for, phrased for a person. */
export function validateGraph(graph: Graph, descriptors: Record<string, NodeDescriptor>): string[] {
  const problems: string[] = [];

  const triggers = graph.nodes.filter((n) => descriptors[n.type]?.IsTrigger);
  if (graph.nodes.length > 0 && triggers.length === 0) {
    problems.push("The workflow has no trigger node, so nothing can start it.");
  }
  if (triggers.length > 1) {
    problems.push(`There are ${triggers.length} trigger nodes; a workflow may only have one.`);
  }

  const names = new Map<string, number>();
  for (const n of graph.nodes) names.set(n.name, (names.get(n.name) ?? 0) + 1);
  for (const [name, count] of names) {
    if (count > 1) problems.push(`Two nodes are both called “${name}”; names must be unique.`);
  }

  if (hasCycle(graph)) problems.push("The connections form a loop, which cannot be executed.");

  for (const n of graph.nodes) {
    const d = descriptors[n.type];
    if (!d) {
      problems.push(`Node “${n.name}” has unknown type ${n.type}.`);
      continue;
    }
    // A node that requires a credential and has none chosen will fail the moment
    // it runs. Saying so here costs nothing; finding out from a failed 3am
    // scheduled run costs a morning. A node whose credential is optional — the
    // AI nodes, which fall back to a server-side key — is left alone.
    if (d.Credential && !d.CredentialOptional && !n.credentialId) {
      problems.push(`Node “${n.name}” needs a credential; choose or create one.`);
    }

    for (const p of d.Params ?? []) {
      if (!p.Required) continue;
      if (!isParamVisible(p, n.params)) continue;
      const v = n.params[p.Name];
      if (v === undefined || v === null || v === "") {
        problems.push(`Node “${n.name}” is missing ${p.Label}.`);
      }
    }
  }

  problems.push(...toolProblems(graph, descriptors));

  return problems;
}

/**
 * The tool-wiring problems, which the engine also refuses — but at run time,
 * once the author has already paid for a failed execution.
 */
function toolProblems(graph: Graph, descriptors: Record<string, NodeDescriptor>): string[] {
  const problems: string[] = [];
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));

  for (const e of graph.edges) {
    if (!isToolEdge(e)) continue;
    const target = byId.get(e.target);
    const descriptor = target ? descriptors[target.type] : undefined;
    // An unknown or dangling target is already reported; saying it twice in
    // different words would only make the list harder to read.
    if (!target || !descriptor || acceptsTools(descriptor)) continue;
    const source = byId.get(e.source)?.name ?? e.source;
    problems.push(
      `Node “${target.name}” does not take tools, so the tool connection from “${source}” would be ignored.`,
    );
  }

  for (const n of graph.nodes) {
    if (n.type !== AI_AGENT_TYPE) continue;
    const providers = toolProvidersOf(graph, n.id);
    if (providers.length === 0) continue;

    const described = new Set(
      keyValuePairs(n.params.toolDescriptions)
        .filter((row) => row.value.trim() !== "")
        .map((row) => row.key),
    );
    const undescribed = providers.filter((p) => !described.has(p.name)).map((p) => `“${p.name}”`);
    if (undescribed.length > 0) {
      problems.push(
        `Node “${n.name}” needs a tool description for ${undescribed.join(", ")}; ` +
          "the model chooses tools by their description.",
      );
    }
  }

  return problems;
}

export interface KeyValuePair {
  key: string;
  value: string;
}

/**
 * Reads a `keyValue` param. Params come out of a jsonb column, so the shape is
 * whatever an older graph or a hand edit left there: the rows the editor writes,
 * or a plain object from someone who typed JSON.
 */
export function keyValuePairs(value: unknown): KeyValuePair[] {
  if (Array.isArray(value)) {
    return value.map((row) => {
      if (typeof row !== "object" || row === null) return { key: "", value: String(row ?? "") };
      const r = row as Record<string, unknown>;
      return { key: String(r.key ?? r.name ?? ""), value: String(r.value ?? "") };
    });
  }
  if (value && typeof value === "object") {
    return Object.entries(value as Record<string, unknown>).map(([key, v]) => ({
      key,
      value: String(v ?? ""),
    }));
  }
  return [];
}

/** Mirrors the ShowWhen rule the config drawer uses. */
export function isParamVisible(
  spec: { ShowWhen?: { Param: string; Equals: unknown[] } | null },
  params: Record<string, unknown>,
): boolean {
  const when = spec.ShowWhen;
  if (!when) return true;
  const actual = params[when.Param];
  return when.Equals.some((v) => v === actual);
}

/**
 * Tool edges count here, deliberately: the engine's own topological sort
 * includes them, so a loop through a tool edge is a loop the backend refuses
 * too. A tool-only node is never itself a cycle — nothing points back at it.
 */
export function hasCycle(graph: Graph): boolean {
  const out = new Map<string, string[]>();
  for (const e of graph.edges) {
    out.set(e.source, [...(out.get(e.source) ?? []), e.target]);
  }
  const state = new Map<string, 0 | 1 | 2>();

  const visit = (id: string): boolean => {
    const s = state.get(id);
    if (s === 1) return true;
    if (s === 2) return false;
    state.set(id, 1);
    for (const next of out.get(id) ?? []) {
      if (visit(next)) return true;
    }
    state.set(id, 2);
    return false;
  };

  return graph.nodes.some((n) => visit(n.id));
}

/** Collapses node executions into the per-node runtime the canvas paints. */
export function runtimeFromExecutions(nodeExecutions: NodeExecution[]): Record<string, NodeRuntime> {
  const out: Record<string, NodeRuntime> = {};
  for (const ne of nodeExecutions) {
    out[ne.nodeId] = {
      status: ne.status,
      itemCount: itemCount(ne),
      durationMs: ne.durationMs,
      error: ne.error,
    };
  }
  return out;
}

/**
 * How many items a node produced, across every output handle. A branching node
 * such as IF emits on "true" and "false", so reading only "main" would report
 * it as having produced nothing.
 */
export function itemCount(ne: NodeExecution): number {
  if (!ne.output) return 0;
  return Object.values(ne.output).reduce((sum, items) => sum + items.length, 0);
}

/** The node whose input a retry would resume from, if any. */
export function failedNode(nodeExecutions: NodeExecution[]): NodeExecution | undefined {
  return nodeExecutions.find((n) => n.status === "failed");
}

/** Which nodes feed a given node, by name, for the expression helper. */
export function upstreamNames(graph: Graph, nodeId: string): string[] {
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));
  const seen = new Set<string>();
  const out: string[] = [];

  const walk = (id: string) => {
    // Tool edges are skipped: a tool provider does not feed the agent, the agent
    // calls it. Treating one as upstream would make the drawer preview an
    // agent's input from a node that never ran before it.
    for (const e of graph.edges.filter((x) => x.target === id && !isToolEdge(x))) {
      if (seen.has(e.source)) continue;
      seen.add(e.source);
      const n = byId.get(e.source);
      if (n) out.push(n.name);
      walk(e.source);
    }
  };
  walk(nodeId);
  return out;
}
