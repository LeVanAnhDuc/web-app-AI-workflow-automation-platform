package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/queue"
)

// maxWebhookBody caps what a caller can push through the public ingress.
const maxWebhookBody = 1 << 20 // 1 MiB

// handleWebhook is the public ingress. It records an execution, enqueues it and
// answers 202 immediately — holding the connection open for the length of a
// workflow would make every caller's timeout our problem.
func (s *server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/webhook/"), "/")
	if path == "" {
		writeErr(w, http.StatusNotFound, "not_found", "No webhook is registered at that path.")
		return
	}

	hook, err := s.store.WebhookByPath(r.Context(), path)
	if err != nil {
		// Deliberately the same answer whether the path is unknown or the
		// workflow is inactive: a scanner learns nothing either way.
		writeErr(w, http.StatusNotFound, "not_found", "No webhook is registered at that path.")
		return
	}

	if !strings.EqualFold(hook.Method, r.Method) && r.Method != http.MethodOptions {
		w.Header().Set("Allow", strings.ToUpper(hook.Method))
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"This webhook accepts "+strings.ToUpper(hook.Method)+".")
		return
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", strings.ToUpper(hook.Method)+", OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	version, err := s.store.LatestVersion(r.Context(), hook.WorkspaceID, hook.WorkflowID)
	if err != nil {
		writeStoreErr(w, err, "That workflow is no longer available.")
		return
	}

	item := domain.NewItem(map[string]any{
		"headers": flattenHeaders(r.Header),
		"query":   flattenQuery(r.URL.Query()),
		"body":    readWebhookBody(w, r),
		"method":  r.Method,
		"path":    path,
	})

	exec, err := s.store.CreateExecution(r.Context(), domain.NewExecution{
		WorkspaceID:       hook.WorkspaceID,
		WorkflowID:        hook.WorkflowID,
		WorkflowVersionID: version.ID,
		Status:            domain.StatusQueued,
		TriggerType:       domain.TriggerWebhook,
		TriggerData:       []domain.Item{item},
	})
	if err != nil {
		s.log.Error("api: create webhook execution", "error", err, "path", path)
		writeErr(w, http.StatusInternalServerError, domain.ErrCodeInternal, "The request could not be recorded.")
		return
	}

	if _, err := s.queue.Enqueue(r.Context(), queue.KindExecution, queue.ExecutionPayload{ExecutionID: exec.ID}); err != nil {
		s.log.Error("api: enqueue webhook execution", "error", err, "execution", exec.ID)
		writeErr(w, http.StatusInternalServerError, domain.ErrCodeInternal, "The request could not be queued.")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"executionId": exec.ID,
		"status":      exec.Status,
	})
}

// readWebhookBody parses JSON when the caller says it is JSON, and otherwise
// hands the workflow the raw text — a webhook source we do not control should
// not be able to fail the run by sending an unexpected content type.
func readWebhookBody(w http.ResponseWriter, r *http.Request) any {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil || len(raw) == 0 {
		return nil
	}

	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "json") {
		var parsed any
		if json.Unmarshal(raw, &parsed) == nil {
			return parsed
		}
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "x-www-form-urlencoded") {
		if values, err := url.ParseQuery(string(raw)); err == nil {
			return flattenQuery(values)
		}
	}
	return string(raw)
}

func flattenHeaders(h http.Header) map[string]any {
	out := make(map[string]any, len(h))
	for k, v := range h {
		// A workflow almost always wants the single value; joining keeps the
		// rare multi-value header usable rather than silently dropping it.
		out[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return out
}

func flattenQuery(q map[string][]string) map[string]any {
	out := make(map[string]any, len(q))
	for k, v := range q {
		if len(v) == 1 {
			out[k] = v[0]
			continue
		}
		anyVals := make([]any, len(v))
		for i, s := range v {
			anyVals[i] = s
		}
		out[k] = anyVals
	}
	return out
}
