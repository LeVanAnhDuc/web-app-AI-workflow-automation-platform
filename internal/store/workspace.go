package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

const userColumns = `id, workspace_id, email, password_hash, role, created_at`

// EnsureSeed creates the first workspace and its owner, and is idempotent: it
// runs on every boot with SEED_EMAIL set, so an existing user must be returned
// untouched rather than overwritten with a freshly hashed password.
func (s *Store) EnsureSeed(ctx context.Context, workspaceName, email, passwordHash string) (domain.Workspace, domain.User, error) {
	if ws, user, err := s.seedByEmail(ctx, email); err == nil {
		return ws, user, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.Workspace{}, domain.User{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Workspace{}, domain.User{}, translate("ensure seed", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var ws domain.Workspace
	err = tx.QueryRow(ctx,
		`INSERT INTO workspaces (name) VALUES ($1) RETURNING id, name, created_at`,
		workspaceName,
	).Scan(&ws.ID, &ws.Name, &ws.CreatedAt)
	if err != nil {
		return domain.Workspace{}, domain.User{}, translate("insert workspace", err)
	}

	var user domain.User
	err = tx.QueryRow(ctx,
		`INSERT INTO users (workspace_id, email, password_hash) VALUES ($1, $2, $3)
		 RETURNING `+userColumns,
		ws.ID, email, passwordHash,
	).Scan(&user.ID, &user.WorkspaceID, &user.Email, &user.PasswordHash, &user.Role, &user.CreatedAt)
	if err != nil {
		// A concurrent boot won the race; its rows are equally valid.
		if isUniqueViolation(err) {
			return s.seedByEmail(ctx, email)
		}
		return domain.Workspace{}, domain.User{}, translate("insert user", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Workspace{}, domain.User{}, translate("commit seed", err)
	}
	return ws, user, nil
}

// seedByEmail loads an existing seeded user together with its workspace.
func (s *Store) seedByEmail(ctx context.Context, email string) (domain.Workspace, domain.User, error) {
	var (
		ws   domain.Workspace
		user domain.User
	)
	err := s.pool.QueryRow(ctx,
		`SELECT u.id, u.workspace_id, u.email, u.password_hash, u.role, u.created_at,
		        w.id, w.name, w.created_at
		 FROM users u JOIN workspaces w ON w.id = u.workspace_id
		 WHERE u.email = $1`,
		email,
	).Scan(&user.ID, &user.WorkspaceID, &user.Email, &user.PasswordHash, &user.Role, &user.CreatedAt,
		&ws.ID, &ws.Name, &ws.CreatedAt)
	if err != nil {
		return domain.Workspace{}, domain.User{}, translate("seed user by email", err)
	}
	return ws, user, nil
}

// UserByEmail loads the user the login handler is authenticating.
func (s *Store) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	return s.user(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email)
}

// UserByID loads the user a session token points at.
func (s *Store) UserByID(ctx context.Context, id string) (domain.User, error) {
	if !validIDs(id) {
		return domain.User{}, notFound("user by id")
	}
	return s.user(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
}

func (s *Store) user(ctx context.Context, query string, args ...any) (domain.User, error) {
	var u domain.User
	err := s.pool.QueryRow(ctx, query, args...).
		Scan(&u.ID, &u.WorkspaceID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return domain.User{}, translate("load user", err)
	}
	return u, nil
}

// FirstWorkspace returns the oldest workspace, which is the only one Phase 1
// has and the one background jobs act in.
func (s *Store) FirstWorkspace(ctx context.Context) (domain.Workspace, error) {
	var ws domain.Workspace
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, created_at FROM workspaces ORDER BY created_at, id LIMIT 1`,
	).Scan(&ws.ID, &ws.Name, &ws.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Workspace{}, notFound("first workspace")
		}
		return domain.Workspace{}, translate("first workspace", err)
	}
	return ws, nil
}
