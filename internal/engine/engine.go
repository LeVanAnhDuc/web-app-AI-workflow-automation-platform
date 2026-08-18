// Package engine runs one execution of one workflow version: it orders the
// graph, feeds every node its input, retries what the node's settings ask for,
// and records each step so a run can be inspected, cancelled and resumed.
//
// The engine depends on a narrow Store interface rather than on the concrete
// repository, which is what makes the whole runner testable without Postgres.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/llm"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// Store is the narrow slice of persistence the engine needs. *store.Store
// satisfies it structurally — the engine never imports internal/store, so the
// dependency points inwards only.
type Store interface {
	ExecutionByID(ctx context.Context, id string) (domain.Execution, error)
	Version(ctx context.Context, id string) (domain.WorkflowVersion, error)
	ListNodeExecutions(ctx context.Context, executionID string) ([]domain.NodeExecution, error)
	UpsertNodeExecution(ctx context.Context, ne domain.NodeExecution) (domain.NodeExecution, error)
	UpdateExecution(ctx context.Context, id string, p domain.ExecutionPatch) error
	IsCancelRequested(ctx context.Context, id string) (bool, error)
}

// DefaultExecutionTimeout is the ceiling on one whole run.
const DefaultExecutionTimeout = 5 * time.Minute

// Options configures an Engine. The zero value is usable: Now and Sleep are
// injected only so tests are deterministic and fast.
type Options struct {
	HTTPClient       *http.Client
	Logger           *slog.Logger
	ExecutionTimeout time.Duration       // default 5m
	Now              func() time.Time    // default time.Now; injected so tests are deterministic
	Sleep            func(time.Duration) // default time.Sleep; injected so retry tests are fast

	// LLM is handed to the AI nodes. A nil registry is legal: those nodes then
	// report that no provider is configured, which is the honest answer for a
	// deployment with no API key.
	LLM *llm.Registry
}

