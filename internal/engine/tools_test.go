package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

/* ---------------------------------------------------------------------------
   Tool wiring. A tool edge is a capability edge, not a data edge, and almost
   every bug in this area is a case of the engine forgetting that: waiting on a
   tool node's readiness, running it in the main loop, or greying it out.

   The agent here is a fake that declares a tool handle and invokes its first
   binding, so these tests need no language-model provider.
   --------------------------------------------------------------------------- */

// fakeAgentNode is the part of ai.agent the engine cares about: a node with a
// tool input handle that invokes what is wired into it. args is what the model
// would have decided to send.
func fakeAgentNode(typ string, args map[string]any) *fakeNode {
	d := desc(typ, nodes.ModeOnce)
	d.Inputs = []nodes.Handle{{Name: nodes.HandleMain}, {Name: nodes.HandleTool}}
	return &fakeNode{desc: d, exec: func(ec nodes.ExecContext) (nodes.Result, error) {
		out := map[string]any{"tools": ec.ToolNames()}
		if len(ec.Tools) > 0 {
			items, err := ec.Tools[0].Invoke(ec.Ctx, args)
			if err != nil {
				// A real agent reports a failed tool back to the model and keeps
				// going, so the fake turns the failure into data rather than
				// failing the node.
				out["toolError"] = err.Error()
			} else if len(items) > 0 {
				out["toolResult"] = items[0].JSON
			}
		}
		return nodes.Main(domain.NewItem(out)), nil
	}}
}

/* ------------------------------- unit tests -------------------------------- */

func TestIsToolEdge(t *testing.T) {
	cases := []struct {
		handle string
		want   bool
	}{
		{handle: nodes.HandleTool, want: true},
		{handle: "", want: false}, // an empty handle means main
		{handle: domain.MainHandle, want: false},
		{handle: nodes.HandleInput1, want: false},
		{handle: "Tool", want: false}, // handle names are exact, not case-folded
	}
	for _, c := range cases {
		t.Run("handle "+c.handle, func(t *testing.T) {
			got := isToolEdge(graphEdge("e1", "n1", "", "n2", c.handle))
			mustEqual(t, got, c.want, "isToolEdge")
		})
	}
}

// toolGraph is trigger -> agent, with a tool node wired into the agent's tool
// handle and nothing else touching it.
func toolGraph() domain.Graph {
	return domain.Graph{
		Nodes: []domain.GraphNode{
			graphNode("n1", "trigger.test", "Start"),
			graphNode("n2", "agent.test", "Agent"),
			graphNode("n3", "tool.echo", "Lookup"),
		},
		Edges: []domain.Edge{
			graphEdge("e1", "n1", "", "n2", ""),
			graphEdge("e2", "n3", "", "n2", nodes.HandleTool),
		},
	}
}

func TestDataPredecessors(t *testing.T) {
	// A graph with every interesting shape at once: a plain data edge, a tool
	// edge, and a node that is both a tool provider and part of the flow.
	g := domain.Graph{
		Nodes: []domain.GraphNode{
			graphNode("n1", "trigger.test", "Start"),
			graphNode("n2", "agent.test", "Agent"),
			graphNode("n3", "tool.echo", "Tool only"),
			graphNode("n4", "pass.a", "In flow and a tool"),
			graphNode("n5", "pass.b", "End"),
		},
		Edges: []domain.Edge{
			graphEdge("e1", "n1", "", "n2", ""),               // data
			graphEdge("e2", "n3", "", "n2", nodes.HandleTool), // tool
			graphEdge("e3", "n1", "", "n4", ""),               // data
			graphEdge("e4", "n4", "", "n2", nodes.HandleTool), // tool
			graphEdge("e5", "n2", "", "n5", ""),               // data
		},
	}

	cases := []struct {
		node string
		want []string
	}{
		// The agent waits on its data input only: waiting on a tool would
		// deadlock, since the tool is what the agent runs.
		{node: "n2", want: []string{"e1"}},
		{node: "n3", want: nil},
		{node: "n4", want: []string{"e3"}},
		{node: "n5", want: []string{"e5"}},
		{node: "n1", want: nil},
		{node: "missing", want: nil},
	}
	for _, c := range cases {
		t.Run(c.node, func(t *testing.T) {
			var got []string
			for _, e := range dataPredecessors(g, c.node) {
				got = append(got, e.ID)
			}
			mustEqual(t, got, c.want, "data predecessor edge ids")
		})
	}
}

