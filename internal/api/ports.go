package api

import (
	"context"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// Store is the slice of persistence the HTTP layer uses. The concrete
// *store.Store satisfies it structurally, so declaring it here buys handler
// tests a fake without an import cycle or a database.
type Store interface {
	Ping(ctx context.Context) error

	UserByEmail(ctx context.Context, email string) (domain.User, error)
	UserByID(ctx context.Context, id string) (domain.User, error)

	ListWorkflowSummaries(ctx context.Context, workspaceID string, f domain.WorkflowFilter) ([]domain.WorkflowSummary, error)
	CreateWorkflow(ctx context.Context, workspaceID, name string) (domain.Workflow, error)
	Workflow(ctx context.Context, workspaceID, id string) (domain.Workflow, error)
	UpdateWorkflow(ctx context.Context, workspaceID, id string, p domain.WorkflowPatch) (domain.Workflow, error)
	DeleteWorkflow(ctx context.Context, workspaceID, id string) error

	CreateWorkflowVersion(ctx context.Context, workspaceID, workflowID string, graph domain.Graph, createdBy *string) (domain.WorkflowVersion, error)
	LatestVersion(ctx context.Context, workspaceID, workflowID string) (domain.WorkflowVersion, error)
	Version(ctx context.Context, id string) (domain.WorkflowVersion, error)

	CreateExecution(ctx context.Context, in domain.NewExecution) (domain.Execution, error)
	Execution(ctx context.Context, workspaceID, id string) (domain.Execution, error)
	ListExecutions(ctx context.Context, workspaceID string, f domain.ExecutionFilter) ([]domain.Execution, string, error)
	UpdateExecution(ctx context.Context, id string, p domain.ExecutionPatch) error
	RequestCancel(ctx context.Context, workspaceID, id string) error

	ListNodeExecutions(ctx context.Context, executionID string) ([]domain.NodeExecution, error)
	UpsertNodeExecution(ctx context.Context, ne domain.NodeExecution) (domain.NodeExecution, error)

	ReplaceWebhooks(ctx context.Context, workspaceID, workflowID string, hooks []domain.Webhook) error
	WebhookByPath(ctx context.Context, path string) (domain.Webhook, error)
	ReplaceSchedules(ctx context.Context, workspaceID, workflowID string, schedules []domain.Schedule) error

	// Unused by the HTTP layer today, but part of the same repository and listed
	// so the interface documents the whole contract the API is compiled against.
	DueSchedules(ctx context.Context, now time.Time, limit int) ([]domain.Schedule, error)
}

// Enqueuer is the queue as the API sees it: the API only ever adds work.
type Enqueuer interface {
	Enqueue(ctx context.Context, kind string, payload any) (int64, error)
}
