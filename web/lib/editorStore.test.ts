import { beforeEach, describe, expect, it } from "vitest";
import { useEditorStore } from "./editorStore";
import type { Execution, Graph, NodeDescriptor, NodeExecution } from "./types";

const manual: NodeDescriptor = {
  Type: "trigger.manual",
  Name: "Manual",
  Category: "Triggers",
  Description: "Run by hand",
  Icon: "bolt",
  Mode: "once",
  Inputs: [],
  Outputs: [{ Name: "main", Label: "Output" }],
  Params: [],
  Credential: "",
  IsTrigger: true,
};

const set: NodeDescriptor = {
  Type: "set",
  Name: "Set",
  Category: "Core",
  Description: "Shape an item",
  Icon: "table",
  Mode: "perItem",
  Inputs: [{ Name: "main", Label: "Input" }],
  Outputs: [{ Name: "main", Label: "Output" }],
  Params: [
    { Name: "mode", Label: "Mode", Type: "select", Default: "merge" },
    { Name: "fields", Label: "Fields", Type: "keyValue", Required: true },
  ],
  Credential: "",
  IsTrigger: false,
};

const store = () => useEditorStore.getState();

/** Fresh store, descriptors loaded, ready to build a graph. */
function reset(graph: Graph = { nodes: [], edges: [] }) {
  useEditorStore.setState({
    graph: { nodes: [], edges: [] },
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
  });
  store().setDescriptors([manual, set]);
  store().load({ graph, version: 1 });
}

beforeEach(() => reset());

describe("addNode", () => {
  it("creates a node from its descriptor and marks the editor dirty", () => {
    const id = store().addNode("set");
    expect(id).toBeTruthy();

    const node = store().graph.nodes[0];
    expect(node.type).toBe("set");
    expect(node.name).toBe("Set");
    expect(node.params).toEqual({ mode: "merge" });
    expect(store().dirty).toBe(true);
    // The flow cache is rebuilt in the action, so React Flow sees it at once.
    expect(store().flowNodes).toHaveLength(1);
  });

  it("keeps names unique, because expressions address nodes by name", () => {
    store().addNode("set");
    store().addNode("set");
    expect(store().graph.nodes.map((n) => n.name)).toEqual(["Set", "Set 2"]);
  });

  it("refuses an unknown type rather than storing an unrenderable node", () => {
    expect(store().addNode("nope")).toBeNull();
    expect(store().graph.nodes).toHaveLength(0);
  });
});

describe("selection", () => {
  it("survives clicking straight from one node to another", () => {
    const a = store().addNode("trigger.manual")!;
    const b = store().addNode("set")!;
    store().select(a);
    expect(store().selectedNodeId).toBe(a);

    // React Flow sends both halves of the swap in one batch. This is the case
    // that regressed: a naive last-change-wins read leaves nothing selected.
    store().setNodes([
      { type: "select", id: a, selected: false },
      { type: "select", id: b, selected: true },
    ]);
    expect(store().selectedNodeId).toBe(b);
  });

  it("survives the same batch arriving in the opposite order", () => {
    const a = store().addNode("trigger.manual")!;
    const b = store().addNode("set")!;
    store().select(a);

    store().setNodes([
      { type: "select", id: b, selected: true },
      { type: "select", id: a, selected: false },
    ]);
    expect(store().selectedNodeId).toBe(b);
  });

  it("clears when the canvas background is clicked", () => {
    const a = store().addNode("set")!;
    store().select(a);

    store().setNodes([{ type: "select", id: a, selected: false }]);
    expect(store().selectedNodeId).toBeNull();
  });

  it("clears when the selected node is removed", () => {
    const a = store().addNode("set")!;
    store().select(a);

    store().setNodes([{ type: "remove", id: a }]);
    expect(store().selectedNodeId).toBeNull();
    expect(store().graph.nodes).toHaveLength(0);
  });

  it("ignores a deselect aimed at a node that was not selected", () => {
    const a = store().addNode("trigger.manual")!;
    const b = store().addNode("set")!;
    store().select(a);

    store().setNodes([{ type: "select", id: b, selected: false }]);
    expect(store().selectedNodeId).toBe(a);
  });
});

describe("dirty tracking", () => {
  it("does not dirty the editor for a selection change alone", () => {
    const a = store().addNode("set")!;
    store().markSaved(2);
    expect(store().dirty).toBe(false);

    store().setNodes([{ type: "select", id: a, selected: true }]);
    expect(store().dirty).toBe(false);
  });

  it("dirties the editor when a node moves", () => {
    const a = store().addNode("set")!;
    store().markSaved(2);

    store().setNodes([{ type: "position", id: a, position: { x: 400, y: 200 }, dragging: false }]);
    expect(store().dirty).toBe(true);
    expect(store().graph.nodes[0].position).toEqual({ x: 400, y: 200 });
  });

  it("clears dirty and takes the new version on save", () => {
    store().addNode("set");
    store().markSaved(7);
    expect(store().dirty).toBe(false);
    expect(store().version).toBe(7);
  });
});

