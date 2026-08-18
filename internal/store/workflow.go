package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

const workflowColumns = `id, workspace_id, name, active, active_version_id, created_at, updated_at`

// CreateWorkflow inserts an empty workflow; its graph arrives later as a
// version, so the editor can save a name before any node exists.
func (s *Store) CreateWorkflow(ctx context.Context, workspaceID, name string) (domain.Workflow, error) {
	if !validIDs(workspaceID) {
		return domain.Workflow{}, notFound("create workflow")
	}
	return scanWorkflow(s.pool.QueryRow(ctx,
		`INSERT INTO workflows (workspace_id, name) VALUES ($1, $2) RETURNING `+workflowColumns,
		workspaceID, name,
	), "create workflow")
}

// Workflow loads one workflow inside a workspace.
func (s *Store) Workflow(ctx context.Context, workspaceID, id string) (domain.Workflow, error) {
	if !validIDs(workspaceID, id) {
		return domain.Workflow{}, notFound("workflow")
	}
	return scanWorkflow(s.pool.QueryRow(ctx,
		`SELECT `+workflowColumns+` FROM workflows WHERE id = $1 AND workspace_id = $2`,
		id, workspaceID,
	), "workflow")
}

// UpdateWorkflow applies the non-nil fields of p and bumps updated_at. With an
// empty patch it is a read, so a no-op PATCH does not reorder the list screen.
func (s *Store) UpdateWorkflow(ctx context.Context, workspaceID, id string, p domain.WorkflowPatch) (domain.Workflow, error) {
	if !validIDs(workspaceID, id) {
		return domain.Workflow{}, notFound("update workflow")
	}

	sets := []string{"updated_at = now()"}
	args := []any{id, workspaceID}
	if p.Name != nil {
		args = append(args, *p.Name)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
	}
	if p.Active != nil {
		args = append(args, *p.Active)
		sets = append(sets, fmt.Sprintf("active = $%d", len(args)))
	}
	if p.ActiveVersionID != nil {
		args = append(args, *p.ActiveVersionID)
		sets = append(sets, fmt.Sprintf("active_version_id = $%d", len(args)))
	}
	if len(sets) == 1 {
		return s.Workflow(ctx, workspaceID, id)
	}

	query := `UPDATE workflows SET ` + strings.Join(sets, ", ") +
		` WHERE id = $1 AND workspace_id = $2 RETURNING ` + workflowColumns
	return scanWorkflow(s.pool.QueryRow(ctx, query, args...), "update workflow")
}

