package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/store"
)

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testKindPrefix marks the jobs these tests create. Claim is deliberately
// kind-agnostic, so a test only ever asserts on rows carrying this prefix and
// the helper clears leftovers from an interrupted run instead of the table.
const testKindPrefix = "test."

// testQueue connects to the database named by TEST_DATABASE_URL, skipping the
// Postgres half of the suite when it is unset.
func testQueue(t *testing.T) *Queue {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run queue tests")
	}
	migrateOnce.Do(func() { migrateErr = migrateForTests(dsn) })
	require.NoError(t, migrateErr, "apply migrations")

	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	_, err = pool.Exec(context.Background(),
		`DELETE FROM jobs WHERE kind LIKE $1`, testKindPrefix+"%")
	require.NoError(t, err)

	return New(pool, "worker-"+uuid.NewString())
}

// schemaLockKey serialises migrations between test binaries: `go test ./...`
// runs this package and internal/store in parallel, and two gooses creating
// their version table at the same instant collide.
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

	return store.Migrate(ctx, dsn, os.DirFS(filepath.Join("..", "..", "db", "migrations")), ".")
}

// jobRow reads the columns the tests assert on.
func jobRow(t *testing.T, q *Queue, id int64) (status string, attempt int, runAt time.Time, lastError *string) {
	t.Helper()
	err := q.pool.QueryRow(context.Background(),
		`SELECT status, attempt, run_at, last_error FROM jobs WHERE id = $1`, id,
	).Scan(&status, &attempt, &runAt, &lastError)
	require.NoError(t, err)
	return status, attempt, runAt, lastError
}

// claimOurs claims a batch and returns only the job with the given id, so a
// stray row in a shared database cannot make an assertion flaky.
func claimOurs(t *testing.T, q *Queue, id int64) (Job, bool) {
	t.Helper()
	jobs, err := q.Claim(context.Background(), 20)
	require.NoError(t, err)
	for _, j := range jobs {
		if j.ID == id {
			return j, true
		}
	}
	return Job{}, false
}

func TestClaimThenComplete(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	executionID := uuid.NewString()

	id, err := q.Enqueue(ctx, testKindPrefix+KindExecution, ExecutionPayload{ExecutionID: executionID})
	require.NoError(t, err)

	job, ok := claimOurs(t, q, id)
	require.True(t, ok, "a due job must be claimable")
	assert.Equal(t, 1, job.Attempt, "claiming spends an attempt so a dead worker cannot retry forever")
	assert.Equal(t, 3, job.MaxAttempts)

	var payload ExecutionPayload
	require.NoError(t, job.Decode(&payload))
	assert.Equal(t, executionID, payload.ExecutionID)

	status, _, _, _ := jobRow(t, q, id)
	assert.Equal(t, StatusRunning, status)

	require.NoError(t, q.Complete(ctx, id))
	status, _, _, _ = jobRow(t, q, id)
	assert.Equal(t, StatusDone, status)

	_, ok = claimOurs(t, q, id)
	assert.False(t, ok, "a completed job is never claimed again")
}

func TestEnqueueAtHoldsJobUntilDue(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	id, err := q.EnqueueAt(ctx, testKindPrefix+"future", ExecutionPayload{ExecutionID: uuid.NewString()},
		time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, ok := claimOurs(t, q, id)
	assert.False(t, ok, "a job scheduled for later is not due yet")
}

func TestFailSchedulesBackoffRetry(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	id, err := q.Enqueue(ctx, testKindPrefix+"retry", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)
	_, ok := claimOurs(t, q, id)
	require.True(t, ok)

	before := time.Now()
	require.NoError(t, q.Fail(ctx, id, errors.New("upstream timed out")))

	status, attempt, runAt, lastError := jobRow(t, q, id)
	assert.Equal(t, StatusPending, status, "an attempt is left, so the job goes back to the queue")
	assert.Equal(t, 1, attempt)
	require.NotNil(t, lastError)
	assert.Equal(t, "upstream timed out", *lastError)
	assert.WithinDuration(t, before.Add(Backoff(1)), runAt, 2*time.Second)

	_, ok = claimOurs(t, q, id)
	assert.False(t, ok, "the backoff must keep the job out of the next claim")
}

func TestFailMarksJobDeadAfterLastAttempt(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	id, err := q.Enqueue(ctx, testKindPrefix+"dead", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)
	_, err = q.pool.Exec(ctx, `UPDATE jobs SET max_attempts = 1 WHERE id = $1`, id)
	require.NoError(t, err)

	_, ok := claimOurs(t, q, id)
	require.True(t, ok)
	require.NoError(t, q.Fail(ctx, id, errors.New("permanently broken")))

	status, attempt, _, lastError := jobRow(t, q, id)
	assert.Equal(t, StatusDead, status)
	assert.Equal(t, 1, attempt)
	require.NotNil(t, lastError)
	assert.Equal(t, "permanently broken", *lastError)

	_, ok = claimOurs(t, q, id)
	assert.False(t, ok, "a dead job is never retried")
}

func TestReleaseStaleRequeuesCrashedWorkerJobs(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	id, err := q.Enqueue(ctx, testKindPrefix+"stale", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)
	_, ok := claimOurs(t, q, id)
	require.True(t, ok)

	// Simulate the worker dying while holding the lock.
	_, err = q.pool.Exec(ctx, `UPDATE jobs SET locked_at = now() - interval '1 hour' WHERE id = $1`, id)
	require.NoError(t, err)

	released, err := q.ReleaseStale(ctx, time.Minute)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, released, 1)

	status, _, _, _ := jobRow(t, q, id)
	assert.Equal(t, StatusPending, status)

	_, ok = claimOurs(t, q, id)
	assert.True(t, ok, "a released job is claimable again")
}

func TestWorkRunsAndSettlesJobs(t *testing.T) {
	q := testQueue(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	okID, err := q.Enqueue(ctx, testKindPrefix+"work-ok", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)
	badID, err := q.Enqueue(ctx, testKindPrefix+"work-fail", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)

	handled := make(chan int64, 8)
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- q.Work(workerCtx, 2, 20*time.Millisecond, func(_ context.Context, j Job) error {
			handled <- j.ID
			if j.ID == badID {
				return errors.New("handler said no")
			}
			return nil
		})
	}()

	seen := map[int64]bool{}
	for len(seen) < 2 {
		select {
		case id := <-handled:
			seen[id] = true
		case <-ctx.Done():
			t.Fatal("Work did not run both jobs in time")
		}
	}
	stop()
	require.NoError(t, <-done, "Work returns cleanly once its context is cancelled")

	// Work waits for its in-flight jobs, and their bookkeeping runs on a context
	// that outlives cancellation, so both rows are already settled here.
	okStatus, _, _, _ := jobRow(t, q, okID)
	assert.Equal(t, StatusDone, okStatus)
	badStatus, _, _, badError := jobRow(t, q, badID)
	assert.Equal(t, StatusPending, badStatus, "a failed handler leaves the job for another attempt")
	require.NotNil(t, badError)
	assert.Equal(t, "handler said no", *badError)
}
