// Package integration exercises the whole Phase 1 stack against a real
// Postgres: the HTTP API records and queues an execution, the engine claims it
// through the real queue and runs real nodes, and the API then serves the log
// the UI reads. Every layer below is unit-tested in isolation; this is the test
// that would have caught a mismatch between them.
//
// It skips unless TEST_DATABASE_URL is set:
//
//	TEST_DATABASE_URL=postgres://flowgrid:flowgrid@127.0.0.1:6543/flowgrid?sslmode=disable go test ./internal/integration/...
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/db"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/api"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/auth"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/config"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/engine"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/queue"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/store"
)

type stack struct {
	t      *testing.T
	store  *store.Store
	queue  *queue.Queue
	engine *engine.Engine
	server *httptest.Server
	client *http.Client
	email  string

	// own records the executions this test created. The jobs table is shared
	// with every other suite that runs against the same scratch database, so
	// draining has to ignore rows it did not put there.
	own map[string]bool
}

func newStack(t *testing.T) *stack {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run the integration suite")
	}

	ctx := t.Context()
	if err := store.Migrate(ctx, dsn, db.Migrations, db.MigrationsDir); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	st, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(st.Close)

	// A unique account per test keeps parallel runs and repeated runs from
	// tripping over each other's rows.
	email := fmt.Sprintf("integration+%d@acme.vn", time.Now().UnixNano())
	hash, err := auth.HashPassword("flowgrid123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, _, err := st.EnsureSeed(ctx, "Integration", email, hash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	signer, err := auth.NewSigner([]byte("integration-secret"))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	q := queue.New(st.Pool(), "integration-test")
	reg := nodes.Default()
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	router := api.NewRouter(api.Deps{
		Store:    st,
		Queue:    q,
		Registry: reg,
		Signer:   signer,
		Config:   config.Config{PublicBaseURL: "http://localhost:3000"},
		Logger:   discard,
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	eng := engine.New(st, reg, engine.Options{
		Logger:           discard,
		ExecutionTimeout: 30 * time.Second,
	})

	s := &stack{
		t:      t,
		store:  st,
		queue:  q,
		engine: eng,
		server: srv,
		client: srv.Client(),
		email:  email,
		own:    map[string]bool{},
	}
	// A cookie jar means the tests authenticate the way the browser does,
	// through the session cookie the login handler sets.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	s.client.Jar = jar
	s.login()
	return s
}

func (s *stack) login() {
	s.t.Helper()
	var out struct {
		User struct{ ID string } `json:"user"`
	}
	s.request(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": s.email, "password": "flowgrid123", "remember": true,
	}, http.StatusOK, &out)
	if out.User.ID == "" {
		s.t.Fatal("login returned no user id")
	}
}

func (s *stack) request(method, path string, body any, wantStatus int, out any) {
	s.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			s.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(s.t.Context(), method, s.server.URL+path, reader)
	if err != nil {
		s.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := s.client.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		s.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, res.StatusCode, wantStatus, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			s.t.Fatalf("decode %s %s: %v; body %s", method, path, err, raw)
		}
	}
}

// drainQueue claims and runs every queued job the way cmd/worker does, so the
// test exercises the real enqueue/claim path rather than calling the engine
// straight from the handler's return value.
//
// A real worker may be running against the same scratch database and claim a
// job first. That is fine: settle returns whatever ran it, so the assertions
// hold either way.
func (s *stack) drainQueue() {
	s.t.Helper()

	for range 10 {
		jobs, err := s.queue.Claim(s.t.Context(), 5, queue.KindExecution)
		if err != nil {
			s.t.Fatalf("claim: %v", err)
		}
		if len(jobs) == 0 {
			return
		}
		for _, job := range jobs {
			var payload queue.ExecutionPayload
			if err := json.Unmarshal(job.Payload, &payload); err != nil {
				s.t.Fatalf("decode job payload: %v", err)
			}
			if !s.own[payload.ExecutionID] {
				// A leftover from another suite. Settle it so the claim loop
				// makes progress instead of spinning on foreign work.
				if err := s.queue.Complete(s.t.Context(), job.ID); err != nil {
					s.t.Fatalf("complete foreign job: %v", err)
				}
				continue
			}
			if err := s.engine.Run(s.t.Context(), payload.ExecutionID); err != nil {
				s.t.Fatalf("engine run %s: %v", payload.ExecutionID, err)
			}
			if err := s.queue.Complete(s.t.Context(), job.ID); err != nil {
				s.t.Fatalf("complete job: %v", err)
			}
		}
	}
	s.t.Fatal("queue did not drain within ten rounds")
}

