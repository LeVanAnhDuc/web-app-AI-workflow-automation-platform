// Package queue is the job queue, backed by the jobs table rather than an
// external broker: claiming with FOR UPDATE SKIP LOCKED gives at-least-once
// delivery across processes with no infrastructure beyond the database the
// application already needs.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// KindExecution is the only job kind Phase 1 enqueues: run one execution.
const KindExecution = "execution.run"

// Job status values as stored in the jobs table.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusDead    = "dead"
)

// maxLastError caps what is written to jobs.last_error; a stack-sized message
// would bloat the table without helping anyone debug.
const maxLastError = 2000

// ExecutionPayload is the payload of a KindExecution job.
type ExecutionPayload struct {
	ExecutionID string `json:"executionId"`
}

// Job is one claimed row of the jobs table.
type Job struct {
	ID          int64
	Kind        string
	Payload     json.RawMessage
	Attempt     int
	MaxAttempts int
	RunAt       time.Time
}

// Decode unmarshals the payload into v.
func (j Job) Decode(v any) error {
	if err := json.Unmarshal(j.Payload, v); err != nil {
		return fmt.Errorf("decode %s payload: %w", j.Kind, err)
	}
	return nil
}

// Queue enqueues and claims jobs. workerID is stamped on claimed rows so a
// stuck job can be traced back to the process that took it.
type Queue struct {
	pool     *pgxpool.Pool
	workerID string
}

// New returns a queue over an existing pool, shared with the store.
func New(pool *pgxpool.Pool, workerID string) *Queue {
	return &Queue{pool: pool, workerID: workerID}
}

// Enqueue schedules a job to run as soon as a worker is free.
func (q *Queue) Enqueue(ctx context.Context, kind string, payload any) (int64, error) {
	return q.EnqueueAt(ctx, kind, payload, time.Now())
}

// EnqueueAt schedules a job for a point in time, which the schedule ticker uses
// to place a run without holding it in memory.
func (q *Queue) EnqueueAt(ctx context.Context, kind string, payload any, runAt time.Time) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode %s payload: %w", kind, err)
	}
	var id int64
	if err := q.pool.QueryRow(ctx,
		`INSERT INTO jobs (kind, payload, run_at) VALUES ($1, $2, $3) RETURNING id`,
		kind, raw, runAt,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("enqueue %s: %w", kind, err)
	}
	return id, nil
}

// claimQuery marks up to $2 due jobs as running in one statement. SKIP LOCKED
// lets several workers claim concurrently without blocking each other, and the
// attempt counter is raised on claim so a worker that dies mid-job still
// consumes one of its attempts.
const claimQuery = `
UPDATE jobs SET status = 'running', attempt = attempt + 1, locked_at = now(), locked_by = $1
WHERE id IN (
    SELECT id FROM jobs
    WHERE status = 'pending' AND run_at <= now()
    ORDER BY run_at
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
RETURNING id, kind, payload, attempt, max_attempts, run_at`

// Claim takes up to limit due jobs and marks them running.
func (q *Queue) Claim(ctx context.Context, limit int) ([]Job, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := q.pool.Query(ctx, claimQuery, q.workerID, limit)
	if err != nil {
		return nil, fmt.Errorf("claim jobs: %w", err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Kind, &j.Payload, &j.Attempt, &j.MaxAttempts, &j.RunAt); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim jobs: %w", err)
	}
	return out, nil
}

// Complete retires a job that succeeded.
func (q *Queue) Complete(ctx context.Context, id int64) error {
	if _, err := q.pool.Exec(ctx,
		`UPDATE jobs SET status = $2, locked_at = NULL, locked_by = NULL WHERE id = $1`,
		id, StatusDone,
	); err != nil {
		return fmt.Errorf("complete job %d: %w", id, err)
	}
	return nil
}

