package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testStore connects to the database named by TEST_DATABASE_URL, applying the
// migrations once per test binary. Without the variable the whole Postgres half
// of the suite is skipped, so `go test ./...` stays green on a laptop with no
// database running.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run store tests")
	}
	migrateOnce.Do(func() { migrateErr = migrateForTests(dsn) })
	require.NoError(t, migrateErr, "apply migrations")

	s, err := New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}

// schemaLockKey serialises migrations between test binaries. `go test ./...`
// runs the store and queue packages in parallel, and two gooses creating their
// version table at the same instant collide.
const schemaLockKey = 918273645

// migrateForTests applies the migrations under a Postgres advisory lock; the
// second binary to arrive then finds the schema already at its latest version.
func migrateForTests(dsn string) error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, schemaLockKey); err != nil {
		return err
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, schemaLockKey) //nolint:errcheck // released on close anyway

	return Migrate(ctx, dsn, os.DirFS(filepath.Join("..", "..", "db", "migrations")), ".")
}

// newWorkspace gives a test its own workspace, so tests share a database
// without sharing rows and none of them has to clean up after the others.
func newWorkspace(t *testing.T, s *Store) (domain.Workspace, domain.User) {
	t.Helper()
	email := "store-test-" + uuid.NewString() + "@example.test"
	ws, user, err := s.EnsureSeed(context.Background(), "store test", email, "hash")
	require.NoError(t, err)
	return ws, user
}

// graphWith builds a graph whose first node is the given trigger type.
func graphWith(triggerType, cron string) domain.Graph {
	return domain.Graph{
		Nodes: []domain.GraphNode{
			{
				ID: "n1", Type: triggerType, Name: "Trigger",
				Params: map[string]any{"cron": cron},
			},
			{ID: "n2", Type: "set", Name: "Prepare"},
		},
		Edges: []domain.Edge{{ID: "e1", Source: "n1", Target: "n2"}},
	}
}

func TestEnsureSeedIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	email := "seed-" + uuid.NewString() + "@example.test"

	ws, user, err := s.EnsureSeed(ctx, "acme", email, "first-hash")
	require.NoError(t, err)
	assert.Equal(t, email, user.Email)
	assert.Equal(t, ws.ID, user.WorkspaceID)

	again, sameUser, err := s.EnsureSeed(ctx, "different name", email, "second-hash")
	require.NoError(t, err)
	assert.Equal(t, ws.ID, again.ID, "a second boot must not create a second workspace")
	assert.Equal(t, user.ID, sameUser.ID)
	assert.Equal(t, "first-hash", sameUser.PasswordHash, "an existing password must survive re-seeding")

	byEmail, err := s.UserByEmail(ctx, email)
	require.NoError(t, err)
	assert.Equal(t, user.ID, byEmail.ID)

	byID, err := s.UserByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, email, byID.Email)

	_, err = s.UserByEmail(ctx, "nobody-"+uuid.NewString()+"@example.test")
	assert.ErrorIs(t, err, domain.ErrNotFound)
	_, err = s.UserByID(ctx, "not-a-uuid")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestWorkflowVersionNumbering(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Lead sync")
	require.NoError(t, err)
	assert.False(t, wf.Active)
	assert.Nil(t, wf.ActiveVersionID)

	var lastID string
	for want := 1; want <= 3; want++ {
		v, err := s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, graphWith("trigger.manual", ""), &user.ID)
		require.NoError(t, err)
		assert.Equal(t, want, v.Version)
		assert.Len(t, v.Graph.Nodes, 2, "the graph must survive the JSONB round trip")
		require.NotNil(t, v.CreatedBy)
		assert.Equal(t, user.ID, *v.CreatedBy)
		lastID = v.ID
	}

	latest, err := s.LatestVersion(ctx, ws.ID, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, latest.Version)
	assert.Equal(t, lastID, latest.ID)

	byID, err := s.Version(ctx, lastID)
	require.NoError(t, err)
	assert.Equal(t, "Trigger", byID.Graph.Nodes[0].Name)

	reloaded, err := s.Workflow(ctx, ws.ID, wf.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded.ActiveVersionID)
	assert.Equal(t, lastID, *reloaded.ActiveVersionID, "the newest version becomes the active one")
	assert.True(t, reloaded.UpdatedAt.After(wf.UpdatedAt) || reloaded.UpdatedAt.Equal(wf.UpdatedAt))

	// A workflow in another workspace is invisible, not merely filtered out.
	other, _ := newWorkspace(t, s)
	_, err = s.Workflow(ctx, other.ID, wf.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)
	_, err = s.LatestVersion(ctx, other.ID, wf.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestWorkflowSummaryAggregation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Nightly Digest")
	require.NoError(t, err)
	version, err := s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, graphWith("trigger.schedule", "*/5 * * * *"), &user.ID)
	require.NoError(t, err)

	for _, status := range []domain.Status{domain.StatusSucceeded, domain.StatusFailed} {
		exec, err := s.CreateExecution(ctx, domain.NewExecution{
			WorkspaceID:       ws.ID,
			WorkflowID:        wf.ID,
			WorkflowVersionID: version.ID,
			TriggerType:       domain.TriggerSchedule,
		})
		require.NoError(t, err)
		require.NoError(t, s.UpdateExecution(ctx, exec.ID, domain.ExecutionPatch{Status: &status}))
	}

	summaries, err := s.ListWorkflowSummaries(ctx, ws.ID, domain.WorkflowFilter{})
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	got := summaries[0]
	assert.Equal(t, wf.ID, got.ID)
	assert.Equal(t, 2, got.NodeCount)
	assert.Equal(t, domain.TriggerSchedule, got.TriggerType)
	assert.Equal(t, "*/5 * * * *", got.TriggerDetail)
	assert.Equal(t, 2, got.ExecutionCount)
	require.NotNil(t, got.LastRun)
	assert.NotEmpty(t, got.LastRun.ID)
	require.NotNil(t, got.SuccessRate7d)
	assert.InDelta(t, 0.5, *got.SuccessRate7d, 1e-9)
}