// settle drains the queue and then waits for the execution to reach a terminal
// state, so the test does not care whether this process or a separately running
// worker did the work.
func (s *stack) settle(executionID string) executionDetail {
	s.t.Helper()
	s.drainQueue()

	deadline := time.Now().Add(20 * time.Second)
	for {
		got := s.executionDetail(executionID)
		if got.Execution.Status.Terminal() {
			return got
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("execution %s is still %s after 20s", executionID, got.Execution.Status)
		}
		time.Sleep(100 * time.Millisecond)
		s.drainQueue()
	}
}

type executionDetail struct {
	Execution      domain.Execution       `json:"execution"`
	NodeExecutions []domain.NodeExecution `json:"nodeExecutions"`
	Graph          domain.Graph           `json:"graph"`
}

func (s *stack) executionDetail(id string) executionDetail {
	s.t.Helper()
	var out executionDetail
	s.request(http.MethodGet, "/api/v1/executions/"+id, nil, http.StatusOK, &out)
	return out
}

func (s *stack) createWorkflow(name string, graph domain.Graph) string {
	s.t.Helper()

	var created struct {
		Workflow domain.Workflow `json:"workflow"`
	}
	s.request(http.MethodPost, "/api/v1/workflows", map[string]any{"name": name}, http.StatusCreated, &created)

	s.request(http.MethodPost, "/api/v1/workflows/"+created.Workflow.ID+"/versions",
		map[string]any{"graph": graph}, http.StatusCreated, nil)

	return created.Workflow.ID
}

func (s *stack) run(workflowID string, data any) string {
	s.t.Helper()
	var started struct {
		Execution domain.Execution `json:"execution"`
	}
	s.request(http.MethodPost, "/api/v1/workflows/"+workflowID+"/run",
		map[string]any{"data": data}, http.StatusAccepted, &started)
	s.own[started.Execution.ID] = true
	return started.Execution.ID
}

func nodeByName(t *testing.T, rows []domain.NodeExecution, name string) domain.NodeExecution {
	t.Helper()
	for _, r := range rows {
		if r.NodeName == name {
			return r
		}
	}
	t.Fatalf("no node execution named %q in %d rows", name, len(rows))
	return domain.NodeExecution{}
}

// --- the tests --------------------------------------------------------------

func TestManualRunFlowsDataThroughEveryNodeType(t *testing.T) {
	s := newStack(t)

	graph := domain.Graph{
		Nodes: []domain.GraphNode{
			{ID: "n1", Type: "trigger.manual", Name: "Start", Params: map[string]any{}},
			{ID: "n2", Type: "set", Name: "Normalise", Params: map[string]any{
				"mode": "merge",
				"fields": []any{
					map[string]any{"key": "domain", "value": "{{ $json.company }}"},
					map[string]any{"key": "tier", "value": "enterprise"},
				},
			}},
			{ID: "n3", Type: "code", Name: "Score", Params: map[string]any{
				"mode":   "allItems",
				"jsCode": "return items.map(i => ({ json: { ...i.json, score: i.json.tier === 'enterprise' ? 90 : 10 } }));",
			}},
			{ID: "n4", Type: "if", Name: "High value?", Params: map[string]any{
				"combinator": "all",
				"conditions": []any{
					map[string]any{"left": "{{ $json.score }}", "operator": "gte", "right": 50},
				},
			}},
			{ID: "n5", Type: "set", Name: "Tag hot", Params: map[string]any{
				"mode":   "merge",
				"fields": []any{map[string]any{"key": "queue", "value": "hot"}},
			}},
			{ID: "n6", Type: "set", Name: "Tag cold", Params: map[string]any{
				"mode":   "merge",
				"fields": []any{map[string]any{"key": "queue", "value": "cold"}},
			}},
		},
		Edges: []domain.Edge{
			{ID: "e1", Source: "n1", SourceHandle: "main", Target: "n2", TargetHandle: "main"},
			{ID: "e2", Source: "n2", SourceHandle: "main", Target: "n3", TargetHandle: "main"},
			{ID: "e3", Source: "n3", SourceHandle: "main", Target: "n4", TargetHandle: "main"},
			{ID: "e4", Source: "n4", SourceHandle: "true", Target: "n5", TargetHandle: "main"},
			{ID: "e5", Source: "n4", SourceHandle: "false", Target: "n6", TargetHandle: "main"},
		},
	}

	workflowID := s.createWorkflow("Lead enrichment", graph)
	execID := s.run(workflowID, map[string]any{"email": "ha@acme.vn", "company": "acme.vn"})

	got := s.settle(execID)
	if got.Execution.Status != domain.StatusSucceeded {
		t.Fatalf("status %q, error %+v", got.Execution.Status, got.Execution.Error)
	}
	if len(got.NodeExecutions) != 6 {
		t.Fatalf("recorded %d node executions, want 6", len(got.NodeExecutions))
	}

	// The expression resolved against the trigger item.
	normalise := nodeByName(t, got.NodeExecutions, "Normalise")
	if v := normalise.Output[domain.MainHandle][0].JSON["domain"]; v != "acme.vn" {
		t.Fatalf("{{ $json.company }} resolved to %v, want acme.vn", v)
	}

	// The Code node ran under goja and its numeric result survived JSONB.
	score := nodeByName(t, got.NodeExecutions, "Score")
	if v := score.Output[domain.MainHandle][0].JSON["score"]; fmt.Sprint(v) != "90" {
		t.Fatalf("score is %v (%T), want 90", v, v)
	}

	// The taken branch ran; the other is recorded as skipped, which is what
	// paints it grey on the replay canvas.
	if hot := nodeByName(t, got.NodeExecutions, "Tag hot"); hot.Status != domain.StatusSucceeded {
		t.Fatalf("Tag hot status %q, want succeeded", hot.Status)
	}
	if cold := nodeByName(t, got.NodeExecutions, "Tag cold"); cold.Status != domain.StatusSkipped {
		t.Fatalf("Tag cold status %q, want skipped", cold.Status)
	}

	// The replay graph is the version that ran, not the workflow's draft.
	if len(got.Graph.Nodes) != len(graph.Nodes) {
		t.Fatalf("replay graph has %d nodes, want %d", len(got.Graph.Nodes), len(graph.Nodes))
	}
}

