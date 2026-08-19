package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// fakeStore is an in-memory Store. Handler tests care about status codes,
// payload shapes and which store calls happened — none of which needs SQL.
type fakeStore struct {
	mu sync.Mutex

	users      map[string]domain.User // by email
	usersByID  map[string]domain.User
	workflows  map[string]domain.Workflow
	versions   map[string]domain.WorkflowVersion // by version id
	latest     map[string]string                 // workflow id -> version id
	executions map[string]domain.Execution
	nodeExecs  map[string][]domain.NodeExecution // execution id -> rows
	webhooks   map[string]domain.Webhook         // path -> hook
	summaries  []domain.WorkflowSummary

	// credentials holds the sealed blob and nothing more, like the real table.
	credentials map[string]*fakeCredential
	credOrder   []string

	replacedWebhooks  map[string][]domain.Webhook
	replacedSchedules map[string][]domain.Schedule

	seq       int
	failWith  error
	pingError error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:             map[string]domain.User{},
		usersByID:         map[string]domain.User{},
		workflows:         map[string]domain.Workflow{},
		versions:          map[string]domain.WorkflowVersion{},
		latest:            map[string]string{},
		executions:        map[string]domain.Execution{},
		nodeExecs:         map[string][]domain.NodeExecution{},
		webhooks:          map[string]domain.Webhook{},
		replacedWebhooks:  map[string][]domain.Webhook{},
		replacedSchedules: map[string][]domain.Schedule{},
		credentials:       map[string]*fakeCredential{},
	}
}

func (f *fakeStore) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d", prefix, f.seq)
}

func (f *fakeStore) addUser(u domain.User) {
	f.users[u.Email] = u
	f.usersByID[u.ID] = u
}

func (f *fakeStore) addWorkflow(wf domain.Workflow, graph domain.Graph) domain.WorkflowVersion {
	f.workflows[wf.ID] = wf
	v := domain.WorkflowVersion{
		ID:         f.nextID("version"),
		WorkflowID: wf.ID,
		Version:    1,
		Graph:      graph,
		CreatedAt:  time.Now(),
	}
	f.versions[v.ID] = v
	f.latest[wf.ID] = v.ID
	return v
}

func (f *fakeStore) Ping(context.Context) error { return f.pingError }

