// Package api is the HTTP surface: REST for the editor, server-sent events for
// live execution logs, and the public webhook ingress. It never runs a
// workflow itself — it enqueues a job and lets the worker do that.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/auth"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/config"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/llm"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// Deps is everything the HTTP layer needs, injected so tests can substitute a
// fake store or an empty registry.
type Deps struct {
	Store    Store
	Queue    Enqueuer
	Registry *nodes.Registry
	Signer   *auth.Signer
	Config   config.Config
	Logger   *slog.Logger

	// LLM backs the drawer's "Test step" button for the AI nodes. Nil is legal
	// and makes those nodes report that no provider is configured.
	LLM *llm.Registry
}

type server struct {
	store    Store
	queue    Enqueuer
	registry *nodes.Registry
	signer   *auth.Signer
	cfg      config.Config
	log      *slog.Logger
	llm      *llm.Registry
}

// NewRouter wires every route. The /api/v1 tree is authenticated; /webhook and
// /healthz deliberately are not.
func NewRouter(d Deps) http.Handler {
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	s := &server{
		store:    d.Store,
		queue:    d.Queue,
		registry: d.Registry,
		signer:   d.Signer,
		cfg:      d.Config,
		log:      log,
		llm:      d.LLM,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Next.js proxies /api in development, so same-origin is the normal case.
	// CORS is here only for a direct-to-8080 client such as curl or a script.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{d.Config.PublicBaseURL},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", s.handleHealth)

	// Public ingress. Any method, so a provider can send whatever it likes.
	r.HandleFunc("/webhook/*", s.handleWebhook)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Get("/auth/me", s.handleMe)

			r.Get("/node-types", s.handleNodeTypes)
			r.Post("/nodes/{type}/test", s.handleTestNode)

			r.Get("/workflows", s.handleListWorkflows)
			r.Post("/workflows", s.handleCreateWorkflow)
			r.Get("/workflows/{id}", s.handleGetWorkflow)
			r.Patch("/workflows/{id}", s.handlePatchWorkflow)
			r.Delete("/workflows/{id}", s.handleDeleteWorkflow)
			r.Post("/workflows/{id}/versions", s.handleCreateVersion)
			r.Post("/workflows/{id}/run", s.handleRunWorkflow)

			r.Get("/executions", s.handleListExecutions)
			r.Get("/executions/{id}", s.handleGetExecution)
			r.Get("/executions/{id}/stream", s.handleExecutionStream)
			r.Post("/executions/{id}/cancel", s.handleCancelExecution)
			r.Post("/executions/{id}/retry", s.handleRetryExecution)
		})
	})

	return r
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "unhealthy", "The database is unreachable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
