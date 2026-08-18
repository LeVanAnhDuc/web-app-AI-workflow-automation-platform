package store

import (
	"context"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// defaultScheduleBatch caps DueSchedules when the caller passes no limit, so a
// backlog after downtime is drained in bounded ticks.
const defaultScheduleBatch = 100

// ReplaceWebhooks makes the webhooks table match the workflow's trigger nodes.
// Activation reconciles rather than diffs, so delete-then-insert in one
// transaction is both simpler and atomic: a duplicate path anywhere in the
// batch leaves the previous rows in place.
func (s *Store) ReplaceWebhooks(ctx context.Context, workspaceID, workflowID string, hooks []domain.Webhook) error {
	if !validIDs(workspaceID, workflowID) {
		return notFound("replace webhooks")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return translate("replace webhooks", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if _, err := tx.Exec(ctx,
		`DELETE FROM webhooks WHERE workflow_id = $1 AND workspace_id = $2`,
		workflowID, workspaceID,
	); err != nil {
		return translate("delete webhooks", err)
	}
	for _, h := range hooks {
		method := h.Method
		if method == "" {
			method = "POST"
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO webhooks (workspace_id, workflow_id, node_id, path, method)
			 VALUES ($1, $2, $3, $4, $5)`,
			workspaceID, workflowID, h.NodeID, h.Path, method,
		); err != nil {
			return translate("insert webhook", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return translate("commit webhooks", err)
	}
	return nil
}

// WebhookByPath resolves a public ingress path. It is not workspace-scoped
// because the caller is an anonymous request that only knows the path.
func (s *Store) WebhookByPath(ctx context.Context, path string) (domain.Webhook, error) {
	var h domain.Webhook
	err := s.pool.QueryRow(ctx,
		`SELECT id, workspace_id, workflow_id, node_id, path, method, created_at
		 FROM webhooks WHERE path = $1`, path,
	).Scan(&h.ID, &h.WorkspaceID, &h.WorkflowID, &h.NodeID, &h.Path, &h.Method, &h.CreatedAt)
	if err != nil {
		return domain.Webhook{}, translate("webhook by path", err)
	}
	return h, nil
}

// ReplaceSchedules makes the schedules table match the workflow's schedule
// trigger nodes, in one transaction for the same reason as ReplaceWebhooks.
func (s *Store) ReplaceSchedules(ctx context.Context, workspaceID, workflowID string, schedules []domain.Schedule) error {
	if !validIDs(workspaceID, workflowID) {
		return notFound("replace schedules")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return translate("replace schedules", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if _, err := tx.Exec(ctx,
		`DELETE FROM schedules WHERE workflow_id = $1 AND workspace_id = $2`,
		workflowID, workspaceID,
	); err != nil {
		return translate("delete schedules", err)
	}
	for _, sc := range schedules {
		tz := sc.Timezone
		if tz == "" {
			tz = "UTC"
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schedules (workspace_id, workflow_id, node_id, cron, timezone, next_run_at, last_run_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			workspaceID, workflowID, sc.NodeID, sc.Cron, tz, sc.NextRunAt, sc.LastRunAt,
		); err != nil {
			return translate("insert schedule", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return translate("commit schedules", err)
	}
	return nil
}

// DueSchedules returns the schedules the ticker should fire, oldest due first
// so a backlog is worked off in order.
func (s *Store) DueSchedules(ctx context.Context, now time.Time, limit int) ([]domain.Schedule, error) {
	if limit <= 0 {
		limit = defaultScheduleBatch
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, workspace_id, workflow_id, node_id, cron, timezone, next_run_at, last_run_at
		 FROM schedules WHERE next_run_at <= $1 ORDER BY next_run_at LIMIT $2`,
		now, limit)
	if err != nil {
		return nil, translate("due schedules", err)
	}
	defer rows.Close()

	var out []domain.Schedule
	for rows.Next() {
		var sc domain.Schedule
		if err := rows.Scan(&sc.ID, &sc.WorkspaceID, &sc.WorkflowID, &sc.NodeID,
			&sc.Cron, &sc.Timezone, &sc.NextRunAt, &sc.LastRunAt); err != nil {
			return nil, translate("scan schedule", err)
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, translate("due schedules", err)
	}
	return out, nil
}

// MarkScheduleRun advances a schedule after the ticker enqueued its run.
func (s *Store) MarkScheduleRun(ctx context.Context, id string, lastRun, nextRun time.Time) error {
	if !validIDs(id) {
		return notFound("mark schedule run")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE schedules SET last_run_at = $2, next_run_at = $3 WHERE id = $1`,
		id, lastRun, nextRun)
	if err != nil {
		return translate("mark schedule run", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("mark schedule run")
	}
	return nil
}
