package engine

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/credentials"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

/* ---------------------------------------------------------------------------
   A node's credential, as the engine hands it over.

   The vault itself is faked here: what these tests are about is the wiring —
   which node gets a resolver, when the resolution happens, how often, and whose
   workspace it is scoped to.
   --------------------------------------------------------------------------- */

// fakeVault is a CredentialProvider over a fixed map, counting resolutions so a
// test can prove that the engine did not decrypt more often than it had to.
type fakeVault struct {
	mu    sync.Mutex
	calls int
	// byWorkspace is workspace id -> credential id -> what it resolves to.
	byWorkspace map[string]map[string]credentials.Resolved
	failWith    error
}

func newFakeVault() *fakeVault {
	return &fakeVault{byWorkspace: map[string]map[string]credentials.Resolved{}}
}

func (v *fakeVault) add(workspaceID, id string, r credentials.Resolved) {
	if v.byWorkspace[workspaceID] == nil {
		v.byWorkspace[workspaceID] = map[string]credentials.Resolved{}
	}
	v.byWorkspace[workspaceID][id] = r
}

func (v *fakeVault) Resolve(_ context.Context, workspaceID, id string) (credentials.Resolved, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls++
	if v.failWith != nil {
		return credentials.Resolved{}, v.failWith
	}
	r, ok := v.byWorkspace[workspaceID][id]
	if !ok {
		// Exactly what the store does for a row in another workspace: it is not
		// there at all, rather than there and refused.
		return credentials.Resolved{}, domain.ErrNotFound
	}
	return r, nil
}

func (v *fakeVault) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.calls
}

// bearerCredential is a resolved static-token credential.
func bearerCredential(name, token string) credentials.Resolved {
	return credentials.Resolved{
		Name: name,
		Type: domain.CredentialType{Type: "bearerAuth", Name: "Bearer Token", Auth: domain.AuthBearer},
		Data: domain.CredentialData{Fields: map[string]string{"token": token}},
	}
}

// signingNode signs a request with whatever credential it was given and puts
// the resulting header into its output, so a test can read it off the row.
func signingNode(typ string, applies int) *fakeNode {
	return &fakeNode{desc: desc(typ, nodes.ModeOnce),
		exec: func(ec nodes.ExecContext) (nodes.Result, error) {
			if !ec.HasCredential() {
				return nodes.Main(domain.NewItem(map[string]any{"credential": "none"})), nil
			}
			var header string
			for range applies {
				req, err := http.NewRequestWithContext(ec.Ctx, http.MethodGet, "https://api.example.com/x", nil)
				if err != nil {
					return nodes.Result{}, err
				}
				if err := ec.Credential.Apply(ec.Ctx, req); err != nil {
					return nodes.Result{}, err
				}
				header = req.Header.Get("Authorization")
			}
			return nodes.Main(domain.NewItem(map[string]any{
				"credential": ec.Credential.Name(),
				"type":       ec.Credential.Type(),
				"auth":       header,
			})), nil
		}}
}

// credentialGraph is trigger -> signer, where the signer names a credential.
func credentialGraph(credentialID *string) domain.Graph {
	signer := graphNode("n2", "sign.test", "Call the API")
	signer.CredentialID = credentialID
	return domain.Graph{
		Nodes: []domain.GraphNode{graphNode("n1", "trigger.test", "Start"), signer},
		Edges: []domain.Edge{graphEdge("e1", "n1", "", "n2", "")},
	}
}

func TestNodeWithACredentialGetsOneAndSignsItsRequest(t *testing.T) {
	vault := newFakeVault()
	vault.add("ws-1", "cred-1", bearerCredential("Acme API", "tok-123"))

	signer := signingNode("sign.test", 1)
	reg := nodes.NewRegistry(fakeTrigger("trigger.test"), signer)
	st := newFakeStore(credentialGraph(ptr("cred-1")))
	opts, _ := testOptions()
	opts.Credentials = vault

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")
	mustEqual(t, st.execution().Status, domain.StatusSucceeded, "execution status")

	got := st.row("n2").Output[domain.MainHandle][0].JSON
	mustEqual(t, got["auth"], "Bearer tok-123", "the node's request was signed")
	mustEqual(t, got["credential"], "Acme API", "the resolver names the credential")
	mustEqual(t, got["type"], "bearerAuth", "the resolver reports the credential type")
}

func TestNodeWithoutACredentialGetsNil(t *testing.T) {
	vault := newFakeVault()
	vault.add("ws-1", "cred-1", bearerCredential("Acme API", "tok-123"))

	signer := signingNode("sign.test", 1)
	reg := nodes.NewRegistry(fakeTrigger("trigger.test"), signer)
	st := newFakeStore(credentialGraph(nil))
	opts, _ := testOptions()
	opts.Credentials = vault

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.row("n2").Output[domain.MainHandle][0].JSON["credential"], "none",
		"a node with no credential must see nil, not an empty resolver")
	mustEqual(t, vault.callCount(), 0, "nothing should have been decrypted")
}

