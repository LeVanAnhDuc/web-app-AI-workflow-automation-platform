// Command api serves the REST API, the execution event stream and the public
// webhook ingress. It never runs a workflow: it records one and enqueues it,
// leaving the work to cmd/worker.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/db"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/api"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/auth"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/config"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/queue"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/store"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply database migrations and exit")
	flag.Parse()

	if err := run(*migrateOnly); err != nil {
		slog.Default().Error("api: fatal", "error", err)
		os.Exit(1)
	}
}

func run(migrateOnly bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrating on boot keeps a single-operator deployment honest: there is no
	// separate step to forget.
	migrateCtx, cancelMigrate := context.WithTimeout(ctx, time.Minute)
	defer cancelMigrate()
	if err := store.Migrate(migrateCtx, cfg.DatabaseURL, db.Migrations, db.MigrationsDir); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	log.Info("api: migrations up to date")
	if migrateOnly {
		return nil
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer st.Close()

	if err := seed(ctx, st, cfg, log); err != nil {
		return err
	}

	signer, err := auth.NewSigner(cfg.JWTSecret)
	if err != nil {
		return err
	}

	handler := api.NewRouter(api.Deps{
		Store:    st,
		Queue:    queue.New(st.Pool(), "api"),
		Registry: nodes.Default(),
		Signer:   signer,
		Config:   cfg,
		Logger:   log,
	})

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
		// Generous: the SSE endpoint holds a connection open for minutes, and it
		// sets its own deadline.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errs := make(chan error, 1)
	go func() {
		log.Info("api: listening", "addr", srv.Addr, "publicBaseURL", cfg.PublicBaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("api: shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// seed creates the first workspace and user when SEED_EMAIL and SEED_PASSWORD
// are set. It is idempotent, so leaving them in a .env file is harmless.
func seed(ctx context.Context, st *store.Store, cfg config.Config, log *slog.Logger) error {
	if cfg.SeedEmail == "" || cfg.SeedPassword == "" {
		return nil
	}
	hash, err := auth.HashPassword(cfg.SeedPassword)
	if err != nil {
		return err
	}
	ws, user, err := st.EnsureSeed(ctx, "Acme", cfg.SeedEmail, hash)
	if err != nil {
		return fmt.Errorf("seed workspace: %w", err)
	}
	log.Info("api: seed account ready", "workspace", ws.Name, "email", user.Email)
	return nil
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
