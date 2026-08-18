// Package store is the pgx repository layer. Every workspace-scoped function
// takes a workspaceID and puts it in the WHERE clause, so the single-tenant
// Phase 1 product already queries like a multi-tenant one and Phase 4 opens a
// UI over it instead of migrating data.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx", used by Migrate
	"github.com/pressly/goose/v3"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// Store owns the connection pool and exposes one method per repository
// operation. It holds no other state, so it is safe for concurrent use.
type Store struct {
	pool *pgxpool.Pool
}

// New opens a pool against dsn and verifies it with a ping, so a bad DSN fails
// at boot rather than on the first request.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	s := &Store{pool: pool}
	if err := s.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// NewWithPool wraps an existing pool, which tests and the worker use to share
// one pool between the store and the job queue.
func NewWithPool(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the underlying pool so the job queue can reuse it.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Close releases every connection.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Ping reports whether the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// Migrate applies the goose migrations in migrations/dir. The migrations
// arrive as an fs.FS so the binaries can embed them and a deployment needs no
// files on disk; goose speaks database/sql, hence the pgx stdlib driver.
func Migrate(ctx context.Context, dsn string, migrations fs.FS, dir string) error {
	if dir == "" {
		dir = "."
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	goose.SetBaseFS(migrations)
	defer goose.SetBaseFS(nil)

	if err := goose.UpContext(ctx, db, dir); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// pgUniqueViolation is the SQLSTATE Postgres reports for a duplicate key.
const pgUniqueViolation = "23505"

// translate maps driver errors onto the domain sentinels so handlers can
// answer 404 or 409 without importing pgx.
func translate(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}
	if isUniqueViolation(err) {
		return fmt.Errorf("%s: %w", op, domain.ErrConflict)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// isUniqueViolation reports whether err is a lost uniqueness race.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// notFound builds the error a lookup returns when nothing matched.
func notFound(op string) error {
	return fmt.Errorf("%s: %w", op, domain.ErrNotFound)
}

// validIDs guards queries against ids that are not UUIDs at all. The API takes
// them straight out of the URL, and a malformed id means "no such row" rather
// than a database error.
func validIDs(ids ...string) bool {
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			return false
		}
	}
	return true
}

// jsonbOrNull marshals a value for a JSONB column, mapping a nil Go value onto
// SQL NULL so "no output yet" stays distinguishable from the JSON literal null.
func jsonbOrNull(v any) (any, error) {
	if isNil(v) {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode jsonb: %w", err)
	}
	return raw, nil
}

// isNil reports whether v is nil, including a typed nil pointer, map or slice
// boxed in an interface — the common case for optional JSONB columns.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// nodeErrorFromJSON decodes an error column, tolerating SQL NULL.
func nodeErrorFromJSON(raw []byte) (*domain.NodeError, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var ne domain.NodeError
	if err := json.Unmarshal(raw, &ne); err != nil {
		return nil, fmt.Errorf("decode error column: %w", err)
	}
	return &ne, nil
}

// outputsFromJSON decodes a node execution's per-handle outputs, tolerating
// SQL NULL.
func outputsFromJSON(raw []byte) (map[string][]domain.Item, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out map[string][]domain.Item
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode output column: %w", err)
	}
	return out, nil
}
