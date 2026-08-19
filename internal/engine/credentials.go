package engine

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/credentials"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

/* ---------------------------------------------------------------------------
   Giving a node its credential.

   The engine never sees a decrypted secret and never stores one: it hands each
   node a resolver that signs a request on demand. Three properties matter.

   Lazy: a node that never makes a request must not pay for a decrypt, and a
   broken credential must fail the node that uses it rather than the one before
   it. Resolution therefore happens on first use, not when the node starts.

   Cached: a node that makes three requests must not refresh an OAuth token
   three times, so the resolution is memoised for the life of one node call.

   Scoped: the workspace comes from the execution, never from the graph, so a
   workflow can only ever reach its own workspace's secrets.
   --------------------------------------------------------------------------- */

// CredentialProvider is the slice of the credential vault the engine needs.
// *credentials.Service satisfies it structurally.
//
// The signature names credentials.Resolved, so the engine does import
// internal/credentials — unavoidably, since attaching a credential to a request
// is credentials.Apply and no narrower type expresses it. The dependency still
// points inwards: internal/credentials knows nothing about the engine.
type CredentialProvider interface {
	Resolve(ctx context.Context, workspaceID, id string) (credentials.Resolved, error)
}

// credentialBinding is one node call's view of one credential.
type credentialBinding struct {
	provider     CredentialProvider
	workspaceID  string
	credentialID string
	nodeName     string

	mu       sync.Mutex
	done     bool
	resolved credentials.Resolved
	err      error
}

// bindCredential builds the resolver for one node call, or nil when the node
// has no credential configured — which is what nodes read as "none set".
func (r *run) bindCredential(node domain.GraphNode) nodes.CredentialResolver {
	if node.CredentialID == nil || *node.CredentialID == "" {
		return nil
	}
	if r.e.opts.Credentials == nil {
		// A deployment with no vault wired in: say so plainly rather than
		// letting the node send an unauthenticated request that half-works.
		return &missingVault{credentialID: *node.CredentialID}
	}
	return &credentialBinding{
		provider: r.e.opts.Credentials,
		// The execution's workspace, not anything the graph carries.
		workspaceID:  r.exec.WorkspaceID,
		credentialID: *node.CredentialID,
		nodeName:     node.Name,
	}
}

// load resolves once and remembers the outcome, errors included: a credential
// that will not open will not open on the second request either, and retrying
// the decrypt would only slow the failure down.
func (b *credentialBinding) load(ctx context.Context) (credentials.Resolved, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.done {
		b.done = true
		resolved, err := b.provider.Resolve(ctx, b.workspaceID, b.credentialID)
		if err != nil {
			b.err = b.explain(err)
		} else {
			b.resolved = resolved
		}
	}
	return b.resolved, b.err
}

// peek reports the resolution only if one has already happened. Type and Name
// use it so that asking a node's credential what it is called never becomes the
// thing that decrypts it.
func (b *credentialBinding) peek() (credentials.Resolved, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.resolved, b.done && b.err == nil
}

// Apply signs the request, resolving the credential on the way if this is the
// first use.
func (b *credentialBinding) Apply(ctx context.Context, req *http.Request) error {
	resolved, err := b.load(ctx)
	if err != nil {
		return err
	}
	if err := credentials.Apply(req, resolved); err != nil {
		return b.explain(err)
	}
	return nil
}

// Type is the credential's type id, once one is known. It reports "" rather
// than resolving, because it exists to make a message more specific and must
// not become the reason a credential is decrypted.
func (b *credentialBinding) Type() string {
	if resolved, ok := b.peek(); ok {
		return resolved.Type.Type
	}
	return ""
}

// Name is the credential's display name, falling back to its id so a message
// about a credential nothing has opened yet still names something searchable.
func (b *credentialBinding) Name() string {
	if resolved, ok := b.peek(); ok {
		return resolved.Name
	}
	return b.credentialID
}

// explain turns a vault error into a sentence a workflow author can act on.
// The execution log is where these are read, so "not found" is not enough:
// each case names what is wrong and what to do about it.
func (b *credentialBinding) explain(err error) error {
	var (
		unknownType  *credentials.ErrUnknownType
		notConnected *credentials.ErrNotConnected
	)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return domain.Errorf(domain.ErrCodeValidation,
			"the node %q uses a credential that no longer exists (%s); pick another one in the node's settings",
			b.nodeName, b.credentialID)

	case errors.As(err, &unknownType):
		return domain.Errorf(domain.ErrCodeValidation,
			"the node %q uses a credential of type %q, which this version does not know about",
			b.nodeName, unknownType.Type)

	case errors.As(err, &notConnected):
		return domain.Errorf(domain.ErrCodeValidation,
			"the credential %q used by %q has never been connected; open it under Credentials and connect it",
			notConnected.Name, b.nodeName)

	default:
		// Includes the blob that will not decrypt, whose message already
		// explains that CREDENTIAL_KEY probably changed.
		return domain.Errorf(domain.ErrCodeValidation,
			"the credential for %q could not be used: %s", b.nodeName, err.Error())
	}
}

// missingVault stands in for a node with a credential in a process that has no
// credential service. It fails loudly at the point of use rather than silently
// sending an unauthenticated request.
type missingVault struct{ credentialID string }

func (m *missingVault) Apply(context.Context, *http.Request) error {
	return domain.Errorf(domain.ErrCodeInternal,
		"this node has a credential but no credential store is configured on this deployment")
}

func (m *missingVault) Type() string { return "" }
func (m *missingVault) Name() string { return m.credentialID }
