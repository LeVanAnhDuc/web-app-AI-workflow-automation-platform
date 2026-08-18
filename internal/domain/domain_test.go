package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGraphMarshalsEmptyListsAsArrays(t *testing.T) {
	raw, err := json.Marshal(Graph{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// A nil slice would render as null, and the editor calls .map on both lists
	// the moment a brand-new workflow loads.
	if got := string(raw); got != `{"nodes":[],"edges":[]}` {
		t.Fatalf("got %s, want empty arrays", got)
	}
}

func TestGraphRoundTripsThroughJSON(t *testing.T) {
	in := Graph{
		Nodes: []GraphNode{{
			ID:       "n1",
			Type:     "http.request",
			Name:     "Fetch profile",
			Position: Position{X: 320, Y: 160},
			Params:   map[string]any{"url": "https://api.acme.vn"},
			Settings: NodeSettings{RetryOnFail: true, MaxTries: 2, TimeoutMs: 5000},
		}},
		Edges: []Edge{{ID: "e1", Source: "n1", SourceHandle: "main", Target: "n2", TargetHandle: "main"}},
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Graph
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(out.Nodes) != 1 || out.Nodes[0].Name != "Fetch profile" {
		t.Fatalf("nodes did not survive: %+v", out.Nodes)
	}
	if out.Nodes[0].Settings.MaxTries != 2 || !out.Nodes[0].Settings.RetryOnFail {
		t.Fatalf("settings did not survive: %+v", out.Nodes[0].Settings)
	}
	if len(out.Edges) != 1 || out.Edges[0].SourceHandle != "main" {
		t.Fatalf("edges did not survive: %+v", out.Edges)
	}
}

func TestGraphLookups(t *testing.T) {
	g := Graph{
		Nodes: []GraphNode{
			{ID: "n1", Name: "Start"},
			{ID: "n2", Name: "Shape"},
			{ID: "n3", Name: "Finish"},
		},
		Edges: []Edge{
			{ID: "e1", Source: "n1", Target: "n2"},
			{ID: "e2", Source: "n2", Target: "n3"},
		},
	}

	if n, ok := g.NodeByID("n2"); !ok || n.Name != "Shape" {
		t.Fatalf("NodeByID: %+v %v", n, ok)
	}
	if _, ok := g.NodeByID("ghost"); ok {
		t.Fatal("NodeByID found a node that does not exist")
	}
	// Expressions address nodes by name, so this lookup has to work.
	if n, ok := g.NodeByName("Finish"); !ok || n.ID != "n3" {
		t.Fatalf("NodeByName: %+v %v", n, ok)
	}
	if got := g.IncomingEdges("n2"); len(got) != 1 || got[0].ID != "e1" {
		t.Fatalf("IncomingEdges: %+v", got)
	}
	if got := g.OutgoingEdges("n2"); len(got) != 1 || got[0].ID != "e2" {
		t.Fatalf("OutgoingEdges: %+v", got)
	}
	if got := g.IncomingEdges("n1"); len(got) != 0 {
		t.Fatalf("the trigger should have no incoming edges, got %+v", got)
	}
}

func TestEdgeHandlesDefaultToMain(t *testing.T) {
	e := Edge{ID: "e1", Source: "n1", Target: "n2"}
	if e.SourceHandleOrMain() != MainHandle || e.TargetHandleOrMain() != MainHandle {
		t.Fatal("an omitted handle must default to main, or a hand-written graph will not run")
	}

	branch := Edge{ID: "e2", SourceHandle: "false", TargetHandle: "input2"}
	if branch.SourceHandleOrMain() != "false" || branch.TargetHandleOrMain() != "input2" {
		t.Fatal("an explicit handle must be preserved")
	}
}

func TestNodeSettingsWithDefaults(t *testing.T) {
	got := NodeSettings{}.WithDefaults()
	if got.MaxTries != DefaultMaxTries ||
		got.WaitBetweenTriesMs != DefaultWaitBetweenTriesMs ||
		got.TimeoutMs != DefaultTimeoutMs {
		t.Fatalf("defaults not applied: %+v", got)
	}

	explicit := NodeSettings{MaxTries: 5, WaitBetweenTriesMs: 250, TimeoutMs: 1000}.WithDefaults()
	if explicit.MaxTries != 5 || explicit.WaitBetweenTriesMs != 250 || explicit.TimeoutMs != 1000 {
		t.Fatalf("explicit values overwritten: %+v", explicit)
	}
}

func TestTriesIsOneUnlessRetryIsOn(t *testing.T) {
	if got := (NodeSettings{MaxTries: 5}).Tries(); got != 1 {
		t.Fatalf("Tries %d — maxTries must not apply while retryOnFail is off", got)
	}
	if got := (NodeSettings{RetryOnFail: true, MaxTries: 3}).Tries(); got != 3 {
		t.Fatalf("Tries %d, want 3", got)
	}
	// A graph hand-edited to zero tries would otherwise never run the node.
	if got := (NodeSettings{RetryOnFail: true, MaxTries: 0}).Tries(); got != 1 {
		t.Fatalf("Tries %d, want at least 1", got)
	}
}

func TestStatusTerminal(t *testing.T) {
	for _, s := range []Status{StatusSucceeded, StatusFailed, StatusSkipped, StatusCancelled} {
		if !s.Terminal() {
			t.Fatalf("%s should be terminal", s)
		}
	}
	for _, s := range []Status{StatusQueued, StatusRunning} {
		if s.Terminal() {
			t.Fatalf("%s should not be terminal", s)
		}
	}
}

func TestItemCountSumsEveryHandle(t *testing.T) {
	// A branching node emits on "true" and "false"; counting only "main" made
	// every branch look as though it had produced nothing.
	n := NodeExecution{Output: map[string][]Item{
		"true":  {NewItem(nil), NewItem(nil)},
		"false": {NewItem(nil)},
	}}
	if got := n.ItemCount(); got != 3 {
		t.Fatalf("ItemCount %d, want 3", got)
	}
	if got := (NodeExecution{}).ItemCount(); got != 0 {
		t.Fatalf("ItemCount %d for an unrun node, want 0", got)
	}
}

func TestDurationIsNilUntilFinished(t *testing.T) {
	started := time.Date(2026, 8, 18, 14, 2, 11, 0, time.UTC)
	finished := started.Add(1420 * time.Millisecond)

	if got := (Execution{StartedAt: &started}).DurationMs(); got != nil {
		t.Fatalf("a running execution has no duration yet, got %v", *got)
	}
	got := Execution{StartedAt: &started, FinishedAt: &finished}.DurationMs()
	if got == nil || *got != 1420 {
		t.Fatalf("DurationMs %v, want 1420", got)
	}

	nodeGot := NodeExecution{StartedAt: &started, FinishedAt: &finished}.DurationMs()
	if nodeGot == nil || *nodeGot != 1420 {
		t.Fatalf("node DurationMs %v, want 1420", nodeGot)
	}
}

func TestNewItemTolerAtesNil(t *testing.T) {
	item := NewItem(nil)
	if item.JSON == nil {
		t.Fatal("an item's JSON must never be nil, or every node has to nil-check it")
	}
}

func TestAsNodeErrorPreservesAnExistingNodeError(t *testing.T) {
	original := &NodeError{Code: ErrCodeHTTP, Message: "Rate limit exceeded", Status: 429}
	if got := AsNodeError(original); got != original {
		t.Fatalf("AsNodeError should pass a NodeError through unchanged, got %+v", got)
	}

	wrapped := AsNodeError(errTest{})
	if wrapped.Code != ErrCodeInternal || wrapped.Message != "something broke" {
		t.Fatalf("unexpected conversion: %+v", wrapped)
	}
	if AsNodeError(nil) != nil {
		t.Fatal("AsNodeError(nil) must be nil")
	}
}

func TestNodeErrorMessageNamesTheNode(t *testing.T) {
	withNode := &NodeError{Code: ErrCodeHTTP, Message: "429", NodeName: "Classify ticket"}
	if got := withNode.Error(); got != `http_error at "Classify ticket": 429` {
		t.Fatalf("Error() = %q", got)
	}
	bare := &NodeError{Code: ErrCodeScript, Message: "boom"}
	if got := bare.Error(); got != "script_error: boom" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestItemsFromJSONTolerAtesNull(t *testing.T) {
	items, err := ItemsFromJSON([]byte("null"))
	if err != nil || items != nil {
		t.Fatalf("null should decode to no items, got %+v %v", items, err)
	}
	items, err = ItemsFromJSON(nil)
	if err != nil || items != nil {
		t.Fatalf("an absent column should decode to no items, got %+v %v", items, err)
	}
	items, err = ItemsFromJSON([]byte(`[{"json":{"a":1}}]`))
	if err != nil || len(items) != 1 || items[0].JSON["a"] != float64(1) {
		t.Fatalf("decode: %+v %v", items, err)
	}
}

type errTest struct{}

func (errTest) Error() string { return "something broke" }
