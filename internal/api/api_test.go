package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/auth"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/config"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

const (
	testWorkspace = "ws-1"
	testUserID    = "user-1"
	testEmail     = "ha.nguyen@acme.vn"
	testPassword  = "flowgrid123"
)

type harness struct {
	router http.Handler
	store  *fakeStore
	queue  *fakeQueue
	signer *auth.Signer
	token  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	st := newFakeStore()
	st.addUser(domain.User{
		ID:           testUserID,
		WorkspaceID:  testWorkspace,
		Email:        testEmail,
		PasswordHash: hash,
		Role:         "owner",
		CreatedAt:    time.Now(),
	})

	signer, err := auth.NewSigner([]byte("test-secret"))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	token, _, err := signer.Issue(testUserID, testWorkspace, testEmail, "owner", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	q := &fakeQueue{}
	router := NewRouter(Deps{
		Store:    st,
		Queue:    q,
		Registry: nodes.Default(),
		Signer:   signer,
		Config:   config.Config{PublicBaseURL: "http://localhost:3000"},
	})

	return &harness{router: router, store: st, queue: q, signer: signer, token: token}
}

func (h *harness) do(t *testing.T, method, path string, body any, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	r := httptest.NewRequest(method, path, reader)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.token})
	}

	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
	return out
}

// --- auth -------------------------------------------------------------------

func TestLoginSucceedsAndSetsCookie(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": testEmail, "password": testPassword, "remember": true}, false)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	body := decode[map[string]any](t, w)
	user := body["user"].(map[string]any)
	if user["email"] != testEmail || user["workspaceId"] != testWorkspace {
		t.Fatalf("unexpected user payload: %v", user)
	}
	if _, present := user["passwordHash"]; present {
		t.Fatal("the password hash must never be serialised")
	}

	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" && c.HttpOnly {
			found = true
		}
	}
	if !found {
		t.Fatal("expected an HttpOnly session cookie")
	}
}

func TestLoginRejectsWrongPasswordWithTheSameMessageAsUnknownEmail(t *testing.T) {
	h := newHarness(t)

	wrongPassword := h.do(t, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": testEmail, "password": "nope"}, false)
	unknownEmail := h.do(t, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": "nobody@acme.vn", "password": testPassword}, false)

	for _, w := range []*httptest.ResponseRecorder{wrongPassword, unknownEmail} {
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401; body %s", w.Code, w.Body.String())
		}
	}
	// Identical answers are what stop the endpoint being an account oracle.
	if wrongPassword.Body.String() != unknownEmail.Body.String() {
		t.Fatalf("responses differ:\n%s\n%s", wrongPassword.Body.String(), unknownEmail.Body.String())
	}
}

func TestProtectedRoutesRequireASession(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/api/v1/auth/me", "/api/v1/workflows", "/api/v1/executions", "/api/v1/node-types"} {
		w := h.do(t, http.MethodGet, path, nil, false)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", path, w.Code)
		}
	}
}

func TestMeReturnsTheSignedInUser(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodGet, "/api/v1/auth/me", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	body := decode[map[string]any](t, w)
	if body["user"].(map[string]any)["id"] != testUserID {
		t.Fatalf("unexpected user: %v", body)
	}
}

// --- node types -------------------------------------------------------------

func TestNodeTypesServesEveryDescriptor(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodGet, "/api/v1/node-types", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	body := decode[struct {
		NodeTypes []nodes.Descriptor `json:"nodeTypes"`
	}](t, w)

	if len(body.NodeTypes) != len(nodes.Default().Types()) {
		t.Fatalf("got %d descriptors, want %d", len(body.NodeTypes), len(nodes.Default().Types()))
	}
	// The frontend builds its whole config form from these fields, so an empty
	// one is a silently broken screen rather than an error.
	for _, d := range body.NodeTypes {
		if d.Type == "" || d.Name == "" || d.Category == "" || d.Icon == "" {
			t.Fatalf("descriptor is missing a field the UI needs: %+v", d)
		}
		if len(d.Outputs) == 0 {
			t.Fatalf("descriptor %s has no outputs", d.Type)
		}
	}
}