func (f *fakeStore) UserByEmail(_ context.Context, email string) (domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[email]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (f *fakeStore) UserByID(_ context.Context, id string) (domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.usersByID[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (f *fakeStore) ListWorkflowSummaries(context.Context, string, domain.WorkflowFilter) ([]domain.WorkflowSummary, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	return f.summaries, nil
}

func (f *fakeStore) CreateWorkflow(_ context.Context, workspaceID, name string) (domain.Workflow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wf := domain.Workflow{
		ID:          f.nextID("workflow"),
		WorkspaceID: workspaceID,
		Name:        name,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	f.workflows[wf.ID] = wf
	return wf, nil
}

func (f *fakeStore) Workflow(_ context.Context, workspaceID, id string) (domain.Workflow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wf, ok := f.workflows[id]
	if !ok || wf.WorkspaceID != workspaceID {
		return domain.Workflow{}, domain.ErrNotFound
	}
	return wf, nil
}

func (f *fakeStore) UpdateWorkflow(_ context.Context, workspaceID, id string, p domain.WorkflowPatch) (domain.Workflow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wf, ok := f.workflows[id]
	if !ok || wf.WorkspaceID != workspaceID {
		return domain.Workflow{}, domain.ErrNotFound
	}
	if p.Name != nil {
		wf.Name = *p.Name
	}
	if p.Active != nil {
		wf.Active = *p.Active
	}
	if p.ActiveVersionID != nil {
		wf.ActiveVersionID = p.ActiveVersionID
	}
	wf.UpdatedAt = time.Now()
	f.workflows[id] = wf
	return wf, nil
}

func (f *fakeStore) DeleteWorkflow(_ context.Context, workspaceID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	wf, ok := f.workflows[id]
	if !ok || wf.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	delete(f.workflows, id)
	return nil
}

func (f *fakeStore) CreateWorkflowVersion(_ context.Context, workspaceID, workflowID string, graph domain.Graph, createdBy *string) (domain.WorkflowVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wf, ok := f.workflows[workflowID]
	if !ok || wf.WorkspaceID != workspaceID {
		return domain.WorkflowVersion{}, domain.ErrNotFound
	}
	next := 1
	if current, ok := f.latest[workflowID]; ok {
		next = f.versions[current].Version + 1
	}
	v := domain.WorkflowVersion{
		ID:         f.nextID("version"),
		WorkflowID: workflowID,
		Version:    next,
		Graph:      graph,
		CreatedBy:  createdBy,
		CreatedAt:  time.Now(),
	}
	f.versions[v.ID] = v
	f.latest[workflowID] = v.ID
	return v, nil
}

func (f *fakeStore) LatestVersion(_ context.Context, workspaceID, workflowID string) (domain.WorkflowVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wf, ok := f.workflows[workflowID]
	if !ok || wf.WorkspaceID != workspaceID {
		return domain.WorkflowVersion{}, domain.ErrNotFound
	}
	id, ok := f.latest[workflowID]
	if !ok {
		return domain.WorkflowVersion{}, domain.ErrNotFound
	}
	return f.versions[id], nil
}

func (f *fakeStore) Version(_ context.Context, id string) (domain.WorkflowVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.versions[id]
	if !ok {
		return domain.WorkflowVersion{}, domain.ErrNotFound
	}
	return v, nil
}

func (f *fakeStore) CreateExecution(_ context.Context, in domain.NewExecution) (domain.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := domain.Execution{
		ID:                f.nextID("exec"),
		WorkspaceID:       in.WorkspaceID,
		WorkflowID:        in.WorkflowID,
		WorkflowVersionID: in.WorkflowVersionID,
		Status:            in.Status,
		TriggerType:       in.TriggerType,
		TriggerData:       in.TriggerData,
		ResumeFromNode:    in.ResumeFromNode,
		CreatedAt:         time.Now(),
	}
	f.executions[e.ID] = e
	return e, nil
}

func (f *fakeStore) Execution(_ context.Context, workspaceID, id string) (domain.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.executions[id]
	if !ok || e.WorkspaceID != workspaceID {
		return domain.Execution{}, domain.ErrNotFound
	}
	return e, nil
}

func (f *fakeStore) ListExecutions(_ context.Context, workspaceID string, _ domain.ExecutionFilter) ([]domain.Execution, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Execution
	for _, e := range f.executions {
		if e.WorkspaceID == workspaceID {
			out = append(out, e)
		}
	}
	return out, "", nil
}

func (f *fakeStore) UpdateExecution(_ context.Context, id string, p domain.ExecutionPatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.executions[id]
	if !ok {
		return domain.ErrNotFound
	}
	if p.Status != nil {
		e.Status = *p.Status
	}
	if p.ClearError {
		e.Error = nil
	} else if p.Error != nil {
		e.Error = p.Error
	}
	if p.StartedAt != nil {
		e.StartedAt = p.StartedAt
	}
	if p.FinishedAt != nil {
		e.FinishedAt = p.FinishedAt
	}
	f.executions[id] = e
	return nil
}

func (f *fakeStore) RequestCancel(_ context.Context, workspaceID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.executions[id]
	if !ok || e.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	e.CancelRequested = true
	f.executions[id] = e
	return nil
}

func (f *fakeStore) ListNodeExecutions(_ context.Context, executionID string) ([]domain.NodeExecution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nodeExecs[executionID], nil
}

func (f *fakeStore) UpsertNodeExecution(_ context.Context, ne domain.NodeExecution) (domain.NodeExecution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ne.ID == "" {
		ne.ID = f.nextID("nodeexec")
	}
	rows := f.nodeExecs[ne.ExecutionID]
	for i, existing := range rows {
		if existing.NodeID == ne.NodeID {
			rows[i] = ne
			f.nodeExecs[ne.ExecutionID] = rows
			return ne, nil
		}
	}
	f.nodeExecs[ne.ExecutionID] = append(rows, ne)
	return ne, nil
}

func (f *fakeStore) ReplaceWebhooks(_ context.Context, _, workflowID string, hooks []domain.Webhook) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replacedWebhooks[workflowID] = hooks
	for path, h := range f.webhooks {
		if h.WorkflowID == workflowID {
			delete(f.webhooks, path)
		}
	}
	for _, h := range hooks {
		f.webhooks[h.Path] = h
	}
	return nil
}

func (f *fakeStore) WebhookByPath(_ context.Context, path string) (domain.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.webhooks[path]
	if !ok {
		return domain.Webhook{}, domain.ErrNotFound
	}
	return h, nil
}

func (f *fakeStore) ReplaceSchedules(_ context.Context, _, workflowID string, schedules []domain.Schedule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replacedSchedules[workflowID] = schedules
	return nil
}

func (f *fakeStore) DueSchedules(context.Context, time.Time, int) ([]domain.Schedule, error) {
	return nil, nil
}

// fakeQueue records what the API enqueued.
type fakeQueue struct {
	mu       sync.Mutex
	enqueued []struct {
		Kind    string
		Payload any
	}
	failWith error
}

func (q *fakeQueue) Enqueue(_ context.Context, kind string, payload any) (int64, error) {
	if q.failWith != nil {
		return 0, q.failWith
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.enqueued = append(q.enqueued, struct {
		Kind    string
		Payload any
	}{kind, payload})
	return int64(len(q.enqueued)), nil
}

func (q *fakeQueue) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.enqueued)
}

/* --- credentials -----------------------------------------------------------

   The credential half of the fake store. It holds the sealed blob exactly as
   the real one does — opaque bytes it never looks inside — so the handler tests
   exercise the real credentials.Service, real sealing included, without a
   database. A fake that returned plaintext here would test nothing worth
   testing.
   --------------------------------------------------------------------------- */

// fakeCredential is one stored row plus its sealed blob.
type fakeCredential struct {
	rec    domain.Credential
	sealed []byte
}

func (f *fakeStore) CreateCredential(_ context.Context, workspaceID, credType, name string, sealed []byte) (domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := domain.Credential{
		ID:          f.nextID("cred"),
		WorkspaceID: workspaceID,
		Type:        credType,
		Name:        name,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	f.credentials[rec.ID] = &fakeCredential{rec: rec, sealed: append([]byte(nil), sealed...)}
	f.credOrder = append(f.credOrder, rec.ID)
	return rec, nil
}

func (f *fakeStore) Credential(_ context.Context, workspaceID, id string) (domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.credentials[id]
	if !ok || c.rec.WorkspaceID != workspaceID {
		return domain.Credential{}, domain.ErrNotFound
	}
	return c.rec, nil
}

func (f *fakeStore) CredentialSealed(_ context.Context, workspaceID, id string) (domain.Credential, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.credentials[id]
	if !ok || c.rec.WorkspaceID != workspaceID {
		return domain.Credential{}, nil, domain.ErrNotFound
	}
	return c.rec, c.sealed, nil
}

func (f *fakeStore) ListCredentials(_ context.Context, workspaceID, credType string) ([]domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Credential
	for _, id := range f.credOrder {
		c := f.credentials[id]
		if c == nil || c.rec.WorkspaceID != workspaceID {
			continue
		}
		if credType != "" && c.rec.Type != credType {
			continue
		}
		out = append(out, c.rec)
	}
	return out, nil
}

func (f *fakeStore) UpdateCredential(_ context.Context, workspaceID, id string, name *string, sealed []byte) (domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.credentials[id]
	if !ok || c.rec.WorkspaceID != workspaceID {
		return domain.Credential{}, domain.ErrNotFound
	}
	if name != nil {
		c.rec.Name = *name
	}
	// A nil blob leaves the stored secret alone, exactly like the SQL COALESCE.
	if len(sealed) > 0 {
		c.sealed = append([]byte(nil), sealed...)
	}
	c.rec.UpdatedAt = time.Now()
	return c.rec, nil
}

func (f *fakeStore) DeleteCredential(_ context.Context, workspaceID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.credentials[id]
	if !ok || c.rec.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	delete(f.credentials, id)
	return nil
}

func (f *fakeStore) CredentialUsage(_ context.Context, workspaceID string) (map[string]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for _, v := range f.versions {
		wf, ok := f.workflows[v.WorkflowID]
		if !ok || wf.WorkspaceID != workspaceID {
			continue
		}
		for _, n := range v.Graph.Nodes {
			if n.CredentialID != nil && *n.CredentialID != "" {
				out[*n.CredentialID]++
			}
		}
	}
	return out, nil
}

func (f *fakeStore) TouchCredential(_ context.Context, workspaceID, id string, sealed []byte, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.credentials[id]
	if !ok || c.rec.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	c.sealed = append([]byte(nil), sealed...)
	c.rec.UpdatedAt = at
	return nil
}
