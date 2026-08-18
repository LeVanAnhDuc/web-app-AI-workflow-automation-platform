package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/queue"
)

// executionView adds the derived duration the tables show, which is computed
// rather than stored.
type executionView struct {
	domain.Execution
	DurationMs *int64 `json:"durationMs,omitempty"`
}

type nodeExecutionView struct {
	domain.NodeExecution
	DurationMs *int64 `json:"durationMs,omitempty"`
}

func viewExecution(e domain.Execution) executionView {
	return executionView{Execution: e, DurationMs: e.DurationMs()}
}

func viewNodeExecution(n domain.NodeExecution) nodeExecutionView {
	return nodeExecutionView{NodeExecution: n, DurationMs: n.DurationMs()}
}

func (s *server) handleListExecutions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 25
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}

	list, next, err := s.store.ListExecutions(r.Context(), workspaceOf(r), domain.ExecutionFilter{
		WorkflowID: q.Get("workflowId"),
		Status:     domain.Status(q.Get("status")),
		Limit:      limit,
		Cursor:     q.Get("cursor"),
	})
	if err != nil {
		writeStoreErr(w, err, "No executions found.")
		return
	}

	views := make([]executionView, 0, len(list))
	for _, e := range list {
		views = append(views, viewExecution(e))
	}

	body := map[string]any{"executions": views}
	if next != "" {
		body["nextCursor"] = next
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *server) handleGetExecution(w http.ResponseWriter, r *http.Request) {
	ws := workspaceOf(r)
	id := chi.URLParam(r, "id")

	exec, err := s.store.Execution(r.Context(), ws, id)
	if err != nil {
		writeStoreErr(w, err, "That execution does not exist.")
		return
	}
	nodeExecs, err := s.store.ListNodeExecutions(r.Context(), exec.ID)
	if err != nil {
		writeStoreErr(w, err, "That execution has no recorded nodes.")
		return
	}
	// The graph comes from the version that actually ran, not the workflow's
	// current draft — that is the whole point of versioning executions.
	version, err := s.store.Version(r.Context(), exec.WorkflowVersionID)
	if err != nil {
		writeStoreErr(w, err, "The version this execution ran is no longer available.")
		return
	}

	views := make([]nodeExecutionView, 0, len(nodeExecs))
	for _, n := range nodeExecs {
		views = append(views, viewNodeExecution(n))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"execution":      viewExecution(exec),
		"nodeExecutions": views,
		"graph":          version.Graph,
	})
}

func (s *server) handleCancelExecution(w http.ResponseWriter, r *http.Request) {
	ws := workspaceOf(r)
	id := chi.URLParam(r, "id")

	exec, err := s.store.Execution(r.Context(), ws, id)
	if err != nil {
		writeStoreErr(w, err, "That execution does not exist.")
		return
	}
	if exec.Status.Terminal() {
		writeErr(w, http.StatusConflict, "already_finished", "That execution has already finished.")
		return
	}

	// Cancellation is cooperative: the flag is set here and the engine notices
	// between nodes. Killing a node mid-HTTP-call would leave the remote side
	// in an unknown state.
	if err := s.store.RequestCancel(r.Context(), ws, id); err != nil {
		writeStoreErr(w, err, "That execution does not exist.")
		return
	}

	// A queued execution has no worker to notice the flag, so finish it here.
	if exec.Status == domain.StatusQueued {
		status := domain.StatusCancelled
		now := timeNow()
		if err := s.store.UpdateExecution(r.Context(), id, domain.ExecutionPatch{
			Status:     &status,
			FinishedAt: &now,
		}); err != nil {
			writeStoreErr(w, err, "That execution does not exist.")
			return
		}
	}

	updated, err := s.store.Execution(r.Context(), ws, id)
	if err != nil {
		writeStoreErr(w, err, "That execution does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"execution": viewExecution(updated)})
}

// handleRetryExecution starts a fresh execution that resumes at the node which
// failed. The succeeded nodes are copied across, so the engine rehydrates their
// output and never re-issues their side effects — the reason the design
// persists output after every node.
func (s *server) handleRetryExecution(w http.ResponseWriter, r *http.Request) {
	ws := workspaceOf(r)
	id := chi.URLParam(r, "id")

	exec, err := s.store.Execution(r.Context(), ws, id)
	if err != nil {
		writeStoreErr(w, err, "That execution does not exist.")
		return
	}
	if exec.Status != domain.StatusFailed {
		writeErr(w, http.StatusConflict, "not_failed", "Only a failed execution can be retried.")
		return
	}

	previous, err := s.store.ListNodeExecutions(r.Context(), exec.ID)
	if err != nil {
		writeStoreErr(w, err, "That execution has no recorded nodes.")
		return
	}

	var failedNodeID string
	for _, n := range previous {
		if n.Status == domain.StatusFailed {
			failedNodeID = n.NodeID
			break
		}
	}

	retry, err := s.store.CreateExecution(r.Context(), domain.NewExecution{
		WorkspaceID:       ws,
		WorkflowID:        exec.WorkflowID,
		WorkflowVersionID: exec.WorkflowVersionID,
		Status:            domain.StatusQueued,
		TriggerType:       exec.TriggerType,
		TriggerData:       exec.TriggerData,
		ResumeFromNode:    nullableString(failedNodeID),
	})
	if err != nil {
		writeStoreErr(w, err, "Could not start the retry.")
		return
	}

	for _, n := range previous {
		if n.Status != domain.StatusSucceeded {
			continue
		}
		carried := n
		carried.ID = ""
		carried.ExecutionID = retry.ID
		if _, err := s.store.UpsertNodeExecution(r.Context(), carried); err != nil {
			s.log.Error("api: carry node execution into retry",
				"error", err, "execution", retry.ID, "node", n.NodeID)
			writeErr(w, http.StatusInternalServerError, domain.ErrCodeInternal,
				"The retry could not reuse the earlier results.")
			return
		}
	}

	if _, err := s.queue.Enqueue(r.Context(), queue.KindExecution, queue.ExecutionPayload{ExecutionID: retry.ID}); err != nil {
		s.log.Error("api: enqueue retry", "error", err, "execution", retry.ID)
		writeErr(w, http.StatusInternalServerError, domain.ErrCodeInternal, "The retry could not be queued.")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"execution": viewExecution(retry)})
}

func nullableString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