func TestTestNodeReturnsOutputWithoutPersisting(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodPost, "/api/v1/nodes/set/test", map[string]any{
		"params": map[string]any{
			"mode":   "merge",
			"fields": []any{map[string]any{"key": "greeting", "value": "hello"}},
		},
		"inputItems": []any{map[string]any{"json": map[string]any{"id": 1}}},
	}, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	body := decode[map[string]any](t, w)
	if body["error"] != nil {
		t.Fatalf("unexpected node error: %v", body["error"])
	}
	outputs := body["outputs"].(map[string]any)
	main := outputs["main"].([]any)
	if len(main) != 1 {
		t.Fatalf("got %d output items, want 1", len(main))
	}
	got := main[0].(map[string]any)["json"].(map[string]any)
	if got["greeting"] != "hello" || got["id"] == nil {
		t.Fatalf("merge mode should keep the input and add the field: %v", got)
	}
	if len(h.store.nodeExecs) != 0 {
		t.Fatal("a test step must not persist a node execution")
	}
}

func TestTestNodeRejectsUnknownType(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodPost, "/api/v1/nodes/does.not.exist/test",
		map[string]any{"params": map[string]any{}}, true)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404; body %s", w.Code, w.Body.String())
	}
}

// --- workflows --------------------------------------------------------------

func TestCreateWorkflowAlsoCreatesAnEmptyFirstVersion(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodPost, "/api/v1/workflows", map[string]any{"name": "  "}, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	body := decode[workflowDetail](t, w)

	if body.Workflow.Name != "Untitled workflow" {
		t.Fatalf("a blank name should default, got %q", body.Workflow.Name)
	}
	// The editor always loads a version, so one must exist from the start.
	if body.Version != 1 {
		t.Fatalf("version %d, want 1", body.Version)
	}
}

func TestSaveVersionRejectsDuplicateNodeNames(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Lead enrichment")

	w := h.do(t, http.MethodPost, "/api/v1/workflows/"+wf.ID+"/versions", map[string]any{
		"graph": domain.Graph{Nodes: []domain.GraphNode{
			{ID: "n1", Type: "set", Name: "Same"},
			{ID: "n2", Type: "set", Name: "Same"},
		}},
	}, true)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unique") {
		t.Fatalf("the message should explain why names must be unique: %s", w.Body.String())
	}
}

func TestSaveVersionRejectsACycle(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Loopy")

	w := h.do(t, http.MethodPost, "/api/v1/workflows/"+wf.ID+"/versions", map[string]any{
		"graph": domain.Graph{
			Nodes: []domain.GraphNode{
				{ID: "n1", Type: "set", Name: "A"},
				{ID: "n2", Type: "set", Name: "B"},
			},
			Edges: []domain.Edge{
				{ID: "e1", Source: "n1", Target: "n2"},
				{ID: "e2", Source: "n2", Target: "n1"},
			},
		},
	}, true)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "loop") {
		t.Fatalf("the message should name the loop: %s", w.Body.String())
	}
}

func TestSaveVersionAcceptsAnIncompleteDraft(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Draft")

	// No trigger and an empty required URL: normal while editing, and refusing
	// to save it would make the editor lose work.
	w := h.do(t, http.MethodPost, "/api/v1/workflows/"+wf.ID+"/versions", map[string]any{
		"graph": domain.Graph{Nodes: []domain.GraphNode{
			{ID: "n1", Type: "http.request", Name: "Fetch", Params: map[string]any{"url": ""}},
		}},
	}, true)

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201; body %s", w.Code, w.Body.String())
	}
}

func TestActivateRefusesAnUnrunnableGraph(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "No trigger")
	h.store.addWorkflow(wf, domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: "set", Name: "Only a set"},
	}})

	w := h.do(t, http.MethodPatch, "/api/v1/workflows/"+wf.ID, map[string]any{"active": true}, true)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body %s", w.Code, w.Body.String())
	}
	if h.store.workflows[wf.ID].Active {
		t.Fatal("the workflow must not be left active after a refused activation")
	}
}

func TestActivatePublishesWebhookAndScheduleRows(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Lead enrichment")
	h.store.addWorkflow(wf, domain.Graph{
		Nodes: []domain.GraphNode{
			{ID: "n1", Type: nodeTypeWebhookTrigger, Name: "New lead",
				Params: map[string]any{"path": "lead-in", "method": "POST"}},
			{ID: "n2", Type: "set", Name: "Normalise"},
		},
		Edges: []domain.Edge{{ID: "e1", Source: "n1", Target: "n2"}},
	})

	w := h.do(t, http.MethodPatch, "/api/v1/workflows/"+wf.ID, map[string]any{"active": true}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	hooks := h.store.replacedWebhooks[wf.ID]
	if len(hooks) != 1 || hooks[0].Path != "lead-in" || hooks[0].Method != "POST" {
		t.Fatalf("unexpected webhook rows: %+v", hooks)
	}

	body := decode[workflowDetail](t, w)
	if body.WebhookURL != "http://localhost:3000/webhook/lead-in" {
		t.Fatalf("webhook URL %q is not what the editor should show", body.WebhookURL)
	}
}

func TestDeactivateRemovesTriggerRows(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Lead enrichment")
	wf.Active = true
	h.store.addWorkflow(wf, domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: nodeTypeWebhookTrigger, Name: "New lead", Params: map[string]any{"path": "lead-in"}},
	}})
	if err := h.store.ReplaceWebhooks(t.Context(), testWorkspace, wf.ID, []domain.Webhook{
		{WorkflowID: wf.ID, Path: "lead-in", Method: "POST"},
	}); err != nil {
		t.Fatalf("seed webhooks: %v", err)
	}

	w := h.do(t, http.MethodPatch, "/api/v1/workflows/"+wf.ID, map[string]any{"active": false}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	if len(h.store.replacedWebhooks[wf.ID]) != 0 {
		t.Fatalf("deactivating must take the public URL down, got %+v", h.store.replacedWebhooks[wf.ID])
	}
}