func TestToolProviders(t *testing.T) {
	cases := []struct {
		name  string
		graph domain.Graph
		want  map[string][]string
	}{
		{
			name:  "no tool edges",
			graph: linearGraph("trigger.test", "pass.a"),
			want:  map[string][]string{},
		},
		{
			name:  "one provider",
			graph: toolGraph(),
			want:  map[string][]string{"n2": {"n3"}},
		},
		{
			name: "several providers keep graph order",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					graphNode("n1", "agent.test", "Agent"),
					graphNode("n2", "tool.echo", "B"),
					graphNode("n3", "tool.echo", "A"),
				},
				Edges: []domain.Edge{
					graphEdge("e1", "n2", "", "n1", nodes.HandleTool),
					graphEdge("e2", "n3", "", "n1", nodes.HandleTool),
				},
			},
			// Graph order, not id order: it is the order the author drew, which
			// is the order the model sees the tools in.
			want: map[string][]string{"n1": {"n2", "n3"}},
		},
		{
			name: "one node offers tools to two agents",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					graphNode("n1", "agent.test", "First"),
					graphNode("n2", "agent.test", "Second"),
					graphNode("n3", "tool.echo", "Shared"),
				},
				Edges: []domain.Edge{
					graphEdge("e1", "n3", "", "n1", nodes.HandleTool),
					graphEdge("e2", "n3", "", "n2", nodes.HandleTool),
				},
			},
			want: map[string][]string{"n1": {"n3"}, "n2": {"n3"}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, toolProviders(c.graph), c.want, "tool providers")
		})
	}
}

func TestToolOnlyNodes(t *testing.T) {
	cases := []struct {
		name  string
		graph domain.Graph
		want  map[string]bool
	}{
		{
			name:  "a tool provider with no other edges is tool-only",
			graph: toolGraph(),
			want:  map[string]bool{"n3": true},
		},
		{
			name: "a node that is also downstream in the flow is not tool-only",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					graphNode("n1", "trigger.test", "Start"),
					graphNode("n2", "agent.test", "Agent"),
					graphNode("n3", "pass.a", "Both"),
				},
				Edges: []domain.Edge{
					graphEdge("e1", "n1", "", "n2", ""),
					// n3 is fed by the trigger and also offered as a tool, so the
					// main loop still owns it: skipping it would drop the branch.
					graphEdge("e2", "n1", "", "n3", ""),
					graphEdge("e3", "n3", "", "n2", nodes.HandleTool),
				},
			},
			want: map[string]bool{},
		},
		{
			name: "a node with a data output as well as a tool edge is not tool-only",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					graphNode("n1", "agent.test", "Agent"),
					graphNode("n2", "tool.echo", "Tool"),
					graphNode("n3", "pass.a", "End"),
				},
				Edges: []domain.Edge{
					graphEdge("e1", "n2", "", "n1", nodes.HandleTool),
					graphEdge("e2", "n2", "", "n3", ""),
				},
			},
			want: map[string]bool{},
		},
		{
			name: "an isolated node is not tool-only, so the skip rule still greys it",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					graphNode("n1", "trigger.test", "Start"),
					graphNode("n2", "pass.a", "Orphan"),
				},
			},
			want: map[string]bool{},
		},
		{
			name: "several tool-only nodes",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					graphNode("n1", "agent.test", "Agent"),
					graphNode("n2", "tool.echo", "A"),
					graphNode("n3", "tool.echo", "B"),
				},
				Edges: []domain.Edge{
					graphEdge("e1", "n2", "", "n1", nodes.HandleTool),
					graphEdge("e2", "n3", "", "n1", nodes.HandleTool),
				},
			},
			want: map[string]bool{"n2": true, "n3": true},
		},
		{
			name: "a chain of tool nodes: only the one wired to the agent is tool-only",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					graphNode("n1", "agent.test", "Agent"),
					graphNode("n2", "pass.a", "Feeds the tool"),
					graphNode("n3", "tool.echo", "Tool"),
				},
				Edges: []domain.Edge{
					// n3 has a data predecessor, so the loop must run n2 and n3
					// normally rather than leaving the agent to trigger them.
					graphEdge("e1", "n2", "", "n3", ""),
					graphEdge("e2", "n3", "", "n1", nodes.HandleTool),
				},
			},
			want: map[string]bool{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, toolOnlyNodes(c.graph), c.want, "tool-only nodes")
		})
	}
}

/* ------------------------------ end to end -------------------------------- */

