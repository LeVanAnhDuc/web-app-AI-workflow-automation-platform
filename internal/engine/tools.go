package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

/* ---------------------------------------------------------------------------
   Tool wiring.

   A node connected to another node's "tool" input handle is not a predecessor
   in the flow: nothing pushes items into it, and it may run zero times or many.
   The engine therefore treats a tool edge as a *capability* edge rather than a
   data edge — it is excluded from readiness and from the orphan-skip rule, and
   instead becomes a ToolBinding the consuming node can invoke on demand.
   --------------------------------------------------------------------------- */

// isToolEdge reports whether an edge grants a tool rather than carrying data.
func isToolEdge(e domain.Edge) bool {
	return e.TargetHandleOrMain() == nodes.HandleTool
}

// dataPredecessors are the edges that must have settled before a node may run.
// Tool edges are excluded: the agent runs first and calls its tools, not the
// other way round.
func dataPredecessors(g domain.Graph, nodeID string) []domain.Edge {
	incoming := g.IncomingEdges(nodeID)
	out := make([]domain.Edge, 0, len(incoming))
	for _, e := range incoming {
		if isToolEdge(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// toolProviders maps each node id to the nodes that offer it tools.
func toolProviders(g domain.Graph) map[string][]string {
	out := map[string][]string{}
	for _, e := range g.Edges {
		if !isToolEdge(e) {
			continue
		}
		out[e.Target] = append(out[e.Target], e.Source)
	}
	return out
}

// toolOnlyNodes are the nodes that exist solely to be called as tools — they
// have no incoming data edge and their only outgoing edge is a tool edge.
//
// They must not be run by the main loop, and must not be marked skipped either:
// a grey card for a tool the agent called four times would be a lie. They are
// settled by the agent's own invocations instead.
func toolOnlyNodes(g domain.Graph) map[string]bool {
	out := map[string]bool{}
	for _, n := range g.Nodes {
		outgoing := g.OutgoingEdges(n.ID)
		if len(outgoing) == 0 {
			continue
		}
		onlyTool := true
		for _, e := range outgoing {
			if !isToolEdge(e) {
				onlyTool = false
				break
			}
		}
		if !onlyTool {
			continue
		}
		if len(dataPredecessors(g, n.ID)) == 0 {
			out[n.ID] = true
		}
	}
	return out
}

// bindTools builds the invocable tools for one node.
func (r *run) bindTools(consumer domain.GraphNode) []nodes.ToolBinding {
	providerIDs := r.toolProviders[consumer.ID]
	if len(providerIDs) == 0 {
		return nil
	}

	out := make([]nodes.ToolBinding, 0, len(providerIDs))
	for _, id := range providerIDs {
		provider, ok := r.graph.NodeByID(id)
		if !ok || provider.Disabled {
			continue
		}
		impl, ok := r.e.reg.Get(provider.Type)
		if !ok {
			continue
		}

		out = append(out, nodes.ToolBinding{
			Name: provider.Name,
			// The description defaults to the node type's own, and the consuming
			// node overlays whatever its author wrote.
			Description: impl.Descriptor().Description,
			Invoke:      r.toolInvoker(provider, impl),
		})
	}
	return out
}

// toolInvoker returns the function that runs one tool node.
//
// The call goes through executeNode, so a tool gets the same retry, timeout and
// panic handling as any other node — an agent's tools are not a second-class
// execution path.
//
// The context comes from the caller and nothing else. The consuming node's
// context already descends from the run budget plus that node's own timeout, so
// it is strictly the tighter of the two: substituting the run context here would
// silently let a tool outlive the agent that asked for it, and let a cancelled
// agent keep invoking tools.
func (r *run) toolInvoker(node domain.GraphNode, impl nodes.Node) func(context.Context, map[string]any) ([]domain.Item, error) {
	desc := impl.Descriptor()

	return func(ctx context.Context, args map[string]any) ([]domain.Item, error) {
		// The model's arguments are the tool's input item, so a tool's
		// expressions read them as {{ $json.whatever }} — the same shape a node
		// in the main flow sees.
		input := []domain.Item{domain.NewItem(args)}

		// A caller that has already given up must not start more work, and a call
		// that never happened must not advance the counter.
		if err := ctx.Err(); err != nil {
			return nil, domain.Errorf(domain.ErrCodeCancelled,
				"the tool %q was not called: %s", node.Name, err.Error())
		}

		startedAt := r.e.opts.Now()
		call := r.toolCallCount[node.ID] + 1
		r.toolCallCount[node.ID] = call

		result, attempts, nerr := executeNode(ctx, nodeCall{
			impl:        impl,
			desc:        desc,
			node:        node,
			input:       input,
			inputs:      map[string][]domain.Item{domain.MainHandle: input},
			nodeOutputs: r.nodeOutputs(),
			credential:  r.bindCredential(node),
			trigger:     r.trigger,
			executionID: r.exec.ID,
			opts:        r.e.opts,
		})
		finishedAt := r.e.opts.Now()

		status := domain.StatusSucceeded
		var outputs map[string][]domain.Item
		if nerr != nil {
			status = domain.StatusFailed
		} else {
			outputs = result.Outputs
		}

		// One row per tool node, showing its latest call. node_executions is
		// unique on (execution, node), so a per-call history would need a schema
		// change; the agent's own transcript carries every call, and the row's
		// job here is to colour the card and show the last arguments.
		// Persistence uses the bookkeeping context: if this tool just timed out,
		// the execution context is the one that expired, and the row explaining
		// why is exactly what a person needs to see.
		if err := r.persistToolCall(r.persistCtx(ctx), node, status, input, outputs, nerr,
			attempts, call, startedAt, finishedAt); err != nil {
			return nil, err
		}

		if nerr != nil {
			return nil, nerr
		}
		return result.Outputs[domain.MainHandle], nil
	}
}

// persistCtx is the context to write rows with: the run's bookkeeping context
// when there is one, and otherwise the caller's, which keeps RunNodeOnce and the
// tests working without a full run around them.
func (r *run) persistCtx(fallback context.Context) context.Context {
	if r.bookkeeping != nil {
		return r.bookkeeping
	}
	return fallback
}

func (r *run) persistToolCall(ctx context.Context, node domain.GraphNode, status domain.Status,
	input []domain.Item, outputs map[string][]domain.Item, nerr *domain.NodeError,
	attempts, call int, startedAt, finishedAt time.Time) error {

	if nerr != nil {
		nerr.NodeID = node.ID
		nerr.NodeName = node.Name
		if nerr.Details == nil {
			nerr.Details = map[string]any{}
		}
		nerr.Details["toolCall"] = call
	}

	if _, err := r.e.store.UpsertNodeExecution(ctx, domain.NodeExecution{
		ExecutionID: r.exec.ID,
		NodeID:      node.ID,
		NodeName:    node.Name,
		NodeType:    node.Type,
		Status:      status,
		Attempt:     attempts,
		Input:       input,
		Output:      outputs,
		Error:       nerr,
		StartedAt:   &startedAt,
		FinishedAt:  &finishedAt,
	}); err != nil {
		return fmt.Errorf("engine: persist tool call for %s: %w", node.ID, err)
	}

	// The tool's output is recorded under both keys the engine uses — by id for
	// edges, by name for expressions — so a later node can read
	// {{ $node["Fetch profile"].json }} and see what the agent saw. Recording
	// only one of the two would make that expression silently resolve to nothing.
	//
	// A tool node that also sits in the main flow is run by the loop as well, and
	// that run overwrites this entry. That is the right precedence: the main
	// flow's own pass is the one downstream edges are waiting on.
	if status == domain.StatusSucceeded {
		r.outputs[node.ID] = outputs
		r.byName[node.Name] = outputs
	}
	return nil
}
