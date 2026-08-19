package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

func TestCredentialRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	sealed := []byte("sealed-bytes-that-this-layer-never-reads")
	created, err := s.CreateCredential(ctx, ws.ID, "slackOAuth2", "Acme Slack", sealed)
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "slackOAuth2", created.Type)

	// The plain read deliberately does not carry the blob: a handler that only
	// needs metadata should not be able to leak one by accident.
	got, err := s.Credential(ctx, ws.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Acme Slack", got.Name)

	_, blob, err := s.CredentialSealed(ctx, ws.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, sealed, blob)
}

func TestCredentialsAreInvisibleToOtherWorkspaces(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)
	other, _ := newWorkspace(t, s)

	created, err := s.CreateCredential(ctx, ws.ID, "bearerAuth", "Acme", []byte("sealed"))
	require.NoError(t, err)

	_, err = s.Credential(ctx, other.ID, created.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	_, _, err = s.CredentialSealed(ctx, other.ID, created.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound, "the blob must be unreachable across a tenant boundary")

	assert.ErrorIs(t, s.DeleteCredential(ctx, other.ID, created.ID), domain.ErrNotFound)

	list, err := s.ListCredentials(ctx, other.ID, "")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestUpdateCredentialKeepsTheBlobWhenNoneIsGiven(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	created, err := s.CreateCredential(ctx, ws.ID, "bearerAuth", "Before", []byte("original-blob"))
	require.NoError(t, err)

	// A rename must not disturb the secret, which is the common case: the
	// caller has nothing new to seal.
	name := "After"
	updated, err := s.UpdateCredential(ctx, ws.ID, created.ID, &name, nil)
	require.NoError(t, err)
	assert.Equal(t, "After", updated.Name)

	_, blob, err := s.CredentialSealed(ctx, ws.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("original-blob"), blob)
}

func TestUpdateCredentialReplacesTheBlobWhenOneIsGiven(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	created, err := s.CreateCredential(ctx, ws.ID, "bearerAuth", "Acme", []byte("original"))
	require.NoError(t, err)

	_, err = s.UpdateCredential(ctx, ws.ID, created.ID, nil, []byte("rotated"))
	require.NoError(t, err)

	_, blob, err := s.CredentialSealed(ctx, ws.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("rotated"), blob)
}

func TestListCredentialsFiltersByType(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	_, err := s.CreateCredential(ctx, ws.ID, "bearerAuth", "A token", []byte("x"))
	require.NoError(t, err)
	_, err = s.CreateCredential(ctx, ws.ID, "slackOAuth2", "A Slack", []byte("y"))
	require.NoError(t, err)

	all, err := s.ListCredentials(ctx, ws.ID, "")
	require.NoError(t, err)
	require.Len(t, all, 2)

	// The node drawer's picker asks for exactly one type.
	slack, err := s.ListCredentials(ctx, ws.ID, "slackOAuth2")
	require.NoError(t, err)
	require.Len(t, slack, 1)
	assert.Equal(t, "A Slack", slack[0].Name)
}

func TestCredentialNamesAreUniquePerWorkspace(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)
	other, _ := newWorkspace(t, s)

	_, err := s.CreateCredential(ctx, ws.ID, "bearerAuth", "Production", []byte("x"))
	require.NoError(t, err)

	// Same name, same workspace: a dropdown with two identical entries is a
	// usability bug worth refusing.
	_, err = s.CreateCredential(ctx, ws.ID, "slackOAuth2", "Production", []byte("y"))
	assert.ErrorIs(t, err, domain.ErrConflict)

	// Case-insensitively, because "production" and "Production" read the same.
	_, err = s.CreateCredential(ctx, ws.ID, "slackOAuth2", "production", []byte("y"))
	assert.ErrorIs(t, err, domain.ErrConflict)

	// Another tenant may of course use the same name.
	_, err = s.CreateCredential(ctx, other.ID, "bearerAuth", "Production", []byte("z"))
	assert.NoError(t, err)
}