func TestRunAgentInvokesAToolNode(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	agent := fakeAgentNode("agent.test", map[string]any{"email": "ada@example.com"})
	toolNode := fakePass("tool.echo")
	reg := nodes.NewRegistry(trigger, agent, toolNode)

	g := toolGraph()
	st := newFakeStore(g, domain.NewItem(map[string]any{"ticket": 7}))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")
	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")

	// (a) The tool node ran exactly once — the agent's invocation — and not a
	// second time from the main loop. It is also not grey: a skipped card for a
	// tool the agent called would be a lie.
	mustEqual(t, toolNode.callCount(), 1, "tool node executions")
	mustEqual(t, st.status("n3"), domain.StatusSucceeded, "tool node row status")

	// (b) The agent ran and its binding produced the tool's output.
	mustEqual(t, agent.callCount(), 1, "agent executions")
	agentOut := st.row("n2").Output[domain.MainHandle]
	mustEqual(t, len(agentOut), 1, "agent output items")
	mustEqual(t, agentOut[0].JSON["tools"], []string{"Lookup"}, "the binding is named after the node")
	mustEqual(t, agentOut[0].JSON["toolResult"],
		map[string]any{"email": "ada@example.com"}, "the tool's output reached the agent")

	// (c) The tool call is persisted like any other node execution, with the
	// model's arguments as its input, so the execution log can show them.
	row := st.row("n3")
	mustEqual(t, row.NodeName, "Lookup", "tool row node name")
	mustEqual(t, row.NodeType, "tool.echo", "tool row node type")
	mustEqual(t, row.Input, []domain.Item{domain.NewItem(map[string]any{"email": "ada@example.com"})},
		"the model's arguments are the tool's input item")
	mustEqual(t, row.Attempt, 1, "tool row attempt")
	if row.StartedAt == nil || row.FinishedAt == nil {
		t.Fatal("a tool call must record its own timing")
	}

	// (d) The agent did not wait on the tool node: the tool's row is written
	// between the agent's running and terminal rows, which can only happen if the
	// agent started while the tool node was still unsettled. The tool node also
	// sorts before the agent in topological order, so a readiness check that
	// counted the tool edge would have run it first instead.
	mustEqual(t, st.upsertStatuses(), []string{
		"n1:running", "n1:succeeded",
		"n2:running", "n3:succeeded", "n2:succeeded",
	}, "the tool settles inside the agent's own execution")

	// A tool node never gets a "running" row, because the agent may call it many
	// times and the row records the latest call.
	mustEqual(t, st.row("n3").Status, domain.StatusSucceeded, "tool row is only ever terminal")
}

func TestRunAgentToolCallIsRecordedForExpressions(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	agent := fakeAgentNode("agent.test", map[string]any{"email": "ada@example.com"})
	toolNode := fakePass("tool.echo")
	// A node after the agent proves the tool's output survived for the rest of
	// the run, which is what makes {{ $node["Lookup"].json }} resolvable.
	after := fakePass("pass.after")
	reg := nodes.NewRegistry(trigger, agent, toolNode, after)

	g := toolGraph()
	g.Nodes = append(g.Nodes, graphNode("n4", "pass.after", "After"))
	g.Edges = append(g.Edges, graphEdge("e3", "n2", "", "n4", ""))

	st := newFakeStore(g, domain.NewItem(map[string]any{"ticket": 7}))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")
	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")

	seen := after.call(0).NodeOutputs["Lookup"]
	mustEqual(t, seen[domain.MainHandle],
		[]domain.Item{domain.NewItem(map[string]any{"email": "ada@example.com"})},
		"a later node can read what the agent's tool returned")
}

func TestRunAgentToolCalledSeveralTimesKeepsOneRow(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	toolNode := fakePass("tool.echo")

	// An agent that calls its tool twice, as a real one does when the first
	// answer was not enough.
	d := desc("agent.test", nodes.ModeOnce)
	d.Inputs = []nodes.Handle{{Name: nodes.HandleMain}, {Name: nodes.HandleTool}}
	agent := &fakeNode{desc: d, exec: func(ec nodes.ExecContext) (nodes.Result, error) {
		for _, args := range []map[string]any{{"email": "first"}, {"email": "second"}} {
			if _, err := ec.Tools[0].Invoke(ec.Ctx, args); err != nil {
				return nodes.Result{}, err
			}
		}
		return nodes.Main(domain.NewItem(map[string]any{"done": true})), nil
	}}
	reg := nodes.NewRegistry(trigger, agent, toolNode)

	st := newFakeStore(toolGraph(), domain.NewItem(nil))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, toolNode.callCount(), 2, "tool node executions")
	// node_executions is unique on (execution, node), so the row shows the last
	// call; the agent's own transcript is where every call is kept.
	mustEqual(t, st.row("n3").Input,
		[]domain.Item{domain.NewItem(map[string]any{"email": "second"})},
		"the row shows the most recent arguments")
}

