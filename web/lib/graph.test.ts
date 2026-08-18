import { describe, expect, it } from "vitest";
import {
  acceptsTools,
  failedNode,
  fromFlow,
  hasCycle,
  isParamVisible,
  isToolEdge,
  isToolOnlyNode,
  keyValuePairs,
  newNodeId,
  nextPosition,
  nodeFromDescriptor,
  runtimeFromExecutions,
  toFlow,
  toolProvidersOf,
  uniqueNodeName,
  upstreamNames,
  validateGraph,
  type FlowNode,
} from "./graph";
import type { Graph, GraphNode, NodeDescriptor, NodeExecution } from "./types";

function node(id: string, name: string, type = "set"): GraphNode {
  return {
    id,
    type,
    name,
    position: { x: 0, y: 0 },
    params: {},
    settings: {
      retryOnFail: false,
      maxTries: 3,
      waitBetweenTriesMs: 1000,
      continueOnFail: false,
      timeoutMs: 60000,
    },
  };
}

const setDescriptor: NodeDescriptor = {
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

const triggerDescriptor: NodeDescriptor = {
  ...setDescriptor,
  Type: "trigger.manual",
  Name: "Manual",
  Category: "Triggers",
  Icon: "bolt",
  Mode: "once",
  Inputs: [],
  Params: [],
  IsTrigger: true,
};

/** A node with no required params, so a tool-wiring test reports only tool problems. */
const httpDescriptor: NodeDescriptor = {
  ...setDescriptor,
  Type: "http.request",
  Name: "HTTP Request",
  Icon: "globe",
  Params: [],
};

const agentDescriptor: NodeDescriptor = {
  Type: "ai.agent",
  Name: "AI Agent",
  Category: "AI",
  Description: "Runs a model that can call the nodes wired to its tool input.",
  Icon: "robot",
  Mode: "perItem",
  Inputs: [
    { Name: "main", Label: "Input" },
    { Name: "tool", Label: "Tools" },
  ],
  Outputs: [{ Name: "main", Label: "Output" }],
  Params: [{ Name: "toolDescriptions", Label: "Tool descriptions", Type: "keyValue" }],
  Credential: "anthropicApi",
  IsTrigger: false,
};

const descriptors = {
  set: setDescriptor,
  "trigger.manual": triggerDescriptor,
  "http.request": httpDescriptor,
  "ai.agent": agentDescriptor,
};

/** Trigger to agent on main, plus one node wired into the agent tool handle. */
function agentGraph(params: Record<string, unknown> = {}): Graph {
  return {
    nodes: [
      node("n1", "Start", "trigger.manual"),
      { ...node("n2", "Agent", "ai.agent"), params },
      node("n3", "Fetch profile", "http.request"),
    ],
    edges: [
      { id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" },
      { id: "e2", source: "n3", sourceHandle: "main", target: "n2", targetHandle: "tool" },
    ],
  };
}

describe("toFlow / fromFlow", () => {
  const graph: Graph = {
    nodes: [
      { ...node("n1", "Start", "trigger.manual"), position: { x: 10, y: 20 } },
      { ...node("n2", "Shape"), position: { x: 250, y: 20 } },
    ],
    edges: [{ id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" }],
  };

  it("carries the stored node into the flow node's data", () => {
    const { nodes, edges } = toFlow(graph, { descriptors });
    expect(nodes.map((n) => n.id)).toEqual(["n1", "n2"]);
    expect(nodes[0].data.node.name).toBe("Start");
    expect(nodes[0].data.descriptor?.IsTrigger).toBe(true);
    expect(edges[0].sourceHandle).toBe("main");
  });

  it("makes everything static in read-only mode", () => {
    const { nodes, edges } = toFlow(graph, { readOnly: true });
    expect(nodes.every((n) => n.draggable === false && n.connectable === false)).toBe(true);
    expect(edges.every((e) => e.deletable === false)).toBe(true);
  });

  it("labels only the branch handles, so a plain edge stays unlabelled", () => {
    const branching: Graph = {
      nodes: [node("n1", "If", "if"), node("n2", "Yes"), node("n3", "No")],
      edges: [
        { id: "e1", source: "n1", sourceHandle: "true", target: "n2", targetHandle: "main" },
        { id: "e2", source: "n1", sourceHandle: "false", target: "n3", targetHandle: "main" },
        { id: "e3", source: "n2", sourceHandle: "main", target: "n3", targetHandle: "main" },
      ],
    };
    const { edges } = toFlow(branching);
    expect(edges.map((e) => e.label)).toEqual(["true", "false", undefined]);
  });

  it("round-trips through fromFlow, rounding the dragged position", () => {
    const { nodes, edges } = toFlow(graph);
    const moved: FlowNode[] = nodes.map((n) =>
      n.id === "n2" ? { ...n, position: { x: 260.4, y: 19.6 } } : n,
    );

    const out = fromFlow(moved, edges);
    expect(out.nodes[1].position).toEqual({ x: 260, y: 20 });
    expect(out.edges).toEqual(graph.edges);
    // Everything else about the node survives the trip.
    expect(out.nodes[0]).toEqual(graph.nodes[0]);
  });

  it("paints a failed source edge as an error and a skipped target as muted", () => {
    const { edges } = toFlow(graph, {
      runtime: { n1: { status: "failed" }, n2: { status: "skipped" } },
    });
    expect(edges[0].className).toBe("is-error");

    const succeeded = toFlow(graph, {
      runtime: { n1: { status: "succeeded" }, n2: { status: "skipped" } },
    });
    expect(succeeded.edges[0].className).toBe("is-muted");
  });
});

describe("uniqueNodeName", () => {
  const graph: Graph = { nodes: [node("n1", "Fetch profile")], edges: [] };

  it("keeps a free name", () => {
    expect(uniqueNodeName(graph, "Score lead")).toBe("Score lead");
  });

  it("suffixes a taken name, because expressions address nodes by name", () => {
    expect(uniqueNodeName(graph, "Fetch profile")).toBe("Fetch profile 2");
  });

  it("keeps counting past an existing suffix", () => {
    const busier: Graph = {
      nodes: [node("n1", "Fetch profile"), node("n2", "Fetch profile 2")],
      edges: [],
    };
    expect(uniqueNodeName(busier, "Fetch profile")).toBe("Fetch profile 3");
  });
});

describe("newNodeId", () => {
  it("never collides with an id already in the graph", () => {
    const graph: Graph = { nodes: [node("n1", "A"), node("n2", "B")], edges: [] };
    const id = newNodeId(graph);
    expect(graph.nodes.some((n) => n.id === id)).toBe(false);
  });
});

describe("nextPosition", () => {
  it("starts a first node in from the corner", () => {
    expect(nextPosition({ nodes: [], edges: [] })).toEqual({ x: 80, y: 160 });
  });

  it("lands to the right of the rightmost node, on its row", () => {
    const graph: Graph = {
      nodes: [
        { ...node("n1", "A"), position: { x: 80, y: 160 } },
        { ...node("n2", "B"), position: { x: 320, y: 240 } },
      ],
      edges: [],
    };
    expect(nextPosition(graph)).toEqual({ x: 560, y: 240 });
  });
});

describe("nodeFromDescriptor", () => {
  it("fills parameter defaults and skips notices", () => {
    const withNotice: NodeDescriptor = {
      ...setDescriptor,
      Params: [
        { Name: "mode", Label: "Mode", Type: "select", Default: "merge" },
        { Name: "hint", Label: "Hint", Type: "notice", Default: "read me" },
        { Name: "fields", Label: "Fields", Type: "keyValue" },
      ],
    };
    const created = nodeFromDescriptor({ nodes: [], edges: [] }, withNotice);

    expect(created.params).toEqual({ mode: "merge" });
    expect(created.name).toBe("Set");
    expect(created.settings.timeoutMs).toBe(60000);
    expect(created.credentialId).toBeNull();
  });
});

describe("isParamVisible", () => {
  it("shows a parameter with no condition", () => {
    expect(isParamVisible({}, {})).toBe(true);
  });

  it("follows the ShowWhen condition", () => {
    const spec = { ShowWhen: { Param: "sendBody", Equals: [true] } };
    expect(isParamVisible(spec, { sendBody: true })).toBe(true);
    expect(isParamVisible(spec, { sendBody: false })).toBe(false);
    expect(isParamVisible(spec, {})).toBe(false);
  });
});

describe("hasCycle", () => {
  it("accepts a diamond", () => {
    const graph: Graph = {
      nodes: [node("n1", "A"), node("n2", "B"), node("n3", "C"), node("n4", "D")],
      edges: [
        { id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" },
        { id: "e2", source: "n1", sourceHandle: "main", target: "n3", targetHandle: "main" },
        { id: "e3", source: "n2", sourceHandle: "main", target: "n4", targetHandle: "main" },
        { id: "e4", source: "n3", sourceHandle: "main", target: "n4", targetHandle: "main" },
      ],
    };
    expect(hasCycle(graph)).toBe(false);
  });

  it("catches a two-node loop and a self loop", () => {
    const loop: Graph = {
      nodes: [node("n1", "A"), node("n2", "B")],
      edges: [
        { id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" },
        { id: "e2", source: "n2", sourceHandle: "main", target: "n1", targetHandle: "main" },
      ],
    };
    expect(hasCycle(loop)).toBe(true);

    const self: Graph = {
      nodes: [node("n1", "A")],
      edges: [{ id: "e1", source: "n1", sourceHandle: "main", target: "n1", targetHandle: "main" }],
    };
    expect(hasCycle(self)).toBe(true);
  });
});

describe("validateGraph", () => {
  it("passes a complete workflow", () => {
    const graph: Graph = {
      nodes: [
        node("n1", "Start", "trigger.manual"),
        { ...node("n2", "Shape"), params: { mode: "merge", fields: [{ key: "a", value: "b" }] } },
      ],
      edges: [{ id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" }],
    };
    expect(validateGraph(graph, descriptors)).toEqual([]);
  });

  it("says nothing about an empty canvas", () => {
    expect(validateGraph({ nodes: [], edges: [] }, descriptors)).toEqual([]);
  });

  it("reports a missing trigger in words a person can act on", () => {
    const graph: Graph = {
      nodes: [{ ...node("n1", "Shape"), params: { fields: [{}] } }],
      edges: [],
    };
    expect(validateGraph(graph, descriptors)).toContain(
      "The workflow has no trigger node, so nothing can start it.",
    );
  });

  it("reports two triggers", () => {
    const graph: Graph = {
      nodes: [node("n1", "A", "trigger.manual"), node("n2", "B", "trigger.manual")],
      edges: [],
    };
    expect(validateGraph(graph, descriptors).join(" ")).toContain("2 trigger nodes");
  });

  it("reports a required parameter left empty", () => {
    const graph: Graph = {
      nodes: [node("n1", "Start", "trigger.manual"), node("n2", "Shape")],
      edges: [{ id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" }],
    };
    expect(validateGraph(graph, descriptors)).toContain("Node “Shape” is missing Fields.");
  });

  it("does not demand a parameter its ShowWhen hides", () => {
    const gated: NodeDescriptor = {
      ...setDescriptor,
      Params: [
        { Name: "sendBody", Label: "Send body", Type: "boolean", Default: false },
        {
          Name: "body",
          Label: "Body",
          Type: "json",
          Required: true,
          ShowWhen: { Param: "sendBody", Equals: [true] },
        },
      ],
    };
    const graph: Graph = {
      nodes: [
        node("n1", "Start", "trigger.manual"),
        { ...node("n2", "Call"), params: { sendBody: false } },
      ],
      edges: [{ id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" }],
    };
    expect(validateGraph(graph, { ...descriptors, set: gated })).toEqual([]);
  });

  it("reports an unknown node type", () => {
    const graph: Graph = { nodes: [node("n1", "Mystery", "not.a.node")], edges: [] };
    expect(validateGraph(graph, descriptors).join(" ")).toContain("unknown type not.a.node");
  });
});

describe("runtimeFromExecutions", () => {
  const rows: NodeExecution[] = [
    {
      id: "ne1",
      executionId: "e1",
      nodeId: "n1",
      nodeName: "Start",
      nodeType: "trigger.manual",
      status: "succeeded",
      attempt: 1,
      output: { main: [{ json: { a: 1 } }, { json: { a: 2 } }] },
      durationMs: 4,
    },
    {
      id: "ne2",
      executionId: "e1",
      nodeId: "n2",
      nodeName: "Call",
      nodeType: "http.request",
      status: "failed",
      attempt: 3,
      error: { code: "http_error", message: "Rate limit exceeded", status: 429 },
    },
  ];

  it("keys the per-node runtime the canvas paints from", () => {
    const runtime = runtimeFromExecutions(rows);
    expect(runtime.n1).toEqual({ status: "succeeded", itemCount: 2, durationMs: 4, error: undefined });
    expect(runtime.n2.status).toBe("failed");
    expect(runtime.n2.error?.status).toBe(429);
    // A node with no main output reports zero, not undefined, so the card can
    // say "0 items" rather than showing nothing.
    expect(runtime.n2.itemCount).toBe(0);
  });

  it("finds the node a retry would resume from", () => {
    expect(failedNode(rows)?.nodeId).toBe("n2");
    expect(failedNode([rows[0]])).toBeUndefined();
  });
});

describe("upstreamNames", () => {
  it("walks the whole chain feeding a node, nearest first", () => {
    const graph: Graph = {
      nodes: [node("n1", "Start", "trigger.manual"), node("n2", "Shape"), node("n3", "Score")],
      edges: [
        { id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" },
        { id: "e2", source: "n2", sourceHandle: "main", target: "n3", targetHandle: "main" },
      ],
    };
    expect(upstreamNames(graph, "n3")).toEqual(["Shape", "Start"]);
    expect(upstreamNames(graph, "n1")).toEqual([]);
  });

  it("terminates on a cyclic graph rather than recursing forever", () => {
    const loop: Graph = {
      nodes: [node("n1", "A"), node("n2", "B")],
      edges: [
        { id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" },
        { id: "e2", source: "n2", sourceHandle: "main", target: "n1", targetHandle: "main" },
      ],
    };
    expect(upstreamNames(loop, "n2").sort()).toEqual(["A", "B"]);
  });
});

describe("tool edges", () => {
  it("recognises a tool edge by its target handle, defaulting to main", () => {
    expect(isToolEdge({ targetHandle: "tool" })).toBe(true);
    expect(isToolEdge({ targetHandle: "main" })).toBe(false);
    expect(isToolEdge({ targetHandle: "" })).toBe(false);
  });

  it("lists the nodes wired into a tool handle, once each", () => {
    const graph = agentGraph();
    graph.edges.push({
      id: "e3",
      source: "n3",
      sourceHandle: "main",
      target: "n2",
      targetHandle: "tool",
    });
    expect(toolProvidersOf(graph, "n2").map((n) => n.name)).toEqual(["Fetch profile"]);
    expect(toolProvidersOf(graph, "n1")).toEqual([]);
  });

  it("reads the tool input off the descriptor", () => {
    expect(acceptsTools(agentDescriptor)).toBe(true);
    expect(acceptsTools(setDescriptor)).toBe(false);
    expect(acceptsTools(undefined)).toBe(false);
  });

  it("calls a node tool-only when nothing feeds it and it only offers tools", () => {
    const graph = agentGraph();
    expect(isToolOnlyNode(graph, "n3")).toBe(true);
    // The agent has no outgoing edge at all, so it is a leaf, not a tool.
    expect(isToolOnlyNode(graph, "n2")).toBe(false);
    // The trigger feeds the agent data, so its edge is not a tool edge.
    expect(isToolOnlyNode(graph, "n1")).toBe(false);
  });

  it("stops calling a node tool-only once it also sits in the main flow", () => {
    const graph = agentGraph();
    graph.edges.push({
      id: "e3",
      source: "n1",
      sourceHandle: "main",
      target: "n3",
      targetHandle: "main",
    });
    expect(isToolOnlyNode(graph, "n3")).toBe(false);
  });

  it("marks and labels a tool edge so the canvas can draw it differently", () => {
    const { edges } = toFlow(agentGraph(), { descriptors });
    const tool = edges.find((e) => e.id === "e2");
    expect(tool?.className).toBe("is-tool");
    expect(tool?.label).toBe("tool");
    expect(edges.find((e) => e.id === "e1")?.label).toBeUndefined();
  });

  it("keeps run colour off a tool edge, which does not carry the run", () => {
    const { edges } = toFlow(agentGraph(), {
      descriptors,
      runtime: { n1: { status: "succeeded" }, n3: { status: "succeeded" } },
    });
    expect(edges.find((e) => e.id === "e2")?.className).toBe("is-tool");
    expect(edges.find((e) => e.id === "e1")?.className).toBe("is-success");
  });

  it("does not treat a tool provider as upstream of the agent", () => {
    expect(upstreamNames(agentGraph(), "n2")).toEqual(["Start"]);
  });
});

describe("validateGraph with tools", () => {
  const described = { toolDescriptions: [{ key: "Fetch profile", value: "Looks a customer up" }] };

  it("accepts a tool-only node: it is neither unreachable nor a second trigger", () => {
    expect(validateGraph(agentGraph(described), descriptors)).toEqual([]);
    expect(hasCycle(agentGraph(described))).toBe(false);
  });

  it("refuses a tool edge into a node that takes no tools", () => {
    const graph: Graph = {
      nodes: [
        node("n1", "Start", "trigger.manual"),
        { ...node("n2", "Shape"), params: { fields: [{ key: "a", value: "b" }] } },
        node("n3", "Fetch profile", "http.request"),
      ],
      edges: [
        { id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" },
        { id: "e2", source: "n3", sourceHandle: "main", target: "n2", targetHandle: "tool" },
      ],
    };
    expect(validateGraph(graph, descriptors)).toContain(
      "Node \u201cShape\u201d does not take tools, so the tool connection from \u201cFetch profile\u201d would be ignored.",
    );
  });

  it("asks for a description for every wired tool", () => {
    expect(validateGraph(agentGraph(), descriptors)).toContain(
      "Node \u201cAgent\u201d needs a tool description for \u201cFetch profile\u201d; " +
        "the model chooses tools by their description.",
    );
  });

  it("treats a blank description as no description", () => {
    const blank = { toolDescriptions: [{ key: "Fetch profile", value: "   " }] };
    expect(validateGraph(agentGraph(blank), descriptors).join(" ")).toContain(
      "needs a tool description",
    );
  });

  it("ignores a description whose key matches no wired tool", () => {
    const stale = {
      toolDescriptions: [
        { key: "Fetch profil", value: "typo in the node name" },
        { key: "Fetch profile", value: "Looks a customer up" },
      ],
    };
    expect(validateGraph(agentGraph(stale), descriptors)).toEqual([]);
  });

  it("says nothing about an agent with no tools wired in", () => {
    const graph: Graph = {
      nodes: [node("n1", "Start", "trigger.manual"), node("n2", "Agent", "ai.agent")],
      edges: [
        { id: "e1", source: "n1", sourceHandle: "main", target: "n2", targetHandle: "main" },
      ],
    };
    expect(validateGraph(graph, descriptors)).toEqual([]);
  });
});

describe("keyValuePairs", () => {
  it("reads the rows the editor writes", () => {
    expect(keyValuePairs([{ key: "a", value: "1" }])).toEqual([{ key: "a", value: "1" }]);
  });

  it("copes with a hand-written object and with junk", () => {
    expect(keyValuePairs({ a: "1" })).toEqual([{ key: "a", value: "1" }]);
    expect(keyValuePairs(undefined)).toEqual([]);
    expect(keyValuePairs("nonsense")).toEqual([]);
  });
});