func (o Options) withDefaults() Options {
	if o.HTTPClient == nil {
		o.HTTPClient = http.DefaultClient
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.ExecutionTimeout <= 0 {
		o.ExecutionTimeout = DefaultExecutionTimeout
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	return o
}

// Engine executes workflows. One Engine is shared by every worker goroutine, so
// it holds no per-run state.
type Engine struct {
	store Store
	reg   *nodes.Registry
	opts  Options
}

// New builds an Engine. Options are defaulted, so New(st, reg, Options{}) runs.
func New(st Store, reg *nodes.Registry, opts Options) *Engine {
	return &Engine{store: st, reg: reg, opts: opts.withDefaults()}
}

// Run executes one queued execution to a terminal state. It returns an error
// only for problems the queue should retry (store failures or a cancelled
// context); a workflow that fails on a node is a successful Run with a failed
// execution.
func (e *Engine) Run(ctx context.Context, executionID string) error {
	exec, err := e.store.ExecutionByID(ctx, executionID)
	if err != nil {
		return fmt.Errorf("engine: load execution %s: %w", executionID, err)
	}
	if exec.Status.Terminal() {
		// The queue delivered the same job twice; the run is already recorded.
		return nil
	}

	version, err := e.store.Version(ctx, exec.WorkflowVersionID)
	if err != nil {
		return fmt.Errorf("engine: load workflow version %s: %w", exec.WorkflowVersionID, err)
	}
	graph := version.Graph

	if verr := ValidateGraph(graph, e.reg); verr != nil {
		// An unrunnable graph is a user error, not a queue error: record it and
		// let the job succeed so it is not retried forever.
		return e.finish(ctx, exec.ID, domain.StatusFailed, domain.AsNodeError(verr))
	}
	order, err := TopologicalOrder(graph)
	if err != nil {
		return e.finish(ctx, exec.ID, domain.StatusFailed, domain.AsNodeError(err))
	}

	// A resumed run keeps its original started_at so the duration on the
	// history screen still measures the whole run.
	patch := domain.ExecutionPatch{Status: ptr(domain.StatusRunning), ClearError: true}
	if exec.StartedAt == nil {
		startedAt := e.opts.Now()
		patch.StartedAt = &startedAt
	}
	if err := e.store.UpdateExecution(ctx, exec.ID, patch); err != nil {
		return fmt.Errorf("engine: mark execution %s running: %w", exec.ID, err)
	}

	r := &run{
		e:             e,
		exec:          exec,
		graph:         graph,
		order:         order,
		outputs:       map[string]map[string][]domain.Item{},
		byName:        map[string]map[string][]domain.Item{},
		settled:       map[string]domain.Status{},
		trigger:       nodes.TriggerPayload{Type: exec.TriggerType, Items: exec.TriggerData},
		toolProviders: toolProviders(graph),
		toolOnly:      toolOnlyNodes(graph),
		toolCallCount: map[string]int{},
	}
	if err := r.rehydrate(ctx); err != nil {
		return err
	}

	// The whole run is bounded; a node's own timeout is nested inside this one.
	runCtx, cancel := context.WithTimeout(ctx, e.opts.ExecutionTimeout)
	defer cancel()
	return r.loop(ctx, runCtx)
}

// run is the mutable state of one execution. It lives outside Engine so an
// Engine can be shared between concurrent workers.
type run struct {
	e     *Engine
	exec  domain.Execution
	graph domain.Graph
	order []string

	// outputs is keyed by node id for wiring edges; byName is the same data
	// keyed by node name, which is what expressions address.
	outputs map[string]map[string][]domain.Item
	byName  map[string]map[string][]domain.Item
	settled map[string]domain.Status
	trigger nodes.TriggerPayload

	// toolProviders maps a node id to the nodes wired into its tool handle;
	// toolOnly are the nodes that exist purely to be called as tools, and
	// toolCallCount counts invocations for the persisted row.
	toolProviders map[string][]string
	toolOnly      map[string]bool
	toolCallCount map[string]int
}

// nodeOutputs is the name-keyed output map expressions resolve against.
func (r *run) nodeOutputs() map[string]map[string][]domain.Item {
	return r.byName
}

// rehydrate replays the node executions a previous attempt already completed.
// This is what makes resume cheap: a crashed run does not re-issue the HTTP
// calls it already made.
func (r *run) rehydrate(ctx context.Context) error {
	existing, err := r.e.store.ListNodeExecutions(ctx, r.exec.ID)
	if err != nil {
		return fmt.Errorf("engine: list node executions of %s: %w", r.exec.ID, err)
	}
	// A retry-from-node deletes the downstream rows before enqueueing, but the
	// engine does not trust that: anything from the resume point down is stale.
	stale := map[string]bool{}
	if r.exec.ResumeFromNode != nil {
		stale = descendants(r.graph, *r.exec.ResumeFromNode)
	}
	for _, ne := range existing {
		if ne.Status != domain.StatusSucceeded || stale[ne.NodeID] {
			continue
		}
		node, ok := r.graph.NodeByID(ne.NodeID)
		if !ok {
			continue // a row from a node that no longer exists in this version
		}
		out := ne.Output
		if out == nil {
			out = map[string][]domain.Item{}
		}
		r.settled[ne.NodeID] = ne.Status
		r.outputs[ne.NodeID] = out
		r.byName[node.Name] = out
	}
	return nil
}

// loop walks the graph until every node is settled or the run ends early. ctx
// is the caller's context (its cancellation means "shutting down, resume
// later"), runCtx adds the execution timeout.
func (r *run) loop(ctx, runCtx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			// Worker shutdown: leave the execution running so the job's next
			// attempt resumes from the rows already persisted.
			return err
		}
		if runCtx.Err() != nil {
			return r.e.finish(ctx, r.exec.ID, domain.StatusFailed, r.timeoutError(nil))
		}

		id, ok := r.next()
		if !ok {
			break
		}
		node, _ := r.graph.NodeByID(id)

		cancelled, err := r.e.store.IsCancelRequested(ctx, r.exec.ID)
		if err != nil {
			return fmt.Errorf("engine: check cancellation of %s: %w", r.exec.ID, err)
		}
		if cancelled {
			return r.e.finish(ctx, r.exec.ID, domain.StatusCancelled, nil)
		}

		goOn, err := r.step(ctx, runCtx, node)
		if err != nil {
			return err
		}
		if !goOn {
			return nil // step already wrote the terminal execution row
		}
	}
	return r.e.finish(ctx, r.exec.ID, domain.StatusSucceeded, nil)
}