func TestPerItemNodeFansOutOverItems(t *testing.T) {
	s := newStack(t)

	graph := domain.Graph{
		Nodes: []domain.GraphNode{
			{ID: "n1", Type: "trigger.manual", Name: "Start", Params: map[string]any{}},
			{ID: "n2", Type: "set", Name: "Label", Params: map[string]any{
				"mode":   "merge",
				"fields": []any{map[string]any{"key": "label", "value": "row {{ $itemIndex }}"}},
			}},
		},
		Edges: []domain.Edge{{ID: "e1", Source: "n1", SourceHandle: "main", Target: "n2", TargetHandle: "main"}},
	}

	workflowID := s.createWorkflow("Fan out", graph)
	execID := s.run(workflowID, []any{
		map[string]any{"id": 1},
		map[string]any{"id": 2},
		map[string]any{"id": 3},
	})

	got := s.settle(execID)
	if got.Execution.Status != domain.StatusSucceeded {
		t.Fatalf("status %q, error %+v", got.Execution.Status, got.Execution.Error)
	}

	label := nodeByName(t, got.NodeExecutions, "Label")
	out := label.Output[domain.MainHandle]
	if len(out) != 3 {
		t.Fatalf("got %d output items, want one per input item", len(out))
	}
	// $itemIndex proves each item was resolved against its own scope.
	for i, item := range out {
		want := fmt.Sprintf("row %d", i)
		if item.JSON["label"] != want {
			t.Fatalf("item %d label %v, want %q", i, item.JSON["label"], want)
		}
	}
}