// TestCredentialResolutionIsLazyAndCached is two properties at once. Lazy: a
// node that never makes a request pays nothing, and a broken credential fails
// the node that uses it rather than one before it. Cached: three requests from
// one node must not refresh an OAuth token three times.
func TestCredentialResolutionIsLazyAndCached(t *testing.T) {
	vault := newFakeVault()
	vault.add("ws-1", "cred-1", bearerCredential("Acme API", "tok-123"))

	// The first node names the same credential and never uses it.
	idle := graphNode("n2", "idle.test", "Never calls out")
	idle.CredentialID = ptr("cred-1")
	signer := graphNode("n3", "sign.test", "Calls out three times")
	signer.CredentialID = ptr("cred-1")

	g := domain.Graph{
		Nodes: []domain.GraphNode{graphNode("n1", "trigger.test", "Start"), idle, signer},
		Edges: []domain.Edge{graphEdge("e1", "n1", "", "n2", ""), graphEdge("e2", "n2", "", "n3", "")},
	}

	var idleSawCredential bool
	idleNode := &fakeNode{desc: desc("idle.test", nodes.ModeOnce),
		exec: func(ec nodes.ExecContext) (nodes.Result, error) {
			idleSawCredential = ec.HasCredential()
			return nodes.MainSlice(ec.Items), nil
		}}
	reg := nodes.NewRegistry(fakeTrigger("trigger.test"), idleNode, signingNode("sign.test", 3))

	st := newFakeStore(g)
	opts, _ := testOptions()
	opts.Credentials = vault

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	if !idleSawCredential {
		t.Fatal("the idle node should still have been handed a resolver")
	}
	mustEqual(t, st.row("n3").Output[domain.MainHandle][0].JSON["auth"], "Bearer tok-123", "signed")
	// One resolution: none for the node that never applied, and one — not
	// three — for the node that applied three times.
	mustEqual(t, vault.callCount(), 1, "resolutions")
}

// TestCredentialFromAnotherWorkspaceIsRefused is the tenancy boundary. The
// workspace comes from the execution, so a graph naming another workspace's
// credential id resolves to nothing at all.
func TestCredentialFromAnotherWorkspaceIsRefused(t *testing.T) {
	vault := newFakeVault()
	vault.add("ws-other", "cred-other", bearerCredential("Someone else's key", "tok-secret"))

	reg := nodes.NewRegistry(fakeTrigger("trigger.test"), signingNode("sign.test", 1))
	st := newFakeStore(credentialGraph(ptr("cred-other")))
	opts, _ := testOptions()
	opts.Credentials = vault

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.execution().Status, domain.StatusFailed, "execution status")
	mustEqual(t, st.status("n2"), domain.StatusFailed, "the node that used it failed")

	row := st.row("n2")
	if row.Error == nil {
		t.Fatal("the failed node must carry an error a person can read")
	}
	mustContain(t, row.Error.Message, "no longer exists", "message")
	mustContain(t, row.Error.Message, "Call the API", "the message names the node")
	if row.Output != nil {
		t.Fatalf("a refused credential must produce no output, got %+v", row.Output)
	}
}

// The four failures a workflow author actually meets, each with a sentence that
// says what to do. An execution log full of "not found" is what this is for.
func TestCredentialFailuresAreLegibleInTheExecutionLog(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantSub string
	}{
		{"missing", domain.ErrNotFound, "pick another one in the node's settings"},
		{"unknown type", &credentials.ErrUnknownType{Type: "shopifyOAuth2"}, "does not know about"},
		{"never connected", &credentials.ErrNotConnected{Name: "Slack"}, "open it under Credentials and connect it"},
		{"will not decrypt", domain.Errorf(domain.ErrCodeInternal,
			"credentials: \"Acme\" could not be decrypted, which usually means CREDENTIAL_KEY changed"),
			"CREDENTIAL_KEY changed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vault := newFakeVault()
			vault.failWith = tc.err

			reg := nodes.NewRegistry(fakeTrigger("trigger.test"), signingNode("sign.test", 1))
			st := newFakeStore(credentialGraph(ptr("cred-1")))
			opts, _ := testOptions()
			opts.Credentials = vault

			mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

			row := st.row("n2")
			mustEqual(t, row.Status, domain.StatusFailed, "node status")
			if row.Error == nil {
				t.Fatal("no error recorded")
			}
			mustContain(t, row.Error.Message, tc.wantSub, "message")
			// Whatever went wrong, the node is named: the log is read node by
			// node and an unattributed sentence is not actionable.
			mustContain(t, row.Error.Message, "Call the API", "the message names the node")
		})
	}
}

// A build with no vault must not quietly send unauthenticated requests.
func TestNodeWithACredentialFailsWhenNoVaultIsConfigured(t *testing.T) {
	reg := nodes.NewRegistry(fakeTrigger("trigger.test"), signingNode("sign.test", 1))
	st := newFakeStore(credentialGraph(ptr("cred-1")))
	opts, _ := testOptions() // no Credentials

	mustNoError(t, New(st, reg, opts).Run(context.Background(), "exec-1"), "run")

	mustEqual(t, st.status("n2"), domain.StatusFailed, "node status")
	mustContain(t, st.row("n2").Error.Message, "no credential store is configured", "message")
}
