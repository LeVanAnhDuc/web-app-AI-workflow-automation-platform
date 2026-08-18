package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/engine"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// handleNodeTypes serves the descriptors the editor builds its palette, node
// picker and config drawer from. Because the frontend renders forms from this
// response, a new node type ships without a frontend change.
func (s *server) handleNodeTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"nodeTypes": s.registry.Descriptors()})
}

// handleTestNode runs one node against supplied input and persists nothing,
// backing the drawer's "Test step" button. Being able to try a single node
// without running the whole workflow is the difference between guessing at an
// expression and knowing.
func (s *server) handleTestNode(w http.ResponseWriter, r *http.Request) {
	nodeType := chi.URLParam(r, "type")

	if _, ok := s.registry.Get(nodeType); !ok {
		writeErr(w, http.StatusNotFound, "unknown_node_type", "There is no node type "+nodeType+".")
		return
	}

	var req struct {
		Params     map[string]any       `json:"params"`
		InputItems []domain.Item        `json:"inputItems"`
		Settings   *domain.NodeSettings `json:"settings"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	settings := domain.NodeSettings{}
	if req.Settings != nil {
		settings = *req.Settings
	}
	// A test step should fail fast; a node left waiting for a minute makes the
	// button feel broken.
	if settings.TimeoutMs <= 0 {
		settings.TimeoutMs = 15000
	}

	node := domain.GraphNode{
		ID:       "test",
		Type:     nodeType,
		Name:     "Test step",
		Params:   req.Params,
		Settings: settings.WithDefaults(),
	}

	input := req.InputItems
	if len(input) == 0 {
		input = []domain.Item{domain.NewItem(nil)}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := engine.RunNodeOnce(ctx, s.registry, node, input, engine.Options{
		Logger: s.log,
		Now:    timeNow,
		LLM:    s.llm,
	})
	if err != nil {
		// A node that fails during a test is a normal outcome, not an HTTP
		// error: the drawer wants to show the message next to the field.
		writeJSON(w, http.StatusOK, map[string]any{"error": domain.AsNodeError(err)})
		return
	}

	outputs := result.Outputs
	if outputs == nil {
		outputs = map[string][]domain.Item{nodes.HandleMain: nil}
	}
	writeJSON(w, http.StatusOK, map[string]any{"outputs": outputs})
}