func TestRunQueuesAnExecution(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Lead enrichment")
	h.store.addWorkflow(wf, runnableGraph())

	w := h.do(t, http.MethodPost, "/api/v1/workflows/"+wf.ID+"/run",
		map[string]any{"data": map[string]any{"email": "ha@acme.vn"}}, true)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202; body %s", w.Code, w.Body.String())
	}
	if h.queue.count() != 1 {
		t.Fatalf("enqueued %d jobs, want 1", h.queue.count())
	}

	body := decode[struct{ Execution domain.Execution }](t, w)
	if body.Execution.Status != domain.StatusQueued {
		t.Fatalf("status %q, want queued", body.Execution.Status)
	}
	if len(body.Execution.TriggerData) != 1 ||
		body.Execution.TriggerData[0].JSON["email"] != "ha@acme.vn" {
		t.Fatalf("the run payload should arrive as one item: %+v", body.Execution.TriggerData)
	}
}

func TestRunRefusesAnUnrunnableGraph(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Broken")
	h.store.addWorkflow(wf, domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: "set", Name: "No trigger here"},
	}})

	w := h.do(t, http.MethodPost, "/api/v1/workflows/"+wf.ID+"/run", nil, true)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body %s", w.Code, w.Body.String())
	}
	if h.queue.count() != 0 {
		t.Fatal("nothing should be queued for a graph that cannot run")
	}
}

func TestWorkflowsAreScopedToTheWorkspace(t *testing.T) {
	h := newHarness(t)
	other, _ := h.store.CreateWorkflow(t.Context(), "ws-other", "Someone else's")

	w := h.do(t, http.MethodGet, "/api/v1/workflows/"+other.ID, nil, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404 — another workspace's workflow must be invisible", w.Code)
	}
}

// --- executions -------------------------------------------------------------