func TestFailedRunRecordsTheNodeAndRetryResumesFromIt(t *testing.T) {
	s := newStack(t)

	// A URL that cannot resolve fails the HTTP node without needing a stub
	// server that the engine's own client would have to be pointed at.
	graph := domain.Graph{
		Nodes: []domain.GraphNode{
			{ID: "n1", Type: "trigger.manual", Name: "Start", Params: map[string]any{}},
			{ID: "n2", Type: "set", Name: "Prepare", Params: map[string]any{
				"mode":   "merge",
				"fields": []any{map[string]any{"key": "ready", "value": "yes"}},
			}},
			{ID: "n3", Type: "http.request", Name: "Call API", Params: map[string]any{
				"method": "GET",
				"url":    "http://127.0.0.1:1/nowhere",
			}},
			{ID: "n4", Type: "set", Name: "After", Params: map[string]any{
				"mode":   "merge",
				"fields": []any{map[string]any{"key": "done", "value": "yes"}},
			}},
		},
		Edges: []domain.Edge{
			{ID: "e1", Source: "n1", SourceHandle: "main", Target: "n2", TargetHandle: "main"},
			{ID: "e2", Source: "n2", SourceHandle: "main", Target: "n3", TargetHandle: "main"},
			{ID: "e3", Source: "n3", SourceHandle: "main", Target: "n4", TargetHandle: "main"},
		},
	}

	workflowID := s.createWorkflow("Flaky call", graph)
	execID := s.run(workflowID, map[string]any{"id": "TCK-1"})
	failed := s.settle(execID)
	if failed.Execution.Status != domain.StatusFailed {
		t.Fatalf("status %q, want failed", failed.Execution.Status)
	}
	if failed.Execution.Error == nil || failed.Execution.Error.NodeName != "Call API" {
		t.Fatalf("the execution error must name the failing node, got %+v", failed.Execution.Error)
	}
	if got := nodeByName(t, failed.NodeExecutions, "Call API"); got.Status != domain.StatusFailed {
		t.Fatalf("Call API status %q, want failed", got.Status)
	}
	// The node after the failure was never attempted.
	for _, r := range failed.NodeExecutions {
		if r.NodeName == "After" && r.Status == domain.StatusSucceeded {
			t.Fatal("a node downstream of the failure must not have run")
		}
	}

	// Retrying starts a new execution that already knows the earlier results.
	var retried struct {
		Execution domain.Execution `json:"execution"`
	}
	s.request(http.MethodPost, "/api/v1/executions/"+execID+"/retry", nil, http.StatusAccepted, &retried)
	s.own[retried.Execution.ID] = true

	if retried.Execution.ResumeFromNode == nil || *retried.Execution.ResumeFromNode != "n3" {
		t.Fatalf("resumeFromNode %v, want n3", retried.Execution.ResumeFromNode)
	}

	before := s.executionDetail(retried.Execution.ID)
	prepared := nodeByName(t, before.NodeExecutions, "Prepare")
	if prepared.Status != domain.StatusSucceeded {
		t.Fatalf("the retry should start with Prepare already succeeded, got %q", prepared.Status)
	}
	prepareFinishedAt := prepared.FinishedAt

	after := s.settle(retried.Execution.ID)
	// It fails again, which is expected — the point is that the succeeded nodes
	// were reused rather than re-executed, so their side effects happened once.
	rePrepared := nodeByName(t, after.NodeExecutions, "Prepare")
	if rePrepared.FinishedAt == nil || prepareFinishedAt == nil || !rePrepared.FinishedAt.Equal(*prepareFinishedAt) {
		t.Fatalf("Prepare was re-executed on retry: %v then %v", prepareFinishedAt, rePrepared.FinishedAt)
	}
}

func TestContinueOnFailKeepsTheFlowGoing(t *testing.T) {
	s := newStack(t)

	graph := domain.Graph{
		Nodes: []domain.GraphNode{
			{ID: "n1", Type: "trigger.manual", Name: "Start", Params: map[string]any{}},
			{ID: "n2", Type: "http.request", Name: "Optional call",
				Params:   map[string]any{"method": "GET", "url": "http://127.0.0.1:1/nowhere"},
				Settings: domain.NodeSettings{ContinueOnFail: true, TimeoutMs: 3000},
			},
			{ID: "n3", Type: "set", Name: "Always", Params: map[string]any{
				"mode":   "merge",
				"fields": []any{map[string]any{"key": "reached", "value": "yes"}},
			}},
		},
		Edges: []domain.Edge{
			{ID: "e1", Source: "n1", SourceHandle: "main", Target: "n2", TargetHandle: "main"},
			{ID: "e2", Source: "n2", SourceHandle: "main", Target: "n3", TargetHandle: "main"},
		},
	}

	workflowID := s.createWorkflow("Best effort", graph)
	execID := s.run(workflowID, map[string]any{"id": 1})
	got := s.settle(execID)
	if got.Execution.Status != domain.StatusSucceeded {
		t.Fatalf("status %q, error %+v", got.Execution.Status, got.Execution.Error)
	}

	optional := nodeByName(t, got.NodeExecutions, "Optional call")
	if optional.Status != domain.StatusSucceeded {
		t.Fatalf("continueOnFail should record the node as succeeded, got %q", optional.Status)
	}
	if optional.Error == nil {
		t.Fatal("the error must still be recorded so the drawer can show what went wrong")
	}
	if nodeByName(t, got.NodeExecutions, "Always").Status != domain.StatusSucceeded {
		t.Fatal("the downstream node should have run")
	}
}