func TestRunAgentToolFailureIsTheConsumersDecision(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	agent := fakeAgentNode("agent.test", map[string]any{"email": "ada@example.com"})
	toolNode := fakeFail("tool.echo", "the CRM is down")
	reg := nodes.NewRegistry(trigger, agent, toolNode)

	st := newFakeStore(toolGraph(), domain.NewItem(nil))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	// The failure is recorded against the tool node, so its card goes red.
	mustEqual(t, st.status("n3"), domain.StatusFailed, "tool node row status")
	row := st.row("n3")
	if row.Error == nil {
		t.Fatal("a failed tool call must persist its error")
	}
	mustEqual(t, row.Error.NodeName, "Lookup", "the error names the tool node")
	mustContain(t, row.Error.Message, "the CRM is down", "tool error message")
	// The call number is on the error so a transcript entry can be matched to it.
	mustEqual(t, row.Error.Details["toolCall"], 1, "toolCall detail")

	// The consuming node decides what a failed tool means. This fake carries on,
	// so the run succeeds even though a node's row is red.
	mustEqual(t, st.status("n2"), domain.StatusSucceeded, "agent row status")
	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")
	agentOut := st.row("n2").Output[domain.MainHandle]
	mustEqual(t, len(agentOut), 1, "agent output items")
	mustContain(t, agentOut[0].JSON["toolError"].(string), "the CRM is down", "error handed to the agent")
}

func TestRunAgentToolFailureCanAlsoFailTheConsumer(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	toolNode := fakeFail("tool.echo", "the CRM is down")

	// The other reasonable choice: an agent that treats a broken tool as fatal.
	d := desc("agent.test", nodes.ModeOnce)
	d.Inputs = []nodes.Handle{{Name: nodes.HandleMain}, {Name: nodes.HandleTool}}
	agent := &fakeNode{desc: d, exec: func(ec nodes.ExecContext) (nodes.Result, error) {
		_, err := ec.Tools[0].Invoke(ec.Ctx, map[string]any{})
		return nodes.Result{}, err
	}}
	reg := nodes.NewRegistry(trigger, agent, toolNode)

	st := newFakeStore(toolGraph(), domain.NewItem(nil))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.status("n3"), domain.StatusFailed, "tool node row status")
	mustEqual(t, st.status("n2"), domain.StatusFailed, "agent row status")
	mustEqual(t, st.execution().Status, domain.StatusFailed, "execution status")
}

func TestRunDisabledToolIsNotBound(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	agent := fakeAgentNode("agent.test", map[string]any{"email": "ada@example.com"})
	toolNode := fakePass("tool.echo")
	reg := nodes.NewRegistry(trigger, agent, toolNode)

	g := toolGraph()
	g.Nodes[2].Disabled = true

	st := newFakeStore(g, domain.NewItem(nil))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	// A disabled tool must not be offered at all: an agent told about a tool it
	// cannot call would keep trying it.
	mustEqual(t, toolNode.callCount(), 0, "tool node executions")
	agentOut := st.row("n2").Output[domain.MainHandle]
	mustEqual(t, agentOut[0].JSON["tools"], []string{}, "no bindings")
	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")
}

func TestRunNodeWithNoToolEdgesGetsNoBindings(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	agent := fakeAgentNode("agent.test", nil)
	reg := nodes.NewRegistry(trigger, agent)

	g := linearGraph("trigger.test", "agent.test")
	st := newFakeStore(g, domain.NewItem(nil))
	opts, _ := testOptions()

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	// nil rather than an empty slice is what bindTools returns, and an agent with
	// no tools is legal.
	if got := agent.call(0).Tools; got != nil {
		t.Fatalf("tools: got %#v, want nil", got)
	}
	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")
}

/* ------------------------------- validation ------------------------------- */

