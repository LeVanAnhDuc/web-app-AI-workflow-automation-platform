package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// timeNow is a package-level seam so handler tests can freeze the clock.
var timeNow = time.Now

const (
	streamPollInterval = 500 * time.Millisecond
	streamKeepAlive    = 20 * time.Second
	streamMaxDuration  = 10 * time.Minute
)

// handleExecutionStream streams an execution's progress as server-sent events.
//
// It polls the two tables and emits what changed rather than subscribing to an
// in-process bus, because the engine runs in a different process: polling is
// the one approach that needs no extra infrastructure and still works across
// that boundary. 500 ms is imperceptible next to a node that does real work.
func (s *server) handleExecutionStream(w http.ResponseWriter, r *http.Request) {
	ws := workspaceOf(r)
	id := chi.URLParam(r, "id")

	// Fail before the stream opens, so the client sees a real status code.
	if _, err := s.store.Execution(r.Context(), ws, id); err != nil {
		writeStoreErr(w, err, "That execution does not exist.")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, domain.ErrCodeInternal, "Streaming is not supported here.")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	// Defeats proxy buffering, which otherwise holds events until the response
	// ends and makes the live log look broken.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	ticker := time.NewTicker(streamPollInterval)
	defer ticker.Stop()
	keepAlive := time.NewTicker(streamKeepAlive)
	defer keepAlive.Stop()
	deadline := time.After(streamMaxDuration)

	var lastExecution string
	nodeFingerprints := make(map[string]string)

	emit := func(event string, payload any) bool {
		raw, err := json.Marshal(payload)
		if err != nil {
			s.log.Error("api: marshal stream event", "error", err, "event", event)
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	poll := func() (done bool, alive bool) {
		exec, err := s.store.Execution(ctx, ws, id)
		if err != nil {
			return true, true
		}

		nodeExecs, err := s.store.ListNodeExecutions(ctx, exec.ID)
		if err == nil {
			for _, n := range nodeExecs {
				fp := nodeFingerprint(n)
				if nodeFingerprints[n.NodeID] == fp {
					continue
				}
				nodeFingerprints[n.NodeID] = fp
				if !emit("node", map[string]any{"type": "node", "node": viewNodeExecution(n)}) {
					return false, false
				}
			}
		}

		if fp := executionFingerprint(exec); fp != lastExecution {
			lastExecution = fp
			if !emit("execution", map[string]any{"type": "execution", "execution": viewExecution(exec)}) {
				return false, false
			}
		}

		return exec.Status.Terminal(), true
	}

	// Emit the current state at once so a client that connects late is not
	// staring at an empty log until the next node finishes.
	if finished, alive := poll(); !alive {
		return
	} else if finished {
		emit("done", map[string]any{"type": "done"})
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			emit("done", map[string]any{"type": "done"})
			return
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			finished, alive := poll()
			if !alive {
				return
			}
			if finished {
				emit("done", map[string]any{"type": "done"})
				return
			}
		}
	}
}

// nodeFingerprint changes whenever anything a client renders changes, so an
// unchanged row is never re-sent.
func nodeFingerprint(n domain.NodeExecution) string {
	finished := ""
	if n.FinishedAt != nil {
		finished = n.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	errMsg := ""
	if n.Error != nil {
		errMsg = n.Error.Code + ":" + n.Error.Message
	}
	return fmt.Sprintf("%s|%d|%s|%d|%s", n.Status, n.Attempt, finished, n.ItemCount(), errMsg)
}

func executionFingerprint(e domain.Execution) string {
	finished := ""
	if e.FinishedAt != nil {
		finished = e.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	errMsg := ""
	if e.Error != nil {
		errMsg = e.Error.Code + ":" + e.Error.Message
	}
	return fmt.Sprintf("%s|%s|%s", e.Status, finished, errMsg)
}
