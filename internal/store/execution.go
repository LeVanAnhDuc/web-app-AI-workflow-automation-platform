package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// executionSelect joins the workflow name and version number the history
// screens display, so a listing never needs a second query per row.
const executionSelect = `
SELECT e.id, e.workspace_id, e.workflow_id, w.name, e.workflow_version_id, v.version,
       e.status, e.trigger_type, e.trigger_data, e.error, e.resume_from_node,
       e.cancel_requested, e.created_at, e.started_at, e.finished_at
FROM executions e
JOIN workflows w         ON w.id = e.workflow_id
JOIN workflow_versions v ON v.id = e.workflow_version_id`

// Default and maximum page size of ListExecutions.
const (
	defaultExecutionLimit = 25
	maxExecutionLimit     = 100
)

// CreateExecution records a queued run. started_at stays NULL: the engine sets
// it when a worker actually picks the job up, so queue wait is visible.
func (s *Store) CreateExecution(ctx context.Context, in domain.NewExecution) (domain.Execution, error) {
	if !validIDs(in.WorkspaceID, in.WorkflowID, in.WorkflowVersionID) {
		return domain.Execution{}, notFound("create execution")
	}
	triggerData, err := jsonbOrNull(in.TriggerData)
	if err != nil {
		return domain.Execution{}, err
	}
	status := in.Status
	if status == "" {
		status = domain.StatusQueued
	}

	var id string
	err = s.pool.QueryRow(ctx,
		`INSERT INTO executions
		    (workspace_id, workflow_id, workflow_version_id, status, trigger_type,
		     trigger_data, resume_from_node, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		 RETURNING id`,
		in.WorkspaceID, in.WorkflowID, in.WorkflowVersionID, status, in.TriggerType,
		triggerData, in.ResumeFromNode,
	).Scan(&id)
	if err != nil {
		return domain.Execution{}, translate("create execution", err)
	}
	return s.ExecutionByID(ctx, id)
}

// Execution loads one run inside a workspace.
func (s *Store) Execution(ctx context.Context, workspaceID, id string) (domain.Execution, error) {
	if !validIDs(workspaceID, id) {
		return domain.Execution{}, notFound("execution")
	}
	return scanExecution(s.pool.QueryRow(ctx,
		executionSelect+` WHERE e.id = $1 AND e.workspace_id = $2`, id, workspaceID,
	), "execution")
}

// ExecutionByID loads a run without a workspace filter, for the worker that
// only ever knows the execution id it claimed from the queue.
func (s *Store) ExecutionByID(ctx context.Context, id string) (domain.Execution, error) {
	if !validIDs(id) {
		return domain.Execution{}, notFound("execution by id")
	}
	return scanExecution(s.pool.QueryRow(ctx,
		executionSelect+` WHERE e.id = $1`, id,
	), "execution by id")
}