func TestGetExecutionReturnsTheVersionThatRan(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Lead enrichment")
	ranVersion := h.store.addWorkflow(wf, runnableGraph())

	exec, _ := h.store.CreateExecution(t.Context(), domain.NewExecution{
		WorkspaceID: testWorkspace, WorkflowID: wf.ID, WorkflowVersionID: ranVersion.ID,
		Status: domain.StatusSucceeded, TriggerType: domain.TriggerManual,
	})

	// The workflow moves on; the execution must keep showing what it ran.
	if _, err := h.store.CreateWorkflowVersion(t.Context(), testWorkspace, wf.ID, domain.Graph{}, nil); err != nil {
		t.Fatalf("second version: %v", err)
	}

	w := h.do(t, http.MethodGet, "/api/v1/executions/"+exec.ID, nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	body := decode[struct {
		Execution domain.Execution `json:"execution"`
		Graph     domain.Graph     `json:"graph"`
	}](t, w)

	if len(body.Graph.Nodes) != len(runnableGraph().Nodes) {
		t.Fatalf("the replay graph should be the version that ran, got %d nodes", len(body.Graph.Nodes))
	}
}

func TestCancelFinishesAQueuedExecutionImmediately(t *testing.T) {
	h := newHarness(t)
	exec, _ := h.store.CreateExecution(t.Context(), domain.NewExecution{
		WorkspaceID: testWorkspace, WorkflowID: "wf", WorkflowVersionID: "v",
		Status: domain.StatusQueued, TriggerType: domain.TriggerManual,
	})

	w := h.do(t, http.MethodPost, "/api/v1/executions/"+exec.ID+"/cancel", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	// Nothing is running it, so waiting for a worker to notice would hang.
	got := h.store.executions[exec.ID]
	if got.Status != domain.StatusCancelled || got.FinishedAt == nil {
		t.Fatalf("expected a finished, cancelled execution, got %+v", got)
	}
}

func TestCancelIsCooperativeForARunningExecution(t *testing.T) {
	h := newHarness(t)
	exec, _ := h.store.CreateExecution(t.Context(), domain.NewExecution{
		WorkspaceID: testWorkspace, WorkflowID: "wf", WorkflowVersionID: "v",
		Status: domain.StatusRunning, TriggerType: domain.TriggerManual,
	})

	if w := h.do(t, http.MethodPost, "/api/v1/executions/"+exec.ID+"/cancel", nil, true); w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	got := h.store.executions[exec.ID]
	if !got.CancelRequested {
		t.Fatal("the cancel flag must be set for the engine to notice")
	}
	if got.Status != domain.StatusRunning {
		t.Fatalf("status %q — the engine, not the API, ends a running execution", got.Status)
	}
}

func TestCancelRejectsAFinishedExecution(t *testing.T) {
	h := newHarness(t)
	exec, _ := h.store.CreateExecution(t.Context(), domain.NewExecution{
		WorkspaceID: testWorkspace, WorkflowID: "wf", WorkflowVersionID: "v",
		Status: domain.StatusSucceeded, TriggerType: domain.TriggerManual,
	})

	w := h.do(t, http.MethodPost, "/api/v1/executions/"+exec.ID+"/cancel", nil, true)
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", w.Code)
	}
}

func TestRetryCarriesSucceededNodesAndResumesAtTheFailure(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Support ticket triage")
	version := h.store.addWorkflow(wf, runnableGraph())

	failed, _ := h.store.CreateExecution(t.Context(), domain.NewExecution{
		WorkspaceID: testWorkspace, WorkflowID: wf.ID, WorkflowVersionID: version.ID,
		Status:      domain.StatusFailed,
		TriggerType: domain.TriggerWebhook,
		TriggerData: []domain.Item{domain.NewItem(map[string]any{"id": "TCK-40219"})},
	})
	seed := func(nodeID, name string, status domain.Status) {
		if _, err := h.store.UpsertNodeExecution(t.Context(), domain.NodeExecution{
			ExecutionID: failed.ID, NodeID: nodeID, NodeName: name, NodeType: "set",
			Status: status,
			Output: map[string][]domain.Item{domain.MainHandle: {domain.NewItem(map[string]any{"n": nodeID})}},
		}); err != nil {
			t.Fatalf("seed node execution: %v", err)
		}
	}
	seed("n1", "New ticket", domain.StatusSucceeded)
	seed("n2", "Normalise fields", domain.StatusSucceeded)
	seed("n3", "Classify ticket", domain.StatusFailed)

	w := h.do(t, http.MethodPost, "/api/v1/executions/"+failed.ID+"/retry", nil, true)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202; body %s", w.Code, w.Body.String())
	}
	body := decode[struct{ Execution domain.Execution }](t, w)
	retry := body.Execution

	if retry.ID == failed.ID {
		t.Fatal("a retry is a new execution, so the failed one stays readable")
	}
	if retry.ResumeFromNode == nil || *retry.ResumeFromNode != "n3" {
		t.Fatalf("resumeFromNode %v, want n3", retry.ResumeFromNode)
	}
	if len(retry.TriggerData) != 1 || retry.TriggerData[0].JSON["id"] != "TCK-40219" {
		t.Fatalf("the retry must reuse the original trigger payload: %+v", retry.TriggerData)
	}

	// The succeeded nodes are carried over: that is what stops the retry from
	// re-issuing their side effects.
	carried := h.store.nodeExecs[retry.ID]
	if len(carried) != 2 {
		t.Fatalf("carried %d node executions, want 2 (the succeeded ones)", len(carried))
	}
	for _, n := range carried {
		if n.Status != domain.StatusSucceeded {
			t.Fatalf("only succeeded rows should be carried, got %s for %s", n.Status, n.NodeID)
		}
		if n.ExecutionID != retry.ID {
			t.Fatalf("carried row still points at the old execution: %+v", n)
		}
	}
	if h.queue.count() != 1 {
		t.Fatalf("enqueued %d jobs, want 1", h.queue.count())
	}
}

func TestRetryRejectsAnExecutionThatDidNotFail(t *testing.T) {
	h := newHarness(t)
	exec, _ := h.store.CreateExecution(t.Context(), domain.NewExecution{
		WorkspaceID: testWorkspace, WorkflowID: "wf", WorkflowVersionID: "v",
		Status: domain.StatusSucceeded, TriggerType: domain.TriggerManual,
	})

	w := h.do(t, http.MethodPost, "/api/v1/executions/"+exec.ID+"/retry", nil, true)
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", w.Code)
	}
}

