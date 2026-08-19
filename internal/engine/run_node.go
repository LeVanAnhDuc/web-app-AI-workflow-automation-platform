package engine

import (
	"context"
	"errors"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/expr"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// nodeCall is everything one node invocation needs. Both Run and RunNodeOnce
// build one, which is how the retry, timeout and per-item machinery is shared
// between a real execution and the node test endpoint.
type nodeCall struct {
	impl        nodes.Node
	desc        nodes.Descriptor
	node        domain.GraphNode
	input       []domain.Item
	inputs      map[string][]domain.Item
	nodeOutputs map[string]map[string][]domain.Item
	trigger     nodes.TriggerPayload
	executionID string
	tools       []nodes.ToolBinding
	credential  nodes.CredentialResolver
	opts        Options
}

// RunNodeOnce executes a single node against supplied input, for the
// POST /nodes/{type}/test endpoint. It persists nothing.
func RunNodeOnce(ctx context.Context, reg *nodes.Registry, node domain.GraphNode, input []domain.Item, opts Options) (nodes.Result, error) {
	impl, ok := reg.Get(node.Type)
	if !ok {
		return nodes.Result{}, domain.Errorf(domain.ErrCodeValidation,
			"unknown node type %q", node.Type)
	}
	desc := impl.Descriptor()
	result, _, nerr := executeNode(ctx, nodeCall{
		impl:   impl,
		desc:   desc,
		node:   node,
		input:  input,
		inputs: map[string][]domain.Item{domain.MainHandle: input},
		// A test run has no upstream nodes and no execution row, so $node[…]
		// is empty and the trigger payload is simply the supplied input.
		nodeOutputs: map[string]map[string][]domain.Item{},
		trigger:     nodes.TriggerPayload{Type: domain.TriggerManual, Items: input},
		opts:        opts.withDefaults(),
	})
	if nerr != nil {
		nerr.NodeID, nerr.NodeName = node.ID, node.Name
		return nodes.Result{}, nerr
	}
	return result, nil
}

// executeNode makes the attempts the node's settings ask for and reports the
// attempt number it reached, which the execution log shows as "attempt 3 of 3".
func executeNode(ctx context.Context, c nodeCall) (nodes.Result, int, *domain.NodeError) {
	settings := c.node.Settings.WithDefaults()
	tries := settings.Tries()
	timeout := time.Duration(settings.TimeoutMs) * time.Millisecond
	wait := time.Duration(settings.WaitBetweenTriesMs) * time.Millisecond

	var last *domain.NodeError
	for attempt := 1; attempt <= tries; attempt++ {
		result, nerr := attemptNode(ctx, c, timeout)
		if nerr == nil {
			return result, attempt, nil
		}
		last = nerr
		if ctx.Err() != nil {
			// The run budget is gone or the worker is stopping: retrying would
			// only burn the remaining attempts against a dead context.
			return nodes.Result{}, attempt, last
		}
		if attempt < tries {
			c.opts.Sleep(wait)
		}
	}
	return nodes.Result{}, tries, last
}

// attemptNode is one attempt: bounded by the node's own timeout, with panics
// recovered so a buggy node fails its execution instead of the worker process.
func attemptNode(ctx context.Context, c nodeCall, timeout time.Duration) (result nodes.Result, nerr *domain.NodeError) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	defer func() {
		if p := recover(); p != nil {
			nerr = domain.Errorf(domain.ErrCodeInternal, "node panicked: %v", p)
		}
	}()

	if c.desc.Mode == nodes.ModePerItem {
		result, nerr = runPerItem(callCtx, c)
	} else {
		result, nerr = runOnce(callCtx, c)
	}
	if nerr != nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		// Whatever the node reported, the reason it stopped was the clock.
		nerr = domain.Errorf(domain.ErrCodeTimeout, "node timed out after %s", timeout)
	}
	return result, nerr
}

// runOnce hands the node the whole input set at once. Its expressions still
// need a current item, which is item 0 by convention.
func runOnce(ctx context.Context, c nodeCall) (nodes.Result, *domain.NodeError) {
	item := domain.NewItem(nil)
	if len(c.input) > 0 {
		item = c.input[0]
	}
	result, err := c.impl.Execute(c.execContext(ctx, item, 0))
	if err != nil {
		return nodes.Result{}, domain.AsNodeError(err)
	}
	return withMainHandle(result), nil
}

// runPerItem calls the node once per input item and concatenates the calls per
// output handle. Per handle rather than per call is what lets an IF node route
// each item independently and still produce two coherent outputs.
func runPerItem(ctx context.Context, c nodeCall) (nodes.Result, *domain.NodeError) {
	merged := map[string][]domain.Item{domain.MainHandle: nil}
	for i, item := range c.input {
		if err := ctx.Err(); err != nil {
			return nodes.Result{}, domain.AsNodeError(err)
		}
		result, err := c.impl.Execute(c.execContext(ctx, item, i))
		if err != nil {
			return nodes.Result{}, domain.AsNodeError(err)
		}
		for _, handle := range sortedKeys(result.Outputs) {
			merged[handle] = append(merged[handle], result.Outputs[handle]...)
		}
	}
	// No input means no calls and an empty output, which skips everything
	// downstream rather than inventing an item.
	return nodes.Result{Outputs: merged}, nil
}

// execContext builds the ExecContext for one call, including the parameter
// resolver bound to this item so {{ $json.x }} means the right thing.
func (c nodeCall) execContext(ctx context.Context, item domain.Item, index int) nodes.ExecContext {
	env := expr.Env{
		JSON:        item.JSON,
		Items:       c.input,
		Nodes:       c.nodeOutputs,
		ItemIndex:   index,
		Now:         c.opts.Now(),
		ExecutionID: c.executionID,
	}
	return nodes.ExecContext{
		Ctx:         ctx,
		Node:        c.node,
		Params:      NewParamResolver(c.desc.Params, c.node.Params, env),
		Items:       c.input,
		Inputs:      c.inputs,
		Item:        item,
		ItemIndex:   index,
		NodeOutputs: c.nodeOutputs,
		Tools:       c.tools,
		Credential:  c.credential,
		LLM:         c.opts.LLM,
		Trigger:     c.trigger,
		ExecutionID: c.executionID,
		HTTPClient:  c.opts.HTTPClient,
		Logger:      c.opts.Logger,
	}
}

// withMainHandle normalises a node that returned a zero Result, so the rest of
// the engine can index Outputs without a nil check.
func withMainHandle(r nodes.Result) nodes.Result {
	if r.Outputs == nil {
		return nodes.Result{Outputs: map[string][]domain.Item{domain.MainHandle: nil}}
	}
	return r
}
