// Command worker claims queued executions and runs them, and ticks the cron
// schedules that create new ones. Splitting it from the API means a workflow
// that hammers an external service cannot slow down the editor.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/config"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/engine"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/llm"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/queue"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/schedule"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/store"
)

const (
	jobPollInterval  = 500 * time.Millisecond
	schedulePoll     = 10 * time.Second
	staleJobDeadline = 10 * time.Minute
)

func main() {
	if err := run(); err != nil {
		slog.Default().Error("worker: fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer st.Close()

	workerID := workerIdentity()
	q := queue.New(st.Pool(), workerID)

	eng := engine.New(st, nodes.Default(), engine.Options{
		HTTPClient: &http.Client{
			// A node has its own per-node timeout; this is the backstop for a
			// server that accepts the connection and then goes silent.
			Timeout: 2 * time.Minute,
		},
		Logger:           log,
		ExecutionTimeout: cfg.ExecutionTimeout,
		LLM:              llm.FromAPIKey(cfg.AnthropicAPIKey, log),
	})

	go tickSchedules(ctx, st, q, log)
	go releaseStaleJobs(ctx, q, log)

	log.Info("worker: started", "id", workerID, "concurrency", cfg.WorkerConcurrency)

	// Claiming only the kinds this binary understands means a job kind added by
	// a newer deployment waits for a worker that can run it, rather than being
	// taken and discarded by this one.
	return q.Work(ctx, cfg.WorkerConcurrency, jobPollInterval, func(ctx context.Context, job queue.Job) error {
		var payload queue.ExecutionPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			log.Error("worker: unreadable job payload", "job", job.ID, "error", err)
			return nil
		}

		log.Info("worker: running execution", "execution", payload.ExecutionID, "attempt", job.Attempt)
		if err := eng.Run(ctx, payload.ExecutionID); err != nil {
			// Engine.Run only errors on problems worth retrying — a workflow
			// that fails on a node is a completed run, not a failed job.
			return fmt.Errorf("run execution %s: %w", payload.ExecutionID, err)
		}
		return nil
	}, queue.KindExecution)
}

// tickSchedules turns due cron schedules into executions. Every worker runs
// this loop; the queue's own claim semantics plus the schedule row's
// next_run_at advance make a duplicate enqueue harmless in practice, and one
// worker is the normal deployment for Phase 1.
func tickSchedules(ctx context.Context, st *store.Store, q *queue.Queue, log *slog.Logger) {
	ticker := time.NewTicker(schedulePoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			due, err := st.DueSchedules(ctx, now, 50)
			if err != nil {
				log.Error("worker: list due schedules", "error", err)
				continue
			}
			for _, sched := range due {
				if err := fireSchedule(ctx, st, q, sched, now); err != nil {
					log.Error("worker: fire schedule", "error", err, "schedule", sched.ID)
				}
			}
		}
	}
}

func fireSchedule(ctx context.Context, st *store.Store, q *queue.Queue, sched domain.Schedule, now time.Time) error {
	next, err := schedule.NextRun(sched.Cron, sched.Timezone, now)
	if err != nil {
		// Advance past a bad expression so one broken schedule cannot spin the
		// ticker; the workflow's next save will fix or remove it.
		if markErr := st.MarkScheduleRun(ctx, sched.ID, now, now.Add(time.Hour)); markErr != nil {
			return markErr
		}
		return err
	}

	// Claim the slot first: if creating the execution fails we would rather
	// skip a tick than fire the same minute repeatedly.
	if err := st.MarkScheduleRun(ctx, sched.ID, now, next); err != nil {
		return err
	}

	version, err := st.LatestVersion(ctx, sched.WorkspaceID, sched.WorkflowID)
	if err != nil {
		return err
	}

	exec, err := st.CreateExecution(ctx, domain.NewExecution{
		WorkspaceID:       sched.WorkspaceID,
		WorkflowID:        sched.WorkflowID,
		WorkflowVersionID: version.ID,
		Status:            domain.StatusQueued,
		TriggerType:       domain.TriggerSchedule,
		TriggerData: []domain.Item{domain.NewItem(map[string]any{
			"timestamp": now.UTC().Format(time.RFC3339),
			"scheduled": true,
		})},
	})
	if err != nil {
		return err
	}

	_, err = q.Enqueue(ctx, queue.KindExecution, queue.ExecutionPayload{ExecutionID: exec.ID})
	return err
}

// releaseStaleJobs returns jobs abandoned by a crashed worker to the queue. The
// engine skips already-succeeded nodes, so re-running one is safe.
func releaseStaleJobs(ctx context.Context, q *queue.Queue, log *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := q.ReleaseStale(ctx, staleJobDeadline)
			if err != nil {
				log.Error("worker: release stale jobs", "error", err)
				continue
			}
			if n > 0 {
				log.Warn("worker: returned abandoned jobs to the queue", "count", n)
			}
		}
	}
}

func workerIdentity() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return fmt.Sprintf("%s/%d", host, os.Getpid())
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