// ListExecutions returns one keyset-paginated page plus the cursor of the next
// one, empty when this page was the last. Keyset rather than OFFSET because the
// list grows at the head while a user pages through it.
func (s *Store) ListExecutions(ctx context.Context, workspaceID string, f domain.ExecutionFilter) ([]domain.Execution, string, error) {
	if !validIDs(workspaceID) {
		return nil, "", notFound("list executions")
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultExecutionLimit
	}
	if limit > maxExecutionLimit {
		limit = maxExecutionLimit
	}

	query := executionSelect + ` WHERE e.workspace_id = $1`
	args := []any{workspaceID}
	if f.WorkflowID != "" {
		if !validIDs(f.WorkflowID) {
			return nil, "", nil // an unknown workflow simply has no executions
		}
		args = append(args, f.WorkflowID)
		query += fmt.Sprintf(` AND e.workflow_id = $%d`, len(args))
	}
	if f.Status != "" {
		args = append(args, string(f.Status))
		query += fmt.Sprintf(` AND e.status = $%d`, len(args))
	}
	if f.Cursor != "" {
		at, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", err
		}
		args = append(args, at, id)
		query += fmt.Sprintf(` AND (e.created_at, e.id) < ($%d::timestamptz, $%d::uuid)`, len(args)-1, len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY e.created_at DESC, e.id DESC LIMIT $%d`, len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", translate("list executions", err)
	}
	defer rows.Close()

	var out []domain.Execution
	for rows.Next() {
		e, err := scanExecution(rows, "list executions")
		if err != nil {
			return nil, "", err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", translate("list executions", err)
	}

	// A short page proves there is nothing after it, so no cursor is offered.
	var next string
	if len(out) == limit {
		last := out[len(out)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	return out, next, nil
}

// UpdateExecution applies the non-nil fields of p. The engine calls it on every
// lifecycle transition, so an empty patch is a cheap no-op rather than an error.
func (s *Store) UpdateExecution(ctx context.Context, id string, p domain.ExecutionPatch) error {
	if !validIDs(id) {
		return notFound("update execution")
	}

	var (
		sets []string
		args = []any{id}
	)
	if p.Status != nil {
		args = append(args, string(*p.Status))
		sets = append(sets, fmt.Sprintf("status = $%d", len(args)))
	}
	switch {
	case p.ClearError:
		sets = append(sets, "error = NULL")
	case p.Error != nil:
		raw, err := jsonbOrNull(p.Error)
		if err != nil {
			return err
		}
		args = append(args, raw)
		sets = append(sets, fmt.Sprintf("error = $%d", len(args)))
	}
	if p.StartedAt != nil {
		args = append(args, *p.StartedAt)
		sets = append(sets, fmt.Sprintf("started_at = $%d", len(args)))
	}
	if p.FinishedAt != nil {
		args = append(args, *p.FinishedAt)
		sets = append(sets, fmt.Sprintf("finished_at = $%d", len(args)))
	}
	if len(sets) == 0 {
		return nil
	}

	tag, err := s.pool.Exec(ctx,
		`UPDATE executions SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
	if err != nil {
		return translate("update execution", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("update execution")
	}
	return nil
}

// RequestCancel raises the cooperative cancel flag. The engine reads it between
// nodes, so a terminal execution keeps the flag with no effect.
func (s *Store) RequestCancel(ctx context.Context, workspaceID, id string) error {
	if !validIDs(workspaceID, id) {
		return notFound("request cancel")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE executions SET cancel_requested = true WHERE id = $1 AND workspace_id = $2`,
		id, workspaceID)
	if err != nil {
		return translate("request cancel", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("request cancel")
	}
	return nil
}

// IsCancelRequested reports whether the run should stop at the next node
// boundary.
func (s *Store) IsCancelRequested(ctx context.Context, id string) (bool, error) {
	if !validIDs(id) {
		return false, notFound("cancel flag")
	}
	var requested bool
	if err := s.pool.QueryRow(ctx,
		`SELECT cancel_requested FROM executions WHERE id = $1`, id,
	).Scan(&requested); err != nil {
		return false, translate("cancel flag", err)
	}
	return requested, nil
}

func scanExecution(row pgx.Row, op string) (domain.Execution, error) {
	var (
		e               domain.Execution
		triggerRaw      []byte
		errRaw          []byte
		status, trigger string
	)
	err := row.Scan(&e.ID, &e.WorkspaceID, &e.WorkflowID, &e.WorkflowName, &e.WorkflowVersionID, &e.Version,
		&status, &trigger, &triggerRaw, &errRaw, &e.ResumeFromNode,
		&e.CancelRequested, &e.CreatedAt, &e.StartedAt, &e.FinishedAt)
	if err != nil {
		return domain.Execution{}, translate(op, err)
	}
	e.Status = domain.Status(status)
	e.TriggerType = domain.TriggerType(trigger)
	if e.TriggerData, err = domain.ItemsFromJSON(triggerRaw); err != nil {
		return domain.Execution{}, fmt.Errorf("%s: decode trigger data: %w", op, err)
	}
	if e.Error, err = nodeErrorFromJSON(errRaw); err != nil {
		return domain.Execution{}, fmt.Errorf("%s: %w", op, err)
	}
	return e, nil
}

// cursorSeparator splits the two keyset components inside the encoded cursor.
const cursorSeparator = "|"

// encodeCursor packs the keyset position of a row. It is base64 of
// "<createdAt>|<id>" so it survives a query string and stays opaque to clients.
func encodeCursor(createdAt time.Time, id string) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + cursorSeparator + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor unpacks a cursor produced by encodeCursor.
func decodeCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		// Tolerate the padded alphabet in case a client round-tripped it.
		raw, err = base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			return time.Time{}, "", fmt.Errorf("decode cursor: %w", err)
		}
	}
	at, id, ok := strings.Cut(string(raw), cursorSeparator)
	if !ok {
		return time.Time{}, "", errors.New("decode cursor: missing separator")
	}
	ts, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("decode cursor timestamp: %w", err)
	}
	if !validIDs(id) {
		return time.Time{}, "", errors.New("decode cursor: id is not a uuid")
	}
	return ts, id, nil
}