describe("connect", () => {
  it("adds an edge and dirties the editor", () => {
    const a = store().addNode("trigger.manual")!;
    const b = store().addNode("set")!;
    store().markSaved(2);

    store().connect({ source: a, sourceHandle: "main", target: b, targetHandle: "main" });

    expect(store().graph.edges).toHaveLength(1);
    expect(store().graph.edges[0]).toMatchObject({ source: a, target: b, sourceHandle: "main" });
    expect(store().dirty).toBe(true);
  });

  it("refuses a self-connection, which would be an instant cycle", () => {
    const a = store().addNode("set")!;
    store().connect({ source: a, sourceHandle: "main", target: a, targetHandle: "main" });
    expect(store().graph.edges).toHaveLength(0);
  });

  it("does not duplicate an edge that already exists", () => {
    const a = store().addNode("trigger.manual")!;
    const b = store().addNode("set")!;
    const conn = { source: a, sourceHandle: "main", target: b, targetHandle: "main" };

    store().connect(conn);
    store().connect(conn);
    expect(store().graph.edges).toHaveLength(1);
  });
});

describe("removing a node", () => {
  it("takes its edges with it, so the graph never keeps a dangling edge", () => {
    const a = store().addNode("trigger.manual")!;
    const b = store().addNode("set")!;
    store().connect({ source: a, sourceHandle: "main", target: b, targetHandle: "main" });
    expect(store().graph.edges).toHaveLength(1);

    store().setNodes([{ type: "remove", id: b }]);
    expect(store().graph.nodes).toHaveLength(1);
    expect(store().graph.edges).toHaveLength(0);
  });
});

describe("renameNode", () => {
  it("keeps the new name unique against the other nodes", () => {
    const a = store().addNode("trigger.manual")!;
    store().addNode("set");

    store().renameNode(a, "Set");
    expect(store().graph.nodes.find((n) => n.id === a)?.name).toBe("Set 2");
  });

  it("leaves a node alone when renamed to what it already is", () => {
    const a = store().addNode("set")!;
    store().renameNode(a, "Set");
    expect(store().graph.nodes[0].name).toBe("Set");
  });

  it("ignores an empty name rather than storing an unnamed node", () => {
    const a = store().addNode("set")!;
    store().renameNode(a, "   ");
    expect(store().graph.nodes[0].name).toBe("Set");
  });
});

describe("setParam", () => {
  it("writes one parameter and dirties the editor", () => {
    const a = store().addNode("set")!;
    store().markSaved(2);

    store().setParam(a, "mode", "keepOnly");
    expect(store().graph.nodes[0].params.mode).toBe("keepOnly");
    expect(store().dirty).toBe(true);
  });
});

describe("applyStreamEvent", () => {
  const nodeRow = (id: string, status: NodeExecution["status"]): NodeExecution => ({
    id: `ne-${id}`,
    executionId: "exec-1",
    nodeId: id,
    nodeName: "Node",
    nodeType: "set",
    status,
    attempt: 1,
    output: { main: [{ json: { a: 1 } }] },
  });

  it("paints per-node runtime as the events arrive", () => {
    const a = store().addNode("set")!;

    store().applyStreamEvent({ type: "node", node: nodeRow(a, "running") });
    expect(store().runtime[a].status).toBe("running");

    store().applyStreamEvent({ type: "node", node: nodeRow(a, "succeeded") });
    expect(store().runtime[a].status).toBe("succeeded");
    expect(store().runtime[a].itemCount).toBe(1);
    // One row per node, updated in place rather than appended.
    expect(store().nodeExecutions).toHaveLength(1);
  });

  it("tracks the execution's own status", () => {
    const execution: Execution = {
      id: "exec-1",
      workspaceId: "ws",
      workflowId: "wf",
      workflowVersionId: "v",
      status: "succeeded",
      triggerType: "manual",
      cancelRequested: false,
      createdAt: new Date().toISOString(),
    };
    store().applyStreamEvent({ type: "execution", execution });
    expect(store().execution?.status).toBe("succeeded");
  });

  it("does not dirty the editor — a run changes no graph", () => {
    const a = store().addNode("set")!;
    store().markSaved(2);

    store().applyStreamEvent({ type: "node", node: nodeRow(a, "succeeded") });
    expect(store().dirty).toBe(false);
  });
});

describe("load", () => {
  it("replaces the graph and forgets the previous run", () => {
    const a = store().addNode("set")!;
    store().applyStreamEvent({
      type: "node",
      node: {
        id: "ne",
        executionId: "exec-1",
        nodeId: a,
        nodeName: "Set",
        nodeType: "set",
        status: "succeeded",
        attempt: 1,
      },
    });

    store().load({
      graph: {
        nodes: [
          {
            id: "x1",
            type: "trigger.manual",
            name: "Start",
            position: { x: 0, y: 0 },
            params: {},
            settings: {
              retryOnFail: false,
              maxTries: 3,
              waitBetweenTriesMs: 1000,
              continueOnFail: false,
              timeoutMs: 60000,
            },
          },
        ],
        edges: [],
      },
      version: 4,
    });

    expect(store().graph.nodes.map((n) => n.id)).toEqual(["x1"]);
    expect(store().version).toBe(4);
    expect(store().dirty).toBe(false);
    expect(store().runtime).toEqual({});
    expect(store().selectedNodeId).toBeNull();
  });
});