// DeleteWorkflow removes a workflow; versions, executions, webhooks and
// schedules follow by cascade.
func (s *Store) DeleteWorkflow(ctx context.Context, workspaceID, id string) error {
	if !validIDs(workspaceID, id) {
		return notFound("delete workflow")
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM workflows WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return translate("delete workflow", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("delete workflow")
	}
	return nil
}

func scanWorkflow(row pgx.Row, op string) (domain.Workflow, error) {
	var w domain.Workflow
	err := row.Scan(&w.ID, &w.WorkspaceID, &w.Name, &w.Active, &w.ActiveVersionID, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return domain.Workflow{}, translate(op, err)
	}
	return w, nil
}

// summaryQuery aggregates everything the workflow list screen shows in one
// round trip: node count and trigger from the newest version's graph, the most
// recent run, and the seven-day success rate. Doing this per workflow in Go
// would be an N+1 over three tables.
const summaryQuery = `
WITH latest AS (
    SELECT DISTINCT ON (v.workflow_id) v.workflow_id, v.graph
    FROM workflow_versions v
    JOIN workflows w ON w.id = v.workflow_id
    WHERE w.workspace_id = $1
    ORDER BY v.workflow_id, v.version DESC
),
last_exec AS (
    SELECT DISTINCT ON (workflow_id) workflow_id, id, status, created_at
    FROM executions
    WHERE workspace_id = $1
    ORDER BY workflow_id, created_at DESC, id DESC
),
runs AS (
    SELECT workflow_id,
           count(*) AS total,
           count(*) FILTER (WHERE status = 'succeeded' AND created_at >= now() - interval '7 days') AS ok7,
           count(*) FILTER (WHERE status = 'failed'    AND created_at >= now() - interval '7 days') AS bad7
    FROM executions
    WHERE workspace_id = $1
    GROUP BY workflow_id
)
SELECT w.id, w.name, w.active, w.updated_at,
       COALESCE(jsonb_array_length(l.graph -> 'nodes'), 0) AS node_count,
       COALESCE(t.node_type, '') AS trigger_type,
       COALESCE(t.cron, '')      AS trigger_detail,
       le.id, le.status, le.created_at,
       COALESCE(r.total, 0), COALESCE(r.ok7, 0), COALESCE(r.bad7, 0)
FROM workflows w
LEFT JOIN latest    l  ON l.workflow_id = w.id
LEFT JOIN last_exec le ON le.workflow_id = w.id
LEFT JOIN runs      r  ON r.workflow_id = w.id
LEFT JOIN LATERAL (
    SELECT n ->> 'type' AS node_type, COALESCE(n -> 'params' ->> 'cron', '') AS cron
    FROM jsonb_array_elements(COALESCE(l.graph -> 'nodes', '[]'::jsonb)) AS n
    WHERE n ->> 'type' LIKE 'trigger.%'
    ORDER BY n ->> 'type'
    LIMIT 1
) t ON true
WHERE w.workspace_id = $1`

// ListWorkflowSummaries returns the rows of the workflow list screen.
func (s *Store) ListWorkflowSummaries(ctx context.Context, workspaceID string, f domain.WorkflowFilter) ([]domain.WorkflowSummary, error) {
	if !validIDs(workspaceID) {
		return nil, notFound("list workflows")
	}

	query := summaryQuery
	args := []any{workspaceID}
	switch f.Status {
	case "active":
		query += ` AND w.active`
	case "inactive":
		query += ` AND NOT w.active`
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, q)
		query += fmt.Sprintf(` AND w.name ILIKE '%%' || $%d || '%%'`, len(args))
	}
	query += ` ORDER BY ` + workflowOrder(f.Sort)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, translate("list workflows", err)
	}
	defer rows.Close()

	var out []domain.WorkflowSummary
	for rows.Next() {
		var (
			sum       domain.WorkflowSummary
			nodeType  string
			detail    string
			runID     *string
			runStatus *string
			runAt     *time.Time
			total     int
			ok7, bad7 int
		)
		if err := rows.Scan(&sum.ID, &sum.Name, &sum.Active, &sum.UpdatedAt,
			&sum.NodeCount, &nodeType, &detail,
			&runID, &runStatus, &runAt,
			&total, &ok7, &bad7); err != nil {
			return nil, translate("scan workflow summary", err)
		}

		sum.TriggerType = triggerFromNodeType(nodeType)
		if sum.TriggerType == domain.TriggerSchedule {
			sum.TriggerDetail = detail
		}
		sum.ExecutionCount = total
		if runID != nil && runStatus != nil && runAt != nil {
			sum.LastRun = &domain.LastRun{ID: *runID, Status: domain.Status(*runStatus), At: *runAt}
		}
		if settled := ok7 + bad7; settled > 0 {
			rate := float64(ok7) / float64(settled)
			sum.SuccessRate7d = &rate
		}
		out = append(out, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, translate("list workflows", err)
	}
	return out, nil
}

// workflowOrder maps the sort choice onto a stable ORDER BY; the id tie-break
// keeps paging and test assertions deterministic.
func workflowOrder(sort domain.WorkflowSort) string {
	switch sort {
	case domain.SortName:
		return `lower(w.name), w.id`
	case domain.SortCreated:
		return `w.created_at DESC, w.id DESC`
	default:
		return `w.updated_at DESC, w.id DESC`
	}
}

// triggerFromNodeType turns a trigger node type into the summary's trigger
// label, defaulting to manual for a graph with no trigger node yet.
func triggerFromNodeType(nodeType string) domain.TriggerType {
	switch strings.TrimPrefix(nodeType, "trigger.") {
	case string(domain.TriggerWebhook):
		return domain.TriggerWebhook
	case string(domain.TriggerSchedule):
		return domain.TriggerSchedule
	default:
		return domain.TriggerManual
	}
}