// next picks the first node in topological order that is unsettled and all of
// whose predecessors are settled. Written as a ready-set pick so Phase 2 can
// replace it with a worker pool without restructuring the loop.
func (r *run) next() (string, bool) {
	for _, id := range r.order {
		if _, done := r.settled[id]; done {
			continue
		}
		// A tool-only node is never picked by the loop: the agent that owns it
		// decides whether and how often it runs.
		if r.toolOnly[id] {
			continue
		}
		ready := true
		// Tool edges are capability edges, not data edges, so they do not make
		// the consuming node wait on their source.
		for _, e := range dataPredecessors(r.graph, id) {
			if _, done := r.settled[e.Source]; !done {
				ready = false
				break
			}
		}
		if ready {
			return id, true
		}
	}
	return "", false
}

// step runs, skips or fails exactly one node. It reports false when the
// execution has reached a terminal state and the loop must stop.
func (r *run) step(ctx, runCtx context.Context, node domain.GraphNode) (bool, error) {
	impl, ok := r.e.reg.Get(node.Type)
	if !ok {
		// Validation already rejected this; belt and braces for a graph that
		// was written by a newer version of the product.
		return false, r.e.finish(ctx, r.exec.ID, domain.StatusFailed,
			domain.Errorf(domain.ErrCodeValidation, "unknown node type %q", node.Type))
	}
	desc := impl.Descriptor()
	input, inputs := r.gather(node, desc)

	isTrigger := desc.IsTrigger
	if isTrigger && len(input) == 0 {
		// The trigger's input is the payload that started the run.
		input = r.trigger.Items
		inputs = map[string][]domain.Item{domain.MainHandle: input}
	}

	switch {
	case node.Disabled:
		// A disabled node is grey in the UI but must not break the chain, so it
		// forwards whatever reached it.
		return true, r.settle(ctx, node, domain.StatusSkipped, input,
			map[string][]domain.Item{domain.MainHandle: input}, nil, 0)
	case !isTrigger && len(input) == 0:
		// Either nothing can feed this node, or the branch that would have was
		// not taken: this is how the untaken side of an IF goes grey.
		return true, r.settle(ctx, node, domain.StatusSkipped, nil, nil, nil, 0)
	}

	startedAt := r.e.opts.Now()
	if _, err := r.e.store.UpsertNodeExecution(ctx, domain.NodeExecution{
		ExecutionID: r.exec.ID,
		NodeID:      node.ID,
		NodeName:    node.Name,
		NodeType:    node.Type,
		Status:      domain.StatusRunning,
		Attempt:     1,
		Input:       input,
		StartedAt:   &startedAt,
	}); err != nil {
		return false, fmt.Errorf("engine: persist running node %s: %w", node.ID, err)
	}

	result, attempts, nerr := executeNode(runCtx, nodeCall{
		impl:        impl,
		desc:        desc,
		node:        node,
		input:       input,
		inputs:      inputs,
		nodeOutputs: r.byName,
		tools:       r.bindTools(runCtx, node),
		trigger:     r.trigger,
		executionID: r.exec.ID,
		opts:        r.e.opts,
	})
	finishedAt := r.e.opts.Now()

	if nerr != nil {
		if ctx.Err() != nil {
			// Shutdown rather than a node failure: the row stays running and
			// the next attempt re-runs this node.
			return false, ctx.Err()
		}
		nerr.NodeID, nerr.NodeName, nerr.Attempts = node.ID, node.Name, attempts

		if node.Settings.ContinueOnFail {
			// The error becomes data so the flow carries on; the row is
			// succeeded but keeps the error for the log panel.
			out := map[string][]domain.Item{domain.MainHandle: errorItems(input, nerr)}
			return true, r.settleAt(ctx, node, domain.StatusSucceeded, input, out, nerr,
				attempts, &startedAt, &finishedAt)
		}

		if err := r.settleAt(ctx, node, domain.StatusFailed, input, nil, nerr,
			attempts, &startedAt, &finishedAt); err != nil {
			return false, err
		}
		execErr := nerr
		if runCtx.Err() != nil {
			// The run budget expired, which outranks whatever the node said.
			execErr = r.timeoutError(&node)
		}
		return false, r.e.finish(ctx, r.exec.ID, domain.StatusFailed, execErr)
	}

	return true, r.settleAt(ctx, node, domain.StatusSucceeded, input, result.Outputs, nil,
		attempts, &startedAt, &finishedAt)
}

