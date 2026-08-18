package engine

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

/* ---------------------------------------------------------------------------
   Test doubles. The engine is deliberately written against a narrow Store and
   a node Registry, so the whole runner is exercised here with no database and
   no real node implementations — the nodes package is being written in
   parallel and these tests must not depend on it.

   Assertions use the standard library only: the repository's go.sum carries no
   checksum for testify's yaml dependency.
   --------------------------------------------------------------------------- */

func mustEqual(t *testing.T, got, want any, what string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: got %#v, want %#v", what, got, want)
	}
}

func mustNoError(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", what, err)
	}
}

func mustError(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", what)
	}
}

func mustContain(t *testing.T, s, sub, what string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("%s: %q does not contain %q", what, s, sub)
	}
}

// fakeStore is an in-memory Store that records the order of every write, which
// is how the tests assert "node 2 ran after node 1" and "node 1 never ran".
type fakeStore struct {
	mu sync.Mutex

	exec    domain.Execution
	version domain.WorkflowVersion

	// rows is the latest row per node id; upserts is every call in order.
	rows    map[string]domain.NodeExecution
	upserts []domain.NodeExecution
	patches []domain.ExecutionPatch

	// cancelAt makes IsCancelRequested return true from that call onwards
	// (1-based); zero never cancels.
	cancelAt    int
	cancelCalls int

	failUpsert error
}

func newFakeStore(graph domain.Graph, trigger ...domain.Item) *fakeStore {
	return &fakeStore{
		exec: domain.Execution{
			ID:                "exec-1",
			WorkspaceID:       "ws-1",
			WorkflowID:        "wf-1",
			WorkflowVersionID: "ver-1",
			Status:            domain.StatusQueued,
			TriggerType:       domain.TriggerManual,
			TriggerData:       trigger,
			CreatedAt:         time.Unix(0, 0).UTC(),
		},
		version: domain.WorkflowVersion{ID: "ver-1", WorkflowID: "wf-1", Version: 1, Graph: graph},
		rows:    map[string]domain.NodeExecution{},
	}
}

// seedSucceeded pre-loads a completed node execution, which is what a crashed
// worker leaves behind and what resume has to honour.
func (s *fakeStore) seedSucceeded(node domain.GraphNode, out map[string][]domain.Item) {
	s.rows[node.ID] = domain.NodeExecution{
		ID:          "ne-" + node.ID,
		ExecutionID: s.exec.ID,
		NodeID:      node.ID,
		NodeName:    node.Name,
		NodeType:    node.Type,
		Status:      domain.StatusSucceeded,
		Attempt:     1,
		Output:      out,
	}
}

func (s *fakeStore) ExecutionByID(_ context.Context, id string) (domain.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.exec.ID {
		return domain.Execution{}, domain.ErrNotFound
	}
	return s.exec, nil
}

func (s *fakeStore) Version(_ context.Context, id string) (domain.WorkflowVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.version.ID {
		return domain.WorkflowVersion{}, domain.ErrNotFound
	}
	return s.version, nil
}

func (s *fakeStore) ListNodeExecutions(_ context.Context, _ string) ([]domain.NodeExecution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.NodeExecution, 0, len(s.rows))
	for _, id := range sortedKeys(s.rows) {
		out = append(out, s.rows[id])
	}
	return out, nil
}

func (s *fakeStore) UpsertNodeExecution(_ context.Context, ne domain.NodeExecution) (domain.NodeExecution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failUpsert != nil {
		return domain.NodeExecution{}, s.failUpsert
	}
	if ne.ID == "" {
		ne.ID = "ne-" + ne.NodeID
	}
	s.rows[ne.NodeID] = ne
	s.upserts = append(s.upserts, ne)
	return ne, nil
}

func (s *fakeStore) UpdateExecution(_ context.Context, id string, p domain.ExecutionPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.exec.ID {
		return domain.ErrNotFound
	}
	s.patches = append(s.patches, p)
	if p.Status != nil {
		s.exec.Status = *p.Status
	}
	if p.ClearError {
		s.exec.Error = nil
	}
	if p.Error != nil {
		s.exec.Error = p.Error
	}
	if p.StartedAt != nil {
		s.exec.StartedAt = p.StartedAt
	}
	if p.FinishedAt != nil {
		s.exec.FinishedAt = p.FinishedAt
	}
	return nil
}

func (s *fakeStore) IsCancelRequested(_ context.Context, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelCalls++
	return s.cancelAt > 0 && s.cancelCalls >= s.cancelAt, nil
}

// status reports the persisted status of one node, or "" when it never ran.
func (s *fakeStore) status(nodeID string) domain.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[nodeID].Status
}

func (s *fakeStore) row(nodeID string) domain.NodeExecution {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[nodeID]
}

func (s *fakeStore) execution() domain.Execution {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exec
}

// terminalOrder is the node ids that reached a terminal row, in write order.
func (s *fakeStore) terminalOrder() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, ne := range s.upserts {
		if ne.Status.Terminal() {
			out = append(out, ne.NodeID)
		}
	}
	return out
}

// upsertStatuses is every write as "nodeID:status", so a test can assert that
// the running row was persisted before the terminal one.
func (s *fakeStore) upsertStatuses() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.upserts))
	for _, ne := range s.upserts {
		out = append(out, ne.NodeID+":"+string(ne.Status))
	}
	return out
}

/* --------------------------------- nodes ---------------------------------- */

