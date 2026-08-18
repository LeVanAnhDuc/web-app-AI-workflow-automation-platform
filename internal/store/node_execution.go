package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

const nodeExecutionColumns = `id, execution_id, node_id, node_name, node_type, status,
       attempt, input, output, error, started_at, finished_at`

// ListNodeExecutions returns the per-node log of one run, in the order the
// engine settled the nodes; unstarted rows sort last.
func (s *Store) ListNodeExecutions(ctx context.Context, executionID string) ([]domain.NodeExecution, error) {
	if !validIDs(executionID) {
		return nil, notFound("list node executions")
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+nodeExecutionColumns+`
		 FROM node_executions WHERE execution_id = $1
		 ORDER BY started_at NULLS LAST, node_id`,
		executionID)
	if err != nil {
		return nil, translate("list node executions", err)
	}
	defer rows.Close()

	var out []domain.NodeExecution
	for rows.Next() {
		ne, err := scanNodeExecution(rows, "list node executions")
		if err != nil {
			return nil, err
		}
		out = append(out, ne)
	}
	if err := rows.Err(); err != nil {
		return nil, translate("list node executions", err)
	}
	return out, nil
}

// UpsertNodeExecution writes the state of one node in one run. The engine calls
// it twice per node — running, then settled — and again on a retry, so the
// unique (execution_id, node_id) key makes it an upsert rather than an insert.
func (s *Store) UpsertNodeExecution(ctx context.Context, ne domain.NodeExecution) (domain.NodeExecution, error) {
	if !validIDs(ne.ExecutionID) {
		return domain.NodeExecution{}, notFound("upsert node execution")
	}
	input, err := jsonbOrNull(ne.Input)
	if err != nil {
		return domain.NodeExecution{}, err
	}
	output, err := jsonbOrNull(ne.Output)
	if err != nil {
		return domain.NodeExecution{}, err
	}
	nodeErr, err := jsonbOrNull(ne.Error)
	if err != nil {
		return domain.NodeExecution{}, err
	}
	attempt := ne.Attempt
	if attempt < 1 {
		attempt = 1
	}

	return scanNodeExecution(s.pool.QueryRow(ctx,
		`INSERT INTO node_executions
		    (execution_id, node_id, node_name, node_type, status, attempt,
		     input, output, error, started_at, finished_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (execution_id, node_id) DO UPDATE SET
		    node_name   = EXCLUDED.node_name,
		    node_type   = EXCLUDED.node_type,
		    status      = EXCLUDED.status,
		    attempt     = EXCLUDED.attempt,
		    input       = EXCLUDED.input,
		    output      = EXCLUDED.output,
		    error       = EXCLUDED.error,
		    started_at  = EXCLUDED.started_at,
		    finished_at = EXCLUDED.finished_at
		 RETURNING `+nodeExecutionColumns,
		ne.ExecutionID, ne.NodeID, ne.NodeName, ne.NodeType, string(ne.Status), attempt,
		input, output, nodeErr, ne.StartedAt, ne.FinishedAt,
	), "upsert node execution")
}

// DeleteNodeExecutionsFrom drops the log of the given nodes so a retry re-runs
// them; every other node's stored output is what makes resume cheap.
func (s *Store) DeleteNodeExecutionsFrom(ctx context.Context, executionID string, nodeIDs []string) error {
	if len(nodeIDs) == 0 {
		return nil
	}
	if !validIDs(executionID) {
		return notFound("delete node executions")
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM node_executions WHERE execution_id = $1 AND node_id = ANY($2)`,
		executionID, nodeIDs,
	); err != nil {
		return translate("delete node executions", err)
	}
	return nil
}

func scanNodeExecution(row pgx.Row, op string) (domain.NodeExecution, error) {
	var (
		ne                          domain.NodeExecution
		status                      string
		inputRaw, outputRaw, errRaw []byte
	)
	err := row.Scan(&ne.ID, &ne.ExecutionID, &ne.NodeID, &ne.NodeName, &ne.NodeType, &status,
		&ne.Attempt, &inputRaw, &outputRaw, &errRaw, &ne.StartedAt, &ne.FinishedAt)
	if err != nil {
		return domain.NodeExecution{}, translate(op, err)
	}
	ne.Status = domain.Status(status)
	if ne.Input, err = domain.ItemsFromJSON(inputRaw); err != nil {
		return domain.NodeExecution{}, fmt.Errorf("%s: decode input: %w", op, err)
	}
	if ne.Output, err = outputsFromJSON(outputRaw); err != nil {
		return domain.NodeExecution{}, fmt.Errorf("%s: %w", op, err)
	}
	if ne.Error, err = nodeErrorFromJSON(errRaw); err != nil {
		return domain.NodeExecution{}, fmt.Errorf("%s: %w", op, err)
	}
	return ne, nil
}