// Fail records the cause and either schedules a backoff retry or gives up. The
// attempt count is read under a row lock because the backoff is computed in Go.
func (q *Queue) Fail(ctx context.Context, id int64, cause error) error {
	message := ""
	if cause != nil {
		message = truncate(cause.Error(), maxLastError)
	}

	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("fail job %d: %w", id, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var attempt, maxAttempts int
	if err := tx.QueryRow(ctx,
		`SELECT attempt, max_attempts FROM jobs WHERE id = $1 FOR UPDATE`, id,
	).Scan(&attempt, &maxAttempts); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("fail job %d: no such job", id)
		}
		return fmt.Errorf("fail job %d: %w", id, err)
	}

	if attempt < maxAttempts {
		delay := Backoff(attempt)
		_, err = tx.Exec(ctx,
			`UPDATE jobs SET status = $2, run_at = now() + make_interval(secs => $3),
			        locked_at = NULL, locked_by = NULL, last_error = $4
			 WHERE id = $1`,
			id, StatusPending, delay.Seconds(), message)
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE jobs SET status = $2, locked_at = NULL, locked_by = NULL, last_error = $3
			 WHERE id = $1`,
			id, StatusDead, message)
	}
	if err != nil {
		return fmt.Errorf("fail job %d: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("fail job %d: %w", id, err)
	}
	return nil
}

// ReleaseStale returns jobs still marked running past the deadline to pending,
// which is how a crashed worker's jobs get picked up again. It is safe because
// the engine skips nodes that already succeeded.
func (q *Queue) ReleaseStale(ctx context.Context, olderThan time.Duration) (int, error) {
	tag, err := q.pool.Exec(ctx,
		`UPDATE jobs SET status = $1, locked_at = NULL, locked_by = NULL
		 WHERE status = 'running' AND locked_at < now() - make_interval(secs => $2)`,
		StatusPending, olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("release stale jobs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// Handler runs one job. Returning an error hands the job back to Fail.
type Handler func(ctx context.Context, j Job) error

// Work polls for jobs and runs up to concurrency of them at a time until ctx is
// done, then waits for the in-flight ones and returns nil. Transient claim
// errors are logged and retried on the next tick rather than killing the
// worker, since a database blip must not take the process down.
func (q *Queue) Work(ctx context.Context, concurrency int, pollInterval time.Duration, h Handler) error {
	if concurrency < 1 {
		concurrency = 1
	}
	if pollInterval <= 0 {
		pollInterval = time.Second
	}

	slots := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			wg.Wait()
			return nil
		}

		free := cap(slots) - len(slots)
		claimed := 0
		if free > 0 {
			jobs, err := q.Claim(ctx, free)
			if err != nil {
				if ctx.Err() != nil {
					wg.Wait()
					return nil
				}
				slog.Error("claim jobs", "error", err)
			}
			claimed = len(jobs)
			for _, j := range jobs {
				slots <- struct{}{}
				wg.Add(1)
				go func(j Job) {
					defer wg.Done()
					defer func() { <-slots }()
					q.runJob(ctx, j, h)
				}(j)
			}
		}

		// A full batch means there is probably more work waiting; skip the tick.
		if claimed > 0 && claimed == free {
			continue
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-ticker.C:
		}
	}
}

// runJob executes one job and records the outcome, recovering a panic in the
// handler so one bad job cannot take the worker down.
func (q *Queue) runJob(ctx context.Context, j Job, h Handler) {
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic in %s handler: %v", j.Kind, r)
			}
		}()
		return h(ctx, j)
	}()

	// Shutdown cancels the job context, so bookkeeping needs one that outlives
	// it — otherwise a cancelled job would stay marked running until
	// ReleaseStale notices.
	bookkeep, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err != nil {
		if failErr := q.Fail(bookkeep, j.ID, err); failErr != nil {
			slog.Error("record job failure", "job", j.ID, "error", failErr)
		}
		return
	}
	if doneErr := q.Complete(bookkeep, j.ID); doneErr != nil {
		slog.Error("record job completion", "job", j.ID, "error", doneErr)
	}
}

// maxBackoff caps the retry delay: beyond five minutes a human is the better
// escalation path than another automatic attempt.
const maxBackoff = 5 * time.Minute

// Backoff is the delay before retrying an attempt: 1s, 2s, 4s, 8s … capped.
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// Guard the shift itself, not just its result, against overflow.
	if attempt > 20 {
		return maxBackoff
	}
	d := time.Second << (attempt - 1)
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// truncate shortens s to at most n bytes, on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8StartsRune(s[cut]) {
		cut--
	}
	return s[:cut]
}

// utf8StartsRune reports whether b is not a UTF-8 continuation byte.
func utf8StartsRune(b byte) bool { return b&0xC0 != 0x80 }
