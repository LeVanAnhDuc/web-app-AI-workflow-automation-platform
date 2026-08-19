package store

import (
	"context"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

/* ---------------------------------------------------------------------------
   Credentials.

   This layer stores and returns the sealed blob and nothing else — it never
   encrypts, decrypts or inspects it. Keeping the key out of the repository
   means a store test, a migration or a stray log line cannot leak a secret;
   internal/credentials owns the sealing.
   --------------------------------------------------------------------------- */

const credentialColumns = `id, workspace_id, type, name, created_at, updated_at`

// CreateCredential stores a new sealed credential and returns its record.
func (s *Store) CreateCredential(ctx context.Context, workspaceID, credType, name string, sealed []byte) (domain.Credential, error) {
	if !validIDs(workspaceID) {
		return domain.Credential{}, notFound("create credential")
	}

	var c domain.Credential
	err := s.pool.QueryRow(ctx,
		`INSERT INTO credentials (workspace_id, type, name, data_encrypted)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+credentialColumns,
		workspaceID, credType, name, sealed,
	).Scan(&c.ID, &c.WorkspaceID, &c.Type, &c.Name, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return domain.Credential{}, translate("create credential", err)
	}
	return c, nil
}

// Credential returns one credential's record without its blob.
func (s *Store) Credential(ctx context.Context, workspaceID, id string) (domain.Credential, error) {
	if !validIDs(workspaceID, id) {
		return domain.Credential{}, notFound("credential")
	}

	var c domain.Credential
	err := s.pool.QueryRow(ctx,
		`SELECT `+credentialColumns+` FROM credentials WHERE id = $1 AND workspace_id = $2`,
		id, workspaceID,
	).Scan(&c.ID, &c.WorkspaceID, &c.Type, &c.Name, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return domain.Credential{}, translate("credential", err)
	}
	return c, nil
}

// CredentialSealed returns the encrypted blob alongside the record. The engine
// resolves a node's credential with this, so it is scoped by workspace like
// everything else: a workflow can only ever reach its own workspace's secrets.
func (s *Store) CredentialSealed(ctx context.Context, workspaceID, id string) (domain.Credential, []byte, error) {
	if !validIDs(workspaceID, id) {
		return domain.Credential{}, nil, notFound("credential")
	}

	var (
		c      domain.Credential
		sealed []byte
	)
	err := s.pool.QueryRow(ctx,
		`SELECT `+credentialColumns+`, data_encrypted
		 FROM credentials WHERE id = $1 AND workspace_id = $2`,
		id, workspaceID,
	).Scan(&c.ID, &c.WorkspaceID, &c.Type, &c.Name, &c.CreatedAt, &c.UpdatedAt, &sealed)
	if err != nil {
		return domain.Credential{}, nil, translate("credential", err)
	}
	return c, sealed, nil
}

// ListCredentials returns a workspace's credentials, newest first, optionally
// narrowed to one type — which is what the node drawer's picker needs.
func (s *Store) ListCredentials(ctx context.Context, workspaceID, credType string) ([]domain.Credential, error) {
	if !validIDs(workspaceID) {
		return nil, notFound("list credentials")
	}

	query := `SELECT ` + credentialColumns + ` FROM credentials WHERE workspace_id = $1`
	args := []any{workspaceID}
	if credType != "" {
		query += ` AND type = $2`
		args = append(args, credType)
	}
	query += ` ORDER BY created_at DESC, id DESC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, translate("list credentials", err)
	}
	defer rows.Close()

	var out []domain.Credential
	for rows.Next() {
		var c domain.Credential
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Type, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, translate("scan credential", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, translate("list credentials", err)
	}
	return out, nil
}

// UpdateCredential replaces a credential's name, its blob, or both. A nil blob
// leaves the stored secret untouched, which is how an edit that does not retype
// a password keeps it.
func (s *Store) UpdateCredential(ctx context.Context, workspaceID, id string, name *string, sealed []byte) (domain.Credential, error) {
	if !validIDs(workspaceID, id) {
		return domain.Credential{}, notFound("update credential")
	}

	var c domain.Credential
	err := s.pool.QueryRow(ctx,
		`UPDATE credentials
		    SET name = COALESCE($3, name),
		        data_encrypted = COALESCE($4, data_encrypted),
		        updated_at = now()
		  WHERE id = $1 AND workspace_id = $2
		  RETURNING `+credentialColumns,
		id, workspaceID, name, nullableBytes(sealed),
	).Scan(&c.ID, &c.WorkspaceID, &c.Type, &c.Name, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return domain.Credential{}, translate("update credential", err)
	}
	return c, nil
}

// DeleteCredential removes a credential. Nodes referencing it are left alone
// deliberately: rewriting saved workflow versions to erase an id would corrupt
// the history an execution replay depends on. A run then fails with a message
// naming the missing credential, which is the honest outcome.
func (s *Store) DeleteCredential(ctx context.Context, workspaceID, id string) error {
	if !validIDs(workspaceID, id) {
		return notFound("delete credential")
	}

	tag, err := s.pool.Exec(ctx,
		`DELETE FROM credentials WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return translate("delete credential", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("delete credential")
	}
	return nil
}

// CredentialUsage counts, per credential id, how many nodes in the workspace's
// latest workflow versions reference it. It powers an honest delete
// confirmation: "used by 3 nodes" rather than a bare "are you sure".
func (s *Store) CredentialUsage(ctx context.Context, workspaceID string) (map[string]int, error) {
	if !validIDs(workspaceID) {
		return nil, notFound("credential usage")
	}

	rows, err := s.pool.Query(ctx, `
WITH latest AS (
    SELECT DISTINCT ON (v.workflow_id) v.graph
    FROM workflow_versions v
    JOIN workflows w ON w.id = v.workflow_id
    WHERE w.workspace_id = $1
    ORDER BY v.workflow_id, v.version DESC
)
SELECT n ->> 'credentialId' AS credential_id, count(*)
FROM latest,
     LATERAL jsonb_array_elements(
         CASE WHEN jsonb_typeof(latest.graph -> 'nodes') = 'array'
              THEN latest.graph -> 'nodes' ELSE '[]'::jsonb END
     ) AS n
WHERE n ->> 'credentialId' IS NOT NULL
GROUP BY 1`, workspaceID)
	if err != nil {
		return nil, translate("credential usage", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var (
			id    string
			count int
		)
		if err := rows.Scan(&id, &count); err != nil {
			return nil, translate("scan credential usage", err)
		}
		out[id] = count
	}
	if err := rows.Err(); err != nil {
		return nil, translate("credential usage", err)
	}
	return out, nil
}

// TouchCredential records that a credential's sealed blob changed without
// touching its name — the path a refreshed OAuth token takes.
func (s *Store) TouchCredential(ctx context.Context, workspaceID, id string, sealed []byte, at time.Time) error {
	if !validIDs(workspaceID, id) {
		return notFound("touch credential")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE credentials SET data_encrypted = $3, updated_at = $4
		  WHERE id = $1 AND workspace_id = $2`,
		id, workspaceID, sealed, at)
	if err != nil {
		return translate("touch credential", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("touch credential")
	}
	return nil
}

// nullableBytes maps an empty blob onto SQL NULL so COALESCE keeps the stored
// value rather than overwriting it with nothing.
func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