func TestWorkflowSummaryFilters(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	quiet, err := s.CreateWorkflow(ctx, ws.ID, "Quiet Flow")
	require.NoError(t, err)
	loud, err := s.CreateWorkflow(ctx, ws.ID, "Loud Flow")
	require.NoError(t, err)

	active := true
	updated, err := s.UpdateWorkflow(ctx, ws.ID, loud.ID, domain.WorkflowPatch{Active: &active})
	require.NoError(t, err)
	assert.True(t, updated.Active)

	// A workflow with no version has no runs, so its success rate stays unset.
	summaries, err := s.ListWorkflowSummaries(ctx, ws.ID, domain.WorkflowFilter{Status: "active"})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, loud.ID, summaries[0].ID)
	assert.Nil(t, summaries[0].SuccessRate7d)
	assert.Nil(t, summaries[0].LastRun)
	assert.Equal(t, 0, summaries[0].NodeCount)
	assert.Equal(t, domain.TriggerManual, summaries[0].TriggerType)

	summaries, err = s.ListWorkflowSummaries(ctx, ws.ID, domain.WorkflowFilter{Status: "inactive"})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, quiet.ID, summaries[0].ID)

	summaries, err = s.ListWorkflowSummaries(ctx, ws.ID, domain.WorkflowFilter{Query: "QUIET"})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, quiet.ID, summaries[0].ID)

	summaries, err = s.ListWorkflowSummaries(ctx, ws.ID, domain.WorkflowFilter{Sort: domain.SortName})
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	assert.Equal(t, "Loud Flow", summaries[0].Name)

	require.NoError(t, s.DeleteWorkflow(ctx, ws.ID, quiet.ID))
	assert.ErrorIs(t, s.DeleteWorkflow(ctx, ws.ID, quiet.ID), domain.ErrNotFound)
}

func TestExecutionKeysetPagination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Paged")
	require.NoError(t, err)
	version, err := s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, graphWith("trigger.manual", ""), &user.ID)
	require.NoError(t, err)

	var created []string
	for i := 0; i < 3; i++ {
		exec, err := s.CreateExecution(ctx, domain.NewExecution{
			WorkspaceID:       ws.ID,
			WorkflowID:        wf.ID,
			WorkflowVersionID: version.ID,
			TriggerType:       domain.TriggerManual,
			TriggerData:       []domain.Item{domain.NewItem(map[string]any{"i": i})},
		})
		require.NoError(t, err)
		assert.Equal(t, domain.StatusQueued, exec.Status)
		assert.Nil(t, exec.StartedAt, "the engine, not the store, starts an execution")
		assert.Equal(t, "Paged", exec.WorkflowName)
		assert.Equal(t, 1, exec.Version)
		require.Len(t, exec.TriggerData, 1)
		created = append(created, exec.ID)
	}

	first, cursor, err := s.ListExecutions(ctx, ws.ID, domain.ExecutionFilter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.NotEmpty(t, cursor, "a full page must offer a cursor")

	second, next, err := s.ListExecutions(ctx, ws.ID, domain.ExecutionFilter{Limit: 2, Cursor: cursor})
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Empty(t, next, "a short page is the last page")

	seen := []string{first[0].ID, first[1].ID, second[0].ID}
	assert.ElementsMatch(t, created, seen, "the two pages must cover every row exactly once")
	assert.Equal(t, created[2], first[0].ID, "newest first")

	// Filters narrow the same keyset query.
	failed := domain.StatusFailed
	require.NoError(t, s.UpdateExecution(ctx, created[0], domain.ExecutionPatch{Status: &failed}))
	onlyFailed, _, err := s.ListExecutions(ctx, ws.ID, domain.ExecutionFilter{Status: domain.StatusFailed})
	require.NoError(t, err)
	require.Len(t, onlyFailed, 1)
	assert.Equal(t, created[0], onlyFailed[0].ID)

	byWorkflow, _, err := s.ListExecutions(ctx, ws.ID, domain.ExecutionFilter{WorkflowID: wf.ID})
	require.NoError(t, err)
	assert.Len(t, byWorkflow, 3)

	_, _, err = s.ListExecutions(ctx, ws.ID, domain.ExecutionFilter{Cursor: "not-a-cursor"})
	assert.Error(t, err)
}