func TestValidateGraphToolEdgeIntoANodeWithNoToolHandle(t *testing.T) {
	reg := nodes.NewRegistry(fakeTrigger("trigger.test"), fakePass("pass.a"), fakePass("tool.echo"))
	g := domain.Graph{
		Nodes: []domain.GraphNode{
			graphNode("n1", "trigger.test", "Start"),
			graphNode("n2", "pass.a", "Plain node"),
			graphNode("n3", "tool.echo", "Lookup"),
		},
		Edges: []domain.Edge{
			graphEdge("e1", "n1", "", "n2", ""),
			graphEdge("e2", "n3", "", "n2", nodes.HandleTool),
		},
	}

	err := ValidateGraph(g, reg)
	mustError(t, err, "validate")
	// Both ends are named, because from the canvas the author cannot tell which
	// of the two nodes is the problem.
	mustContain(t, err.Error(), "Plain node", "validation message")
	mustContain(t, err.Error(), "Lookup", "validation message")
	mustEqual(t, domain.AsNodeError(err).Code, domain.ErrCodeValidation, "error code")
}

func TestValidateGraphToolEdgeIntoTheRealAgentIsFine(t *testing.T) {
	// The real ai.agent is used here rather than a fake: acceptsTools reads its
	// descriptor, and that is exactly what this asserts.
	reg := nodes.NewRegistry(fakeTrigger("trigger.test"), nodes.AIAgent{}, nodes.LLMChat{}, fakePass("tool.echo"))
	g := domain.Graph{
		Nodes: []domain.GraphNode{
			graphNode("n1", "trigger.test", "Start"),
			graphNode("n2", "ai.agent", "Agent"),
			graphNode("n3", "tool.echo", "Lookup"),
		},
		Edges: []domain.Edge{
			graphEdge("e1", "n1", "", "n2", ""),
			graphEdge("e2", "n3", "", "n2", nodes.HandleTool),
		},
	}

	mustNoError(t, ValidateGraph(g, reg), "validate")

	// The chat node has no tool handle, so the same wiring into it is refused.
	g.Nodes[1] = graphNode("n2", "llm.chat", "Chat")
	err := ValidateGraph(g, reg)
	mustError(t, err, "validate llm.chat")
	mustContain(t, err.Error(), "does not take tools", "validation message")
}

func TestValidateGraphToolEdgeToAnUnknownTargetReportsOnlyTheMissingNode(t *testing.T) {
	reg := nodes.NewRegistry(fakeTrigger("trigger.test"), fakePass("tool.echo"))
	g := domain.Graph{
		Nodes: []domain.GraphNode{
			graphNode("n1", "trigger.test", "Start"),
			graphNode("n2", "tool.echo", "Lookup"),
		},
		Edges: []domain.Edge{graphEdge("e1", "n2", "", "missing", nodes.HandleTool)},
	}

	err := ValidateGraph(g, reg)
	mustError(t, err, "validate")
	mustContain(t, err.Error(), `unknown node "missing"`, "validation message")
	// The tool-handle check must not also fire on a target that does not exist,
	// or one typo would produce two confusing problems.
	if strings.Contains(err.Error(), "does not take tools") {
		t.Fatalf("validation reported a second, misleading problem: %s", err.Error())
	}
}

// A tool call must still honour the context its caller passed in. The agent
// derives that context from its own node timeout, so discarding it lets a tool
// call outlive the deadline the author configured for the agent — and a cancelled
// agent keeps issuing tool calls that nobody is waiting for.
func TestRunAgentToolCallHonoursTheCallersContext(t *testing.T) {
	trigger := fakeTrigger("trigger.test")
	toolNode := fakePass("tool.echo")

	d := desc("agent.test", nodes.ModeOnce)
	d.Inputs = []nodes.Handle{{Name: nodes.HandleMain}, {Name: nodes.HandleTool}}
	var invokeErr error
	agent := &fakeNode{desc: d, exec: func(ec nodes.ExecContext) (nodes.Result, error) {
		// Stand-in for the agent's context having expired: the binding is handed a
		// context that is already done.
		ctx, cancel := context.WithCancel(ec.Ctx)
		cancel()
		_, invokeErr = ec.Tools[0].Invoke(ctx, map[string]any{"email": "ada@example.com"})
		return nodes.Main(domain.NewItem(map[string]any{"ok": true})), nil
	}}
	reg := nodes.NewRegistry(trigger, agent, toolNode)

	st := newFakeStore(toolGraph(), domain.NewItem(nil))
	opts, _ := testOptions()
	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustError(t, invokeErr, "invoking a tool with a cancelled context")
	mustEqual(t, toolNode.callCount(), 0, "tool node executions")
}