// fakeNode is a configurable node: the descriptor decides how the engine drives
// it, and the exec func decides what it does. It records every call so tests can
// assert on call counts and on the items each call saw.
type fakeNode struct {
	desc nodes.Descriptor
	exec func(ec nodes.ExecContext) (nodes.Result, error)

	mu    sync.Mutex
	calls int
	seen  []nodes.ExecContext
}

func (f *fakeNode) Descriptor() nodes.Descriptor { return f.desc }

func (f *fakeNode) Execute(ec nodes.ExecContext) (nodes.Result, error) {
	f.mu.Lock()
	f.calls++
	f.seen = append(f.seen, ec)
	f.mu.Unlock()
	if f.exec == nil {
		return nodes.MainSlice(ec.Items), nil // pass-through
	}
	return f.exec(ec)
}

func (f *fakeNode) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeNode) call(i int) nodes.ExecContext {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[i]
}

func desc(typ string, mode nodes.ExecMode) nodes.Descriptor {
	return nodes.Descriptor{
		Type:     typ,
		Name:     typ,
		Category: nodes.CategoryCore,
		Mode:     mode,
		Inputs:   nodes.MainIn,
		Outputs:  nodes.MainOut,
	}
}

// fakeTrigger emits the run payload, or one empty item, exactly like the real
// manual trigger.
func fakeTrigger(typ string) *fakeNode {
	d := desc(typ, nodes.ModeOnce)
	d.IsTrigger = true
	d.Inputs = nil
	return &fakeNode{desc: d, exec: func(ec nodes.ExecContext) (nodes.Result, error) {
		if len(ec.Trigger.Items) == 0 {
			return nodes.Main(domain.NewItem(nil)), nil
		}
		return nodes.MainSlice(ec.Trigger.Items), nil
	}}
}

// fakePass forwards its input untouched.
func fakePass(typ string) *fakeNode {
	return &fakeNode{desc: desc(typ, nodes.ModeOnce)}
}

// fakePerItem runs once per item and tags the item with the node type, so a
// fan-out test can see which call produced which output item.
func fakePerItem(typ string) *fakeNode {
	return &fakeNode{desc: desc(typ, nodes.ModePerItem),
		exec: func(ec nodes.ExecContext) (nodes.Result, error) {
			out := map[string]any{"by": typ, "index": ec.ItemIndex}
			for k, v := range ec.Item.JSON {
				out[k] = v
			}
			return nodes.Main(domain.NewItem(out)), nil
		}}
}

// fakeFail always fails, counting how many attempts the engine made.
func fakeFail(typ, message string) *fakeNode {
	return &fakeNode{desc: desc(typ, nodes.ModeOnce),
		exec: func(nodes.ExecContext) (nodes.Result, error) {
			return nodes.Result{}, domain.Errorf(domain.ErrCodeHTTP, "%s", message)
		}}
}

// fakeBranch routes each item to the true or false handle on the "ok" field,
// which is the shape the real IF node has.
func fakeBranch(typ string) *fakeNode {
	d := desc(typ, nodes.ModeOnce)
	d.Outputs = []nodes.Handle{{Name: nodes.HandleTrue}, {Name: nodes.HandleFalse}}
	return &fakeNode{desc: d, exec: func(ec nodes.ExecContext) (nodes.Result, error) {
		out := map[string][]domain.Item{nodes.HandleTrue: nil, nodes.HandleFalse: nil}
		for _, it := range ec.Items {
			handle := nodes.HandleFalse
			if ok, _ := it.JSON["ok"].(bool); ok {
				handle = nodes.HandleTrue
			}
			out[handle] = append(out[handle], it)
		}
		return nodes.Result{Outputs: out}, nil
	}}
}

// fakeSlow blocks until its context expires, which is how the per-node timeout
// is exercised without sleeping for real.
func fakeSlow(typ string) *fakeNode {
	return &fakeNode{desc: desc(typ, nodes.ModeOnce),
		exec: func(ec nodes.ExecContext) (nodes.Result, error) {
			<-ec.Ctx.Done()
			return nodes.Result{}, ec.Ctx.Err()
		}}
}

// fakeMerge has two input handles and emits everything it received, in the
// order the engine concatenated the handles.
func fakeMerge(typ string) *fakeNode {
	d := desc(typ, nodes.ModeOnce)
	d.Inputs = []nodes.Handle{{Name: nodes.HandleInput1}, {Name: nodes.HandleInput2}}
	return &fakeNode{desc: d}
}

/* --------------------------------- graphs --------------------------------- */

func graphNode(id, typ, name string) domain.GraphNode {
	return domain.GraphNode{ID: id, Type: typ, Name: name, Params: map[string]any{}}
}

func graphEdge(id, source, sourceHandle, target, targetHandle string) domain.Edge {
	return domain.Edge{ID: id, Source: source, SourceHandle: sourceHandle,
		Target: target, TargetHandle: targetHandle}
}

// testOptions make a suite deterministic and fast: a fake clock, no real
// sleeping, and a run budget that never expires by accident.
func testOptions() (Options, *[]time.Duration) {
	var slept []time.Duration
	tick := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	opts := Options{
		ExecutionTimeout: time.Minute,
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			tick = tick.Add(time.Millisecond)
			return tick
		},
		Sleep: func(d time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			slept = append(slept, d)
		},
	}
	return opts, &slept
}