// --- webhook ingress --------------------------------------------------------

func TestWebhookRecordsAnExecutionAndAnswersImmediately(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Lead enrichment")
	h.store.addWorkflow(wf, runnableGraph())
	if err := h.store.ReplaceWebhooks(t.Context(), testWorkspace, wf.ID, []domain.Webhook{
		{WorkspaceID: testWorkspace, WorkflowID: wf.ID, NodeID: "n1", Path: "lead-in", Method: "POST"},
	}); err != nil {
		t.Fatalf("seed webhook: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/webhook/lead-in?utm=spring",
		strings.NewReader(`{"email":"ha@acme.vn"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)

	// 202 rather than waiting for the workflow: a caller's timeout is not ours.
	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202; body %s", w.Code, w.Body.String())
	}
	if h.queue.count() != 1 {
		t.Fatalf("enqueued %d jobs, want 1", h.queue.count())
	}

	var recorded domain.Execution
	for _, e := range h.store.executions {
		recorded = e
	}
	if recorded.TriggerType != domain.TriggerWebhook {
		t.Fatalf("trigger type %q, want webhook", recorded.TriggerType)
	}
	item := recorded.TriggerData[0].JSON
	body := item["body"].(map[string]any)
	if body["email"] != "ha@acme.vn" {
		t.Fatalf("JSON body should be parsed, got %v", item["body"])
	}
	if item["query"].(map[string]any)["utm"] != "spring" {
		t.Fatalf("query should be captured, got %v", item["query"])
	}
	if item["headers"].(map[string]any)["content-type"] == nil {
		t.Fatalf("headers should be captured and lower-cased, got %v", item["headers"])
	}
}

func TestWebhookKeepsANonJSONBodyAsText(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Raw")
	h.store.addWorkflow(wf, runnableGraph())
	if err := h.store.ReplaceWebhooks(t.Context(), testWorkspace, wf.ID, []domain.Webhook{
		{WorkspaceID: testWorkspace, WorkflowID: wf.ID, NodeID: "n1", Path: "raw", Method: "POST"},
	}); err != nil {
		t.Fatalf("seed webhook: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/webhook/raw", strings.NewReader("plain text"))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202", w.Code)
	}
	for _, e := range h.store.executions {
		if e.TriggerData[0].JSON["body"] != "plain text" {
			t.Fatalf("a non-JSON body should survive as text, got %v", e.TriggerData[0].JSON["body"])
		}
	}
}

func TestWebhookUnknownPathIsANotFound(t *testing.T) {
	h := newHarness(t)

	r := httptest.NewRequest(http.MethodPost, "/webhook/nothing-here", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", w.Code)
	}
}

func TestWebhookWrongMethodIsRejectedWithAllow(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Lead enrichment")
	h.store.addWorkflow(wf, runnableGraph())
	if err := h.store.ReplaceWebhooks(t.Context(), testWorkspace, wf.ID, []domain.Webhook{
		{WorkspaceID: testWorkspace, WorkflowID: wf.ID, NodeID: "n1", Path: "lead-in", Method: "POST"},
	}); err != nil {
		t.Fatalf("seed webhook: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/webhook/lead-in", nil)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "POST" {
		t.Fatalf("Allow header %q, want POST", got)
	}
}

func TestWebhookNeedsNoSession(t *testing.T) {
	h := newHarness(t)
	wf, _ := h.store.CreateWorkflow(t.Context(), testWorkspace, "Lead enrichment")
	h.store.addWorkflow(wf, runnableGraph())
	if err := h.store.ReplaceWebhooks(t.Context(), testWorkspace, wf.ID, []domain.Webhook{
		{WorkspaceID: testWorkspace, WorkflowID: wf.ID, NodeID: "n1", Path: "lead-in", Method: "POST"},
	}); err != nil {
		t.Fatalf("seed webhook: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/webhook/lead-in", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)

	if w.Code == http.StatusUnauthorized {
		t.Fatal("the public ingress must not require a session")
	}
}

// --- helpers ----------------------------------------------------------------

func runnableGraph() domain.Graph {
	return domain.Graph{
		Nodes: []domain.GraphNode{
			{ID: "n1", Type: nodeTypeWebhookTrigger, Name: "New lead",
				Params: map[string]any{"path": "lead-in", "method": "POST"}},
			{ID: "n2", Type: "set", Name: "Normalise fields",
				Params: map[string]any{"mode": "merge", "fields": []any{}}},
		},
		Edges: []domain.Edge{{ID: "e1", Source: "n1", SourceHandle: "main", Target: "n2", TargetHandle: "main"}},
	}
}