func TestWebhookIngressRunsTheWorkflow(t *testing.T) {
	s := newStack(t)

	path := fmt.Sprintf("lead-in-%d", time.Now().UnixNano())
	graph := domain.Graph{
		Nodes: []domain.GraphNode{
			{ID: "n1", Type: "trigger.webhook", Name: "New lead",
				Params: map[string]any{"path": path, "method": "POST", "respondMode": "immediately"}},
			{ID: "n2", Type: "set", Name: "Extract", Params: map[string]any{
				"mode":   "keepOnly",
				"fields": []any{map[string]any{"key": "email", "value": "{{ $json.body.email }}"}},
			}},
		},
		Edges: []domain.Edge{{ID: "e1", Source: "n1", SourceHandle: "main", Target: "n2", TargetHandle: "main"}},
	}

	workflowID := s.createWorkflow("Webhook flow", graph)

	// Activating is what publishes the public path.
	s.request(http.MethodPatch, "/api/v1/workflows/"+workflowID,
		map[string]any{"active": true}, http.StatusOK, nil)

	body := bytes.NewReader([]byte(`{"email":"ha@acme.vn"}`))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.server.URL+"/webhook/"+path, body)
	if err != nil {
		t.Fatalf("build webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d, want 202; body %s", res.StatusCode, raw)
	}

	var accepted struct {
		ExecutionID string `json:"executionId"`
	}
	if err := json.NewDecoder(res.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode webhook response: %v", err)
	}
	s.own[accepted.ExecutionID] = true

	got := s.settle(accepted.ExecutionID)
	if got.Execution.Status != domain.StatusSucceeded {
		t.Fatalf("status %q, error %+v", got.Execution.Status, got.Execution.Error)
	}
	if got.Execution.TriggerType != domain.TriggerWebhook {
		t.Fatalf("trigger type %q, want webhook", got.Execution.TriggerType)
	}

	extract := nodeByName(t, got.NodeExecutions, "Extract")
	out := extract.Output[domain.MainHandle]
	if len(out) != 1 || out[0].JSON["email"] != "ha@acme.vn" {
		t.Fatalf("the webhook body should reach the node: %+v", out)
	}
	// keepOnly means nothing else came through.
	if len(out[0].JSON) != 1 {
		t.Fatalf("keepOnly should leave exactly one field, got %+v", out[0].JSON)
	}
}

func TestExecutionStreamEmitsProgressAndCloses(t *testing.T) {
	s := newStack(t)

	graph := domain.Graph{
		Nodes: []domain.GraphNode{
			{ID: "n1", Type: "trigger.manual", Name: "Start", Params: map[string]any{}},
		},
	}
	workflowID := s.createWorkflow("Streamed", graph)
	execID := s.run(workflowID, map[string]any{"id": 1})
	s.settle(execID)

	// Connecting after the run finished must still replay the final state and
	// then close, or a client that misses the start would hang forever.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.server.URL+"/api/v1/executions/"+execID+"/stream", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	res, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer res.Body.Close()

	if got := res.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type %q", got)
	}

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	text := string(raw)
	for _, want := range []string{"event: node", "event: execution", "event: done"} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("stream did not contain %q; got:\n%s", want, text)
		}
	}
}

func TestWorkflowListSummarisesTheLastRun(t *testing.T) {
	s := newStack(t)

	graph := domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: "trigger.manual", Name: "Start", Params: map[string]any{}},
	}}
	workflowID := s.createWorkflow("Summarised", graph)
	s.settle(s.run(workflowID, nil))

	var listed struct {
		Workflows []domain.WorkflowSummary `json:"workflows"`
	}
	s.request(http.MethodGet, "/api/v1/workflows", nil, http.StatusOK, &listed)

	var found *domain.WorkflowSummary
	for i := range listed.Workflows {
		if listed.Workflows[i].ID == workflowID {
			found = &listed.Workflows[i]
		}
	}
	if found == nil {
		t.Fatalf("workflow %s is missing from the list", workflowID)
	}
	if found.NodeCount != 1 {
		t.Fatalf("nodeCount %d, want 1", found.NodeCount)
	}
	if found.TriggerType != domain.TriggerManual {
		t.Fatalf("triggerType %q, want manual", found.TriggerType)
	}
	if found.LastRun == nil || found.LastRun.Status != domain.StatusSucceeded {
		t.Fatalf("lastRun %+v, want a succeeded run", found.LastRun)
	}
	if found.SuccessRate7d == nil || *found.SuccessRate7d != 1 {
		t.Fatalf("successRate7d %v, want 1", found.SuccessRate7d)
	}
}