// gather builds a node's input: every incoming edge in graph order, grouped by
// target handle.
func (r *run) gather(node domain.GraphNode, desc nodes.Descriptor) ([]domain.Item, map[string][]domain.Item) {
	byHandle := map[string][]domain.Item{}
	for _, e := range dataPredecessors(r.graph, node.ID) {
		out, ok := r.outputs[e.Source]
		if !ok {
			continue
		}
		handle := e.TargetHandleOrMain()
		byHandle[handle] = append(byHandle[handle], out[e.SourceHandleOrMain()]...)
	}
	if items := byHandle[domain.MainHandle]; len(items) > 0 {
		return items, byHandle
	}
	// A multi-input node such as merge has no main handle at all, so ec.Items
	// is every input concatenated in the order the descriptor declares them.
	var items []domain.Item
	declared := map[string]bool{}
	for _, h := range desc.Inputs {
		items = append(items, byHandle[h.Name]...)
		declared[h.Name] = true
	}
	for _, h := range sortedKeys(byHandle) {
		if !declared[h] {
			items = append(items, byHandle[h]...)
		}
	}
	return items, byHandle
}

// settle persists a node's terminal row and records its output for the nodes
// downstream. Timestamps collapse to one instant for rows that never ran.
func (r *run) settle(ctx context.Context, node domain.GraphNode, status domain.Status,
	input []domain.Item, out map[string][]domain.Item, nerr *domain.NodeError, attempts int) error {
	at := r.e.opts.Now()
	return r.settleAt(ctx, node, status, input, out, nerr, attempts, &at, &at)
}

func (r *run) settleAt(ctx context.Context, node domain.GraphNode, status domain.Status,
	input []domain.Item, out map[string][]domain.Item, nerr *domain.NodeError, attempts int,
	startedAt, finishedAt *time.Time) error {
	if attempts < 1 {
		attempts = 1
	}
	if _, err := r.e.store.UpsertNodeExecution(ctx, domain.NodeExecution{
		ExecutionID: r.exec.ID,
		NodeID:      node.ID,
		NodeName:    node.Name,
		NodeType:    node.Type,
		Status:      status,
		Attempt:     attempts,
		Input:       input,
		Output:      out,
		Error:       nerr,
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
	}); err != nil {
		return fmt.Errorf("engine: persist %s node %s: %w", status, node.ID, err)
	}
	r.settled[node.ID] = status
	if out == nil {
		out = map[string][]domain.Item{}
	}
	r.outputs[node.ID] = out
	r.byName[node.Name] = out
	return nil
}

// timeoutError describes an expired execution budget, naming the node that was
// running when it expired.
func (r *run) timeoutError(node *domain.GraphNode) *domain.NodeError {
	ne := domain.Errorf(domain.ErrCodeTimeout,
		"execution timed out after %s", r.e.opts.ExecutionTimeout)
	if node != nil {
		ne.NodeID, ne.NodeName = node.ID, node.Name
	}
	return ne
}

// finish writes the terminal execution row. It returns only store errors,
// because a failed workflow is still a successful Run.
func (e *Engine) finish(ctx context.Context, id string, status domain.Status, nerr *domain.NodeError) error {
	finishedAt := e.opts.Now()
	patch := domain.ExecutionPatch{Status: &status, FinishedAt: &finishedAt}
	if nerr != nil {
		patch.Error = nerr
	} else {
		patch.ClearError = true
	}
	if err := e.store.UpdateExecution(ctx, id, patch); err != nil {
		return fmt.Errorf("engine: finish execution %s as %s: %w", id, status, err)
	}
	return nil
}

// errorItems turns a node failure into data, which is what continueOnFail
// means: one item per input item, and one when there was no input at all, so
// the error is never silently swallowed by the skip rule downstream.
func errorItems(input []domain.Item, nerr *domain.NodeError) []domain.Item {
	n := len(input)
	if n == 0 {
		n = 1
	}
	out := make([]domain.Item, 0, n)
	for range n {
		out = append(out, domain.NewItem(map[string]any{
			"error": nerr.Message,
			"code":  nerr.Code,
		}))
	}
	return out
}

func ptr[T any](v T) *T { return &v }