func TestExecutionLifecycleFields(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Lifecycle")
	require.NoError(t, err)
	version, err := s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, graphWith("trigger.manual", ""), &user.ID)
	require.NoError(t, err)
	exec, err := s.CreateExecution(ctx, domain.NewExecution{
		WorkspaceID:       ws.ID,
		WorkflowID:        wf.ID,
		WorkflowVersionID: version.ID,
		TriggerType:       domain.TriggerManual,
	})
	require.NoError(t, err)

	started := time.Now().UTC().Truncate(time.Millisecond)
	finished := started.Add(1500 * time.Millisecond)
	running, failed := domain.StatusRunning, domain.StatusFailed
	require.NoError(t, s.UpdateExecution(ctx, exec.ID, domain.ExecutionPatch{Status: &running, StartedAt: &started}))
	require.NoError(t, s.UpdateExecution(ctx, exec.ID, domain.ExecutionPatch{
		Status:     &failed,
		FinishedAt: &finished,
		Error:      &domain.NodeError{Code: domain.ErrCodeHTTP, Message: "502", NodeID: "n2"},
	}))

	loaded, err := s.Execution(ctx, ws.ID, exec.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusFailed, loaded.Status)
	require.NotNil(t, loaded.Error)
	assert.Equal(t, "502", loaded.Error.Message)
	require.NotNil(t, loaded.DurationMs())
	assert.Equal(t, int64(1500), *loaded.DurationMs())

	require.NoError(t, s.UpdateExecution(ctx, exec.ID, domain.ExecutionPatch{ClearError: true}))
	cleared, err := s.ExecutionByID(ctx, exec.ID)
	require.NoError(t, err)
	assert.Nil(t, cleared.Error)

	// An empty patch is a no-op rather than an error.
	require.NoError(t, s.UpdateExecution(ctx, exec.ID, domain.ExecutionPatch{}))

	requested, err := s.IsCancelRequested(ctx, exec.ID)
	require.NoError(t, err)
	assert.False(t, requested)
	require.NoError(t, s.RequestCancel(ctx, ws.ID, exec.ID))
	requested, err = s.IsCancelRequested(ctx, exec.ID)
	require.NoError(t, err)
	assert.True(t, requested)

	other, _ := newWorkspace(t, s)
	assert.ErrorIs(t, s.RequestCancel(ctx, other.ID, exec.ID), domain.ErrNotFound)
	assert.ErrorIs(t, s.UpdateExecution(ctx, uuid.NewString(), domain.ExecutionPatch{Status: &failed}), domain.ErrNotFound)
}