const versionColumns = `id, workflow_id, version, graph, created_by, created_at`

// CreateWorkflowVersion snapshots a graph as the next version of a workflow and
// makes it the active one. Numbering and activation share a transaction that
// locks the workflow row, so two concurrent saves cannot claim one number.
func (s *Store) CreateWorkflowVersion(ctx context.Context, workspaceID, workflowID string, graph domain.Graph, createdBy *string) (domain.WorkflowVersion, error) {
	if !validIDs(workspaceID, workflowID) {
		return domain.WorkflowVersion{}, notFound("create version")
	}
	raw, err := json.Marshal(graph)
	if err != nil {
		return domain.WorkflowVersion{}, fmt.Errorf("encode graph: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.WorkflowVersion{}, translate("create version", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var locked string
	err = tx.QueryRow(ctx,
		`SELECT id FROM workflows WHERE id = $1 AND workspace_id = $2 FOR UPDATE`,
		workflowID, workspaceID,
	).Scan(&locked)
	if err != nil {
		return domain.WorkflowVersion{}, translate("lock workflow", err)
	}

	version, err := scanVersion(tx.QueryRow(ctx,
		`INSERT INTO workflow_versions (workflow_id, version, graph, created_by)
		 SELECT $1, COALESCE(MAX(version), 0) + 1, $2, $3
		 FROM workflow_versions WHERE workflow_id = $1
		 RETURNING `+versionColumns,
		workflowID, raw, createdBy,
	), "insert version")
	if err != nil {
		return domain.WorkflowVersion{}, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE workflows SET active_version_id = $1, updated_at = now() WHERE id = $2`,
		version.ID, workflowID,
	); err != nil {
		return domain.WorkflowVersion{}, translate("activate version", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.WorkflowVersion{}, translate("commit version", err)
	}
	return version, nil
}

// LatestVersion returns the newest version of a workflow, which is what the
// editor opens and what a new run executes.
func (s *Store) LatestVersion(ctx context.Context, workspaceID, workflowID string) (domain.WorkflowVersion, error) {
	if !validIDs(workspaceID, workflowID) {
		return domain.WorkflowVersion{}, notFound("latest version")
	}
	return scanVersion(s.pool.QueryRow(ctx,
		`SELECT `+prefixed("v", versionColumns)+`
		 FROM workflow_versions v JOIN workflows w ON w.id = v.workflow_id
		 WHERE v.workflow_id = $1 AND w.workspace_id = $2
		 ORDER BY v.version DESC LIMIT 1`,
		workflowID, workspaceID,
	), "latest version")
}

// Version loads one version by id. The engine calls it with the id recorded on
// an execution, which is why it is not workspace-scoped.
func (s *Store) Version(ctx context.Context, id string) (domain.WorkflowVersion, error) {
	if !validIDs(id) {
		return domain.WorkflowVersion{}, notFound("version")
	}
	return scanVersion(s.pool.QueryRow(ctx,
		`SELECT `+versionColumns+` FROM workflow_versions WHERE id = $1`, id,
	), "version")
}

func scanVersion(row pgx.Row, op string) (domain.WorkflowVersion, error) {
	var (
		v   domain.WorkflowVersion
		raw []byte
	)
	if err := row.Scan(&v.ID, &v.WorkflowID, &v.Version, &raw, &v.CreatedBy, &v.CreatedAt); err != nil {
		return domain.WorkflowVersion{}, translate(op, err)
	}
	if err := json.Unmarshal(raw, &v.Graph); err != nil {
		return domain.WorkflowVersion{}, fmt.Errorf("%s: decode graph: %w", op, err)
	}
	return v, nil
}

// prefixed qualifies a comma-separated column list with a table alias so a
// joined query can reuse the same constant.
func prefixed(alias, columns string) string {
	parts := strings.Split(columns, ", ")
	for i, p := range parts {
		parts[i] = alias + "." + p
	}
	return strings.Join(parts, ", ")
}
