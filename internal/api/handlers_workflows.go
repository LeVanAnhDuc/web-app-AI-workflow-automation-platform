package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/engine"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/queue"
)

type workflowDetail struct {
	Workflow   domain.Workflow `json:"workflow"`
	Graph      domain.Graph    `json:"graph"`
	Version    int             `json:"version"`
	WebhookURL string          `json:"webhookUrl,omitempty"`
}

func (s *server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := domain.WorkflowFilter{
		Query:  strings.TrimSpace(q.Get("q")),
		Status: q.Get("status"),
		Sort:   domain.WorkflowSort(q.Get("sort")),
	}

	list, err := s.store.ListWorkflowSummaries(r.Context(), workspaceOf(r), filter)
	if err != nil {
		writeStoreErr(w, err, "No workflows found.")
		return
	}
	if list == nil {
		list = []domain.WorkflowSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": list})
}

func (s *server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Untitled workflow"
	}

	wf, err := s.store.CreateWorkflow(r.Context(), workspaceOf(r), name)
	if err != nil {
		writeStoreErr(w, err, "Could not create the workflow.")
		return
	}

	// A new workflow starts with an empty first version so the editor always
	// has a version to load and to diff against.
	version, err := s.store.CreateWorkflowVersion(r.Context(), workspaceOf(r), wf.ID, domain.Graph{}, ptr(claimsFrom(r.Context()).UserID))
	if err != nil {
		writeStoreErr(w, err, "Could not create the workflow.")
		return
	}

	writeJSON(w, http.StatusCreated, workflowDetail{Workflow: wf, Graph: version.Graph, Version: version.Version})
}

func (s *server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	ws := workspaceOf(r)
	id := chi.URLParam(r, "id")

	wf, err := s.store.Workflow(r.Context(), ws, id)
	if err != nil {
		writeStoreErr(w, err, "That workflow does not exist.")
		return
	}
	version, err := s.store.LatestVersion(r.Context(), ws, id)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		writeStoreErr(w, err, "That workflow has no versions.")
		return
	}

	writeJSON(w, http.StatusOK, workflowDetail{
		Workflow:   wf,
		Graph:      version.Graph,
		Version:    version.Version,
		WebhookURL: s.webhookURLFor(version.Graph),
	})
}

func (s *server) handlePatchWorkflow(w http.ResponseWriter, r *http.Request) {
	ws := workspaceOf(r)
	id := chi.URLParam(r, "id")

	var req struct {
		Name   *string `json:"name"`
		Active *bool   `json:"active"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeErr(w, http.StatusUnprocessableEntity, domain.ErrCodeValidation, "A workflow needs a name.")
			return
		}
		req.Name = &trimmed
	}

	// Activating publishes the triggers, so the graph must be runnable first.
	// Refusing here is far kinder than accepting a workflow whose webhook can
	// never succeed.
	if req.Active != nil && *req.Active {
		version, err := s.store.LatestVersion(r.Context(), ws, id)
		if err != nil {
			writeStoreErr(w, err, "That workflow has nothing to activate yet.")
			return
		}
		if err := engine.ValidateGraph(version.Graph, s.registry); err != nil {
			writeErrDetails(w, http.StatusUnprocessableEntity, domain.ErrCodeValidation,
				"This workflow cannot be activated yet: "+err.Error(), nil)
			return
		}
	}

	wf, err := s.store.UpdateWorkflow(r.Context(), ws, id, domain.WorkflowPatch{Name: req.Name, Active: req.Active})
	if err != nil {
		writeStoreErr(w, err, "That workflow does not exist.")
		return
	}

	if req.Active != nil {
		if err := s.reconcileTriggers(r.Context(), ws, wf); err != nil {
			writeStoreErr(w, err, "Could not publish the workflow's triggers.")
			return
		}
	}

	version, _ := s.store.LatestVersion(r.Context(), ws, id)
	writeJSON(w, http.StatusOK, workflowDetail{
		Workflow:   wf,
		Graph:      version.Graph,
		Version:    version.Version,
		WebhookURL: s.webhookURLFor(version.Graph),
	})
}

func (s *server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteWorkflow(r.Context(), workspaceOf(r), chi.URLParam(r, "id")); err != nil {
		writeStoreErr(w, err, "That workflow does not exist.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleCreateVersion(w http.ResponseWriter, r *http.Request) {
	ws := workspaceOf(r)
	id := chi.URLParam(r, "id")

	var req struct {
		Graph domain.Graph `json:"graph"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	wf, err := s.store.Workflow(r.Context(), ws, id)
	if err != nil {
		writeStoreErr(w, err, "That workflow does not exist.")
		return
	}

	// A draft may legitimately be half-built, so an incomplete graph is saved
	// rather than rejected. Only activation and running demand a valid graph —
	// except for the structural rules, which would make the saved JSON
	// meaningless.
	if err := validateGraphStructure(req.Graph); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, domain.ErrCodeValidation, err.Error())
		return
	}

	version, err := s.store.CreateWorkflowVersion(r.Context(), ws, id, req.Graph, ptr(claimsFrom(r.Context()).UserID))
	if err != nil {
		writeStoreErr(w, err, "Could not save the workflow.")
		return
	}

	// An active workflow's triggers follow the graph it now points at.
	if wf.Active {
		wf.ActiveVersionID = &version.ID
		if err := s.reconcileTriggers(r.Context(), ws, wf); err != nil {
			writeStoreErr(w, err, "Could not republish the workflow's triggers.")
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"version": version})
}

func (s *server) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	ws := workspaceOf(r)
	id := chi.URLParam(r, "id")

	var req struct {
		Data any `json:"data"`
	}
	// A run with no body is normal, so an unreadable body is only an error when
	// something was actually sent.
	if r.ContentLength > 0 && !decodeJSON(w, r, &req) {
		return
	}

	version, err := s.store.LatestVersion(r.Context(), ws, id)
	if err != nil {
		writeStoreErr(w, err, "That workflow does not exist.")
		return
	}
	if err := engine.ValidateGraph(version.Graph, s.registry); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, domain.ErrCodeValidation, err.Error())
		return
	}

	var trigger []domain.Item
	if req.Data != nil {
		trigger = itemsFromAny(req.Data)
	}

	exec, err := s.store.CreateExecution(r.Context(), domain.NewExecution{
		WorkspaceID:       ws,
		WorkflowID:        id,
		WorkflowVersionID: version.ID,
		Status:            domain.StatusQueued,
		TriggerType:       domain.TriggerManual,
		TriggerData:       trigger,
	})
	if err != nil {
		writeStoreErr(w, err, "Could not start a run.")
		return
	}

	if _, err := s.queue.Enqueue(r.Context(), queue.KindExecution, queue.ExecutionPayload{ExecutionID: exec.ID}); err != nil {
		s.log.Error("api: enqueue execution", "error", err, "execution", exec.ID)
		writeErr(w, http.StatusInternalServerError, domain.ErrCodeInternal, "The run could not be queued.")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"execution": exec})
}

// itemsFromAny turns a run payload into items: an array becomes one item per
// element, anything else becomes a single item.
func itemsFromAny(v any) []domain.Item {
	switch typed := v.(type) {
	case []any:
		out := make([]domain.Item, 0, len(typed))
		for _, e := range typed {
			out = append(out, itemFromAny(e))
		}
		return out
	default:
		return []domain.Item{itemFromAny(v)}
	}
}

func itemFromAny(v any) domain.Item {
	if m, ok := v.(map[string]any); ok {
		return domain.NewItem(m)
	}
	return domain.NewItem(map[string]any{"value": v})
}

func ptr[T any](v T) *T { return &v }