func TestNodeExecutionUpsertOverwrites(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Nodes")
	require.NoError(t, err)
	version, err := s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, graphWith("trigger.manual", ""), &user.ID)
	require.NoError(t, err)
	exec, err := s.CreateExecution(ctx, domain.NewExecution{
		WorkspaceID:       ws.ID,
		WorkflowID:        wf.ID,
		WorkflowVersionID: version.ID,
		TriggerType:       domain.TriggerManual,
	})
	require.NoError(t, err)

	started := time.Now().UTC().Truncate(time.Millisecond)
	running, err := s.UpsertNodeExecution(ctx, domain.NodeExecution{
		ExecutionID: exec.ID,
		NodeID:      "n2",
		NodeName:    "Prepare",
		NodeType:    "set",
		Status:      domain.StatusRunning,
		Input:       []domain.Item{domain.NewItem(map[string]any{"in": 1})},
		StartedAt:   &started,
	})
	require.NoError(t, err)
	assert.Nil(t, running.Output, "an unfinished node stores SQL NULL, not an empty object")
	assert.Nil(t, running.Error)
	assert.Equal(t, 1, running.Attempt)

	finished := started.Add(200 * time.Millisecond)
	settled, err := s.UpsertNodeExecution(ctx, domain.NodeExecution{
		ExecutionID: exec.ID,
		NodeID:      "n2",
		NodeName:    "Prepare",
		NodeType:    "set",
		Status:      domain.StatusSucceeded,
		Attempt:     2,
		Input:       []domain.Item{domain.NewItem(map[string]any{"in": 1})},
		Output:      map[string][]domain.Item{domain.MainHandle: {domain.NewItem(map[string]any{"out": true})}},
		StartedAt:   &started,
		FinishedAt:  &finished,
	})
	require.NoError(t, err)
	assert.Equal(t, running.ID, settled.ID, "the second write must update the same row")
	assert.Equal(t, domain.StatusSucceeded, settled.Status)
	assert.Equal(t, 2, settled.Attempt)
	assert.Equal(t, 1, settled.ItemCount())

	rows, err := s.ListNodeExecutions(ctx, exec.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, domain.StatusSucceeded, rows[0].Status)
	require.NotNil(t, rows[0].DurationMs())

	require.NoError(t, s.DeleteNodeExecutionsFrom(ctx, exec.ID, nil), "no node ids is a no-op")
	require.NoError(t, s.DeleteNodeExecutionsFrom(ctx, exec.ID, []string{"n2"}))
	rows, err = s.ListNodeExecutions(ctx, exec.ID)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestReplaceWebhooksRejectsDuplicatePath(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	first, err := s.CreateWorkflow(ctx, ws.ID, "Ingress A")
	require.NoError(t, err)
	second, err := s.CreateWorkflow(ctx, ws.ID, "Ingress B")
	require.NoError(t, err)
	path := "lead-in-" + uuid.NewString()

	require.NoError(t, s.ReplaceWebhooks(ctx, ws.ID, first.ID, []domain.Webhook{{NodeID: "n1", Path: path}}))
	hook, err := s.WebhookByPath(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, first.ID, hook.WorkflowID)
	assert.Equal(t, "POST", hook.Method, "the method defaults rather than violating NOT NULL")

	err = s.ReplaceWebhooks(ctx, ws.ID, second.ID, []domain.Webhook{
		{NodeID: "n1", Path: "other-" + uuid.NewString()},
		{NodeID: "n2", Path: path},
	})
	assert.ErrorIs(t, err, domain.ErrConflict)

	// The failed batch was one transaction, so neither of its rows exists.
	hook, err = s.WebhookByPath(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, first.ID, hook.WorkflowID)

	// Reconciling the same workflow again replaces rather than accumulates.
	require.NoError(t, s.ReplaceWebhooks(ctx, ws.ID, first.ID, nil))
	_, err = s.WebhookByPath(ctx, path)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestSchedulesDueAndAdvance(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Cron")
	require.NoError(t, err)

	// The due times sit in the near future and the queries use a synthetic
	// "now" past them. That pins the test to the SQL predicate rather than to
	// wall-clock ordering, and keeps a live worker — which sweeps schedules
	// globally using the real clock — from consuming the rows mid-test.
	base := time.Now().UTC()
	soon := base.Add(30 * time.Second)
	later := base.Add(2 * time.Hour)
	asOf := base.Add(time.Minute)

	require.NoError(t, s.ReplaceSchedules(ctx, ws.ID, wf.ID, []domain.Schedule{
		{NodeID: "n1", Cron: "*/5 * * * *", NextRunAt: soon},
		{NodeID: "n2", Cron: "0 9 * * *", NextRunAt: later},
	}))

	// A generous limit: DueSchedules orders by next_run_at, so any backlog of
	// older due rows in a shared database would otherwise crowd this test's own
	// rows out of a small page. The predicate is what is under test here.
	schedules, err := s.DueSchedules(ctx, asOf, 500)
	require.NoError(t, err)
	var mine []domain.Schedule
	for _, sc := range schedules {
		if sc.WorkflowID == wf.ID {
			mine = append(mine, sc)
		}
	}
	require.Len(t, mine, 1, "only the schedule due by asOf is returned")
	assert.Equal(t, "n1", mine[0].NodeID)
	assert.Equal(t, "UTC", mine[0].Timezone)

	require.NoError(t, s.MarkScheduleRun(ctx, mine[0].ID, asOf, later))
	schedules, err = s.DueSchedules(ctx, asOf, 500)
	require.NoError(t, err)
	for _, sc := range schedules {
		assert.NotEqual(t, wf.ID, sc.WorkflowID, "an advanced schedule is no longer due")
	}

	assert.ErrorIs(t, s.MarkScheduleRun(ctx, uuid.NewString(), asOf, later), domain.ErrNotFound)
}

func TestFirstWorkspaceAndPing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	newWorkspace(t, s)

	require.NoError(t, s.Ping(ctx))
	ws, err := s.FirstWorkspace(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, ws.ID)
	assert.NotNil(t, s.Pool())
}