func TestTouchCredentialWritesTheBlobWithoutChangingTheName(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	created, err := s.CreateCredential(ctx, ws.ID, "slackOAuth2", "Acme Slack", []byte("before"))
	require.NoError(t, err)

	// The path a refreshed OAuth token takes.
	at := time.Now().UTC().Add(time.Minute)
	require.NoError(t, s.TouchCredential(ctx, ws.ID, created.ID, []byte("after"), at))

	got, blob, err := s.CredentialSealed(ctx, ws.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Acme Slack", got.Name)
	assert.Equal(t, []byte("after"), blob)
	assert.WithinDuration(t, at, got.UpdatedAt, time.Second)

	assert.ErrorIs(t,
		s.TouchCredential(ctx, other(t, s).ID, created.ID, []byte("nope"), at),
		domain.ErrNotFound)
}

func TestCredentialUsageCountsNodesInTheLatestVersion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	cred, err := s.CreateCredential(ctx, ws.ID, "slackOAuth2", "Acme Slack", []byte("x"))
	require.NoError(t, err)
	unused, err := s.CreateCredential(ctx, ws.ID, "bearerAuth", "Spare", []byte("y"))
	require.NoError(t, err)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Uses Slack")
	require.NoError(t, err)

	credID := cred.ID
	_, err = s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: "trigger.manual", Name: "Start"},
		{ID: "n2", Type: "slack", Name: "Post", CredentialID: &credID},
		{ID: "n3", Type: "slack", Name: "Post again", CredentialID: &credID},
	}}, &user.ID)
	require.NoError(t, err)

	usage, err := s.CredentialUsage(ctx, ws.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, usage[cred.ID], "both nodes referencing it are counted")
	assert.Zero(t, usage[unused.ID], "an unused credential is absent, not zero-with-a-row")
}

func TestCredentialUsageOnlyCountsTheLatestVersion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	cred, err := s.CreateCredential(ctx, ws.ID, "slackOAuth2", "Acme Slack", []byte("x"))
	require.NoError(t, err)
	wf, err := s.CreateWorkflow(ctx, ws.ID, "Changed its mind")
	require.NoError(t, err)

	credID := cred.ID
	_, err = s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: "slack", Name: "Post", CredentialID: &credID},
	}}, &user.ID)
	require.NoError(t, err)

	// The node was removed. An old version still references the credential, but
	// counting history would make "used by 1 node" a lie about the live state.
	_, err = s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: "set", Name: "Shape"},
	}}, &user.ID)
	require.NoError(t, err)

	usage, err := s.CredentialUsage(ctx, ws.ID)
	require.NoError(t, err)
	assert.Zero(t, usage[cred.ID])
}

func TestCredentialUsageSurvivesAnEmptyGraph(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Empty")
	require.NoError(t, err)
	_, err = s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, domain.Graph{}, &user.ID)
	require.NoError(t, err)

	// jsonb_array_elements raises on a non-array, and a brand-new workflow's
	// graph is exactly that case.
	usage, err := s.CredentialUsage(ctx, ws.ID)
	require.NoError(t, err)
	assert.Empty(t, usage)
}

func TestDeleteCredentialLeavesReferencingNodesAlone(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	cred, err := s.CreateCredential(ctx, ws.ID, "slackOAuth2", "Acme Slack", []byte("x"))
	require.NoError(t, err)
	wf, err := s.CreateWorkflow(ctx, ws.ID, "Uses Slack")
	require.NoError(t, err)

	credID := cred.ID
	version, err := s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: "slack", Name: "Post", CredentialID: &credID},
	}}, &user.ID)
	require.NoError(t, err)

	require.NoError(t, s.DeleteCredential(ctx, ws.ID, cred.ID))

	// Rewriting saved versions to erase the id would corrupt the history an
	// execution replay depends on. The node keeps its reference and fails at
	// run time with a message naming the missing credential.
	stored, err := s.Version(ctx, version.ID)
	require.NoError(t, err)
	require.Len(t, stored.Graph.Nodes, 1)
	require.NotNil(t, stored.Graph.Nodes[0].CredentialID)
	assert.Equal(t, credID, *stored.Graph.Nodes[0].CredentialID)
}

func TestCredentialRejectsAMalformedID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	// An id straight out of a URL is not necessarily a UUID, and that means
	// "no such credential" rather than a driver error.
	_, err := s.Credential(ctx, ws.ID, "not-a-uuid")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

// other builds a second workspace, for the cross-tenant assertions.
func other(t *testing.T, s *Store) domain.Workspace {
	t.Helper()
	ws, _ := newWorkspace(t, s)
	return ws
}
