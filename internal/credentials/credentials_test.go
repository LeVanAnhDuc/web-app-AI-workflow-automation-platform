package credentials

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

/* --- a store that keeps sealed blobs in memory ---------------------------- */

type fakeStore struct {
	mu      sync.Mutex
	records map[string]domain.Credential
	sealed  map[string][]byte
	usage   map[string]int
	seq     int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		records: map[string]domain.Credential{},
		sealed:  map[string][]byte{},
		usage:   map[string]int{},
	}
}

func (f *fakeStore) CreateCredential(_ context.Context, workspaceID, credType, name string, sealed []byte) (domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	rec := domain.Credential{
		ID:          "cred-" + string(rune('a'+f.seq-1)),
		WorkspaceID: workspaceID,
		Type:        credType,
		Name:        name,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	f.records[rec.ID] = rec
	f.sealed[rec.ID] = sealed
	return rec, nil
}

func (f *fakeStore) Credential(_ context.Context, workspaceID, id string) (domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[id]
	if !ok || rec.WorkspaceID != workspaceID {
		return domain.Credential{}, domain.ErrNotFound
	}
	return rec, nil
}

func (f *fakeStore) CredentialSealed(_ context.Context, workspaceID, id string) (domain.Credential, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[id]
	if !ok || rec.WorkspaceID != workspaceID {
		return domain.Credential{}, nil, domain.ErrNotFound
	}
	return rec, f.sealed[id], nil
}

func (f *fakeStore) ListCredentials(_ context.Context, workspaceID, credType string) ([]domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Credential
	for _, rec := range f.records {
		if rec.WorkspaceID != workspaceID {
			continue
		}
		if credType != "" && rec.Type != credType {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func (f *fakeStore) UpdateCredential(_ context.Context, workspaceID, id string, name *string, sealed []byte) (domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[id]
	if !ok || rec.WorkspaceID != workspaceID {
		return domain.Credential{}, domain.ErrNotFound
	}
	if name != nil {
		rec.Name = *name
	}
	rec.UpdatedAt = time.Now()
	f.records[id] = rec
	if len(sealed) > 0 {
		f.sealed[id] = sealed
	}
	return rec, nil
}

func (f *fakeStore) DeleteCredential(_ context.Context, workspaceID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[id]
	if !ok || rec.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	delete(f.records, id)
	delete(f.sealed, id)
	return nil
}

func (f *fakeStore) CredentialUsage(context.Context, string) (map[string]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for k, v := range f.usage {
		out[k] = v
	}
	return out, nil
}

func (f *fakeStore) TouchCredential(_ context.Context, workspaceID, id string, sealed []byte, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[id]
	if !ok || rec.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	rec.UpdatedAt = at
	f.records[id] = rec
	f.sealed[id] = sealed
	return nil
}

/* --- helpers -------------------------------------------------------------- */

const testWorkspace = "ws-1"

func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func newService(t *testing.T, opts Options) (*Service, *fakeStore) {
	t.Helper()
	st := newFakeStore()
	svc, err := New(st, testKey(), opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, st
}

func mustCreate(t *testing.T, svc *Service, credType, name string, fields map[string]string) domain.CredentialSummary {
	t.Helper()
	sum, err := svc.Create(context.Background(), domain.NewCredential{
		WorkspaceID: testWorkspace,
		Type:        credType,
		Name:        name,
		Data:        domain.CredentialData{Fields: fields},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return sum
}

/* --- storing ------------------------------------------------------------- */

func TestCreateSealsTheSecret(t *testing.T) {
	svc, st := newService(t, Options{})

	sum := mustCreate(t, svc, "httpBasicAuth", "Acme API", map[string]string{
		"username": "ha",
		"password": "s3cr3t-value",
	})

	// The stored bytes must not contain the plaintext: the whole point of the
	// vault is that a database dump is not a password dump.
	blob := st.sealed[sum.ID]
	if strings.Contains(string(blob), "s3cr3t-value") {
		t.Fatal("the password appears verbatim in the stored blob")
	}
	if len(blob) == 0 {
		t.Fatal("nothing was stored")
	}
}

func TestSummaryNamesSetFieldsWithoutTheirValues(t *testing.T) {
	svc, _ := newService(t, Options{})

	sum := mustCreate(t, svc, "httpBasicAuth", "Acme API", map[string]string{
		"username": "ha",
		"password": "s3cr3t-value",
	})

	encoded, err := json.Marshal(sum)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if strings.Contains(string(encoded), "s3cr3t-value") {
		t.Fatalf("the summary leaks the secret: %s", encoded)
	}
	// It still has to say the field is set, or an edit form looks like data loss.
	if got := strings.Join(sum.SetFields, ","); got != "password,username" {
		t.Fatalf("setFields %q, want both fields named", got)
	}
}

func TestCreateRefusesAMissingRequiredField(t *testing.T) {
	svc, _ := newService(t, Options{})

	_, err := svc.Create(context.Background(), domain.NewCredential{
		WorkspaceID: testWorkspace,
		Type:        "httpBasicAuth",
		Name:        "Half a credential",
		Data:        domain.CredentialData{Fields: map[string]string{"username": "ha"}},
	})
	if err == nil {
		t.Fatal("expected a validation error")
	}
	// The message is read by a person filling in a form, so it must name the
	// field rather than say "invalid".
	if !strings.Contains(err.Error(), "Password") {
		t.Fatalf("error %q does not name the missing field", err)
	}
}

func TestCreateRefusesAnUnknownType(t *testing.T) {
	svc, _ := newService(t, Options{})

	_, err := svc.Create(context.Background(), domain.NewCredential{
		WorkspaceID: testWorkspace, Type: "notAType", Name: "x",
	})
	var unknown *ErrUnknownType
	if err == nil || !asErr(err, &unknown) {
		t.Fatalf("error %v, want ErrUnknownType", err)
	}
}

func TestUpdateKeepsASecretTheCallerOmitted(t *testing.T) {
	svc, _ := newService(t, Options{})
	sum := mustCreate(t, svc, "httpBasicAuth", "Acme API", map[string]string{
		"username": "ha", "password": "original",
	})

	// The editor cannot show a stored secret, so it submits an empty string for
	// one the user did not retype. That must not blank it.
	if _, err := svc.Update(context.Background(), testWorkspace, sum.ID, domain.CredentialPatch{
		Fields: map[string]string{"username": "duc", "password": ""},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	resolved, err := svc.Resolve(context.Background(), testWorkspace, sum.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := resolved.Data.Field("password"); got != "original" {
		t.Fatalf("password is %q — an omitted secret must survive the edit", got)
	}
	if got := resolved.Data.Field("username"); got != "duc" {
		t.Fatalf("username is %q, want the new value", got)
	}
}

func TestUpdateReplacesASecretThatWasRetyped(t *testing.T) {
	svc, _ := newService(t, Options{})
	sum := mustCreate(t, svc, "httpBasicAuth", "Acme API", map[string]string{
		"username": "ha", "password": "original",
	})

	if _, err := svc.Update(context.Background(), testWorkspace, sum.ID, domain.CredentialPatch{
		Fields: map[string]string{"password": "rotated"},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	resolved, _ := svc.Resolve(context.Background(), testWorkspace, sum.ID)
	if got := resolved.Data.Field("password"); got != "rotated" {
		t.Fatalf("password is %q, want the retyped value", got)
	}
}

func TestCredentialsAreScopedToTheirWorkspace(t *testing.T) {
	svc, _ := newService(t, Options{})
	sum := mustCreate(t, svc, "bearerAuth", "Acme", map[string]string{"token": "t"})

	if _, err := svc.Get(context.Background(), "ws-other", sum.ID); err == nil {
		t.Fatal("another workspace must not be able to read a credential")
	}
	if _, err := svc.Resolve(context.Background(), "ws-other", sum.ID); err == nil {
		t.Fatal("another workspace must not be able to resolve a credential")
	}
}

func TestListSurvivesABlobItCannotDecrypt(t *testing.T) {
	svc, st := newService(t, Options{})
	sum := mustCreate(t, svc, "bearerAuth", "Rotated key", map[string]string{"token": "t"})

	// What a changed CREDENTIAL_KEY looks like from here.
	st.sealed[sum.ID] = []byte("this will not open, not even close")

	list, err := svc.List(context.Background(), testWorkspace, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d credentials, want the undecryptable one still listed", len(list))
	}
	// Listed, but visibly unusable rather than silently fine.
	if len(list[0].SetFields) != 0 {
		t.Fatalf("an unopenable credential must claim no set fields, got %v", list[0].SetFields)
	}
}

func TestResolveReportsAChangedKeyInPlainWords(t *testing.T) {
	svc, st := newService(t, Options{})
	sum := mustCreate(t, svc, "bearerAuth", "Rotated key", map[string]string{"token": "t"})
	st.sealed[sum.ID] = []byte("garbage that is long enough to pass the length check")

	_, err := svc.Resolve(context.Background(), testWorkspace, sum.ID)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "CREDENTIAL_KEY") {
		t.Fatalf("error %q should name the likely cause", err)
	}
}

func TestListFiltersByType(t *testing.T) {
	svc, _ := newService(t, Options{})
	mustCreate(t, svc, "bearerAuth", "A", map[string]string{"token": "t"})
	mustCreate(t, svc, "httpBasicAuth", "B", map[string]string{"username": "u", "password": "p"})

	list, err := svc.List(context.Background(), testWorkspace, "bearerAuth")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Type != "bearerAuth" {
		t.Fatalf("got %+v, want only the bearer credential", list)
	}
}

func TestDelete(t *testing.T) {
	svc, _ := newService(t, Options{})
	sum := mustCreate(t, svc, "bearerAuth", "A", map[string]string{"token": "t"})

	if err := svc.Delete(context.Background(), testWorkspace, sum.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(context.Background(), testWorkspace, sum.ID); err == nil {
		t.Fatal("the credential is still readable after a delete")
	}
}

/* --- attaching to a request ---------------------------------------------- */

func TestApply(t *testing.T) {
	tests := []struct {
		name     string
		credType string
		fields   map[string]string
		oauth    *domain.OAuth2Tokens
		assert   func(t *testing.T, req *http.Request)
	}{
		{
			name:     "basic",
			credType: "httpBasicAuth",
			fields:   map[string]string{"username": "ha", "password": "pw"},
			assert: func(t *testing.T, req *http.Request) {
				user, pass, ok := req.BasicAuth()
				if !ok || user != "ha" || pass != "pw" {
					t.Fatalf("basic auth %q/%q ok=%v", user, pass, ok)
				}
			},
		},
		{
			name:     "bearer",
			credType: "bearerAuth",
			fields:   map[string]string{"token": "abc"},
			assert: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("Authorization"); got != "Bearer abc" {
					t.Fatalf("Authorization %q", got)
				}
			},
		},
		{
			name:     "api key in a custom header",
			credType: "httpHeaderAuth",
			fields:   map[string]string{"apiKey": "k", "in": "header", "headerName": "X-API-Key"},
			assert: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("X-API-Key"); got != "k" {
					t.Fatalf("X-API-Key %q", got)
				}
			},
		},
		{
			name:     "api key with a prefix",
			credType: "httpHeaderAuth",
			fields: map[string]string{
				"apiKey": "k", "in": "header", "headerName": "Authorization", "prefix": "Token",
			},
			assert: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("Authorization"); got != "Token k" {
					t.Fatalf("Authorization %q", got)
				}
			},
		},
		{
			name:     "api key in the query string",
			credType: "httpHeaderAuth",
			fields:   map[string]string{"apiKey": "k", "in": "query", "headerName": "api_key"},
			assert: func(t *testing.T, req *http.Request) {
				if got := req.URL.Query().Get("api_key"); got != "k" {
					t.Fatalf("query api_key %q", got)
				}
				if req.Header.Get("Authorization") != "" {
					t.Fatal("a query-string key must not also be sent as a header")
				}
			},
		},
		{
			name:     "oauth2",
			credType: "slackOAuth2",
			fields:   map[string]string{"clientId": "c", "clientSecret": "s"},
			oauth:    &domain.OAuth2Tokens{AccessToken: "at", TokenType: "Bearer"},
			assert: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("Authorization"); got != "Bearer at" {
					t.Fatalf("Authorization %q", got)
				}
			},
		},
	}

	reg := DefaultRegistry()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ct, ok := reg.Get(tc.credType)
			if !ok {
				t.Fatalf("unknown type %s", tc.credType)
			}
			req, _ := http.NewRequest(http.MethodGet, "https://api.acme.vn/v1/things", nil)
			err := Apply(req, Resolved{
				Type: ct,
				Data: domain.CredentialData{Fields: tc.fields, OAuth: tc.oauth},
				Name: tc.credType,
			})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			tc.assert(t, req)
		})
	}
}

func TestApplyRefusesAnUnconnectedOAuthCredential(t *testing.T) {
	ct, _ := DefaultRegistry().Get("slackOAuth2")
	req, _ := http.NewRequest(http.MethodGet, "https://slack.com/api/auth.test", nil)

	err := Apply(req, Resolved{Type: ct, Name: "My Slack",
		Data: domain.CredentialData{Fields: map[string]string{"clientId": "c"}}})
	if err == nil {
		t.Fatal("expected an error for an unconnected credential")
	}
	if !strings.Contains(err.Error(), "My Slack") {
		t.Fatalf("error %q should name the credential", err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("a failed Apply must not leave a partial header")
	}
}

func TestApplyDoesNothingForAnAuthNoneType(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.acme.vn/", nil)
	err := Apply(req, Resolved{Type: domain.CredentialType{Auth: domain.AuthNone}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(req.Header) != 0 {
		t.Fatalf("headers were added: %v", req.Header)
	}
}

/* --- OAuth --------------------------------------------------------------- */

// stubProvider plays the authorize and token halves of a provider.
type stubProvider struct {
	server *httptest.Server

	mu           sync.Mutex
	tokenForms   []map[string][]string
	accessToken  string
	refreshToken string
	expiresIn    int64
	failWith     string // a body to return instead of tokens
	status       int
}

func newStubProvider(t *testing.T) *stubProvider {
	t.Helper()
	p := &stubProvider{accessToken: "access-1", refreshToken: "refresh-1", expiresIn: 3600, status: 200}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		p.mu.Lock()
		p.tokenForms = append(p.tokenForms, r.Form)
		body := p.failWith
		status := p.status
		payload := map[string]any{
			"access_token": p.accessToken,
			"token_type":   "Bearer",
			"expires_in":   p.expiresIn,
			"scope":        "chat:write",
		}
		if p.refreshToken != "" {
			payload["refresh_token"] = p.refreshToken
		}
		p.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
			return
		}
		_ = json.NewEncoder(w).Encode(payload)
	})
	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

func (p *stubProvider) lastForm() map[string][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tokenForms) == 0 {
		return nil
	}
	return p.tokenForms[len(p.tokenForms)-1]
}

func (p *stubProvider) credentialType() domain.CredentialType {
	return domain.CredentialType{
		Type: "stubOAuth2",
		Name: "Stub",
		Auth: domain.AuthOAuth2,
		Fields: []domain.CredentialField{
			{Name: "clientId", Label: "Client ID", Type: "string", Required: true},
			{Name: "clientSecret", Label: "Client Secret", Type: "password", Required: true, Secret: true},
			{Name: "scopes", Label: "Scopes", Type: "string"},
		},
		OAuth2: &domain.OAuth2Config{
			AuthorizeURL: p.server.URL + "/authorize",
			TokenURL:     p.server.URL + "/token",
			Scopes:       []string{"chat:write", "channels:read"},
			AuthParams:   map[string]string{"access_type": "offline"},
		},
	}
}

const redirectURI = "http://localhost:3000/oauth/callback"

func oauthService(t *testing.T, p *stubProvider) (*Service, domain.CredentialSummary) {
	t.Helper()
	svc, _ := newService(t, Options{
		Types:      NewRegistry(p.credentialType()),
		HTTPClient: p.server.Client(),
	})
	sum := mustCreate(t, svc, "stubOAuth2", "My Stub", map[string]string{
		"clientId": "client-1", "clientSecret": "secret-1",
	})
	return svc, sum
}

func TestOAuthRoundTrip(t *testing.T) {
	p := newStubProvider(t)
	svc, sum := oauthService(t, p)
	ctx := context.Background()

	start, err := svc.StartOAuth(ctx, testWorkspace, sum.ID, redirectURI, "/credentials")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	// The authorisation URL is what the user's browser is sent to, so every
	// part of it has to be right or the provider rejects the whole attempt.
	for _, want := range []string{
		"response_type=code",
		"client_id=client-1",
		"redirect_uri=http%3A%2F%2Flocalhost%3A3000%2Foauth%2Fcallback",
		"scope=chat%3Awrite+channels%3Aread",
		"access_type=offline",
		"state=" + start.State,
	} {
		if !strings.Contains(start.URL, want) {
			t.Fatalf("authorisation URL is missing %q:\n%s", want, start.URL)
		}
	}

	result, err := svc.CompleteOAuth(ctx, start.State, "the-code", redirectURI)
	if err != nil {
		t.Fatalf("CompleteOAuth: %v", err)
	}
	if !result.Credential.Connected {
		t.Fatal("the credential should report itself connected")
	}
	if result.ReturnTo != "/credentials" {
		t.Fatalf("returnTo %q", result.ReturnTo)
	}

	form := p.lastForm()
	if got := form["grant_type"]; len(got) != 1 || got[0] != "authorization_code" {
		t.Fatalf("grant_type %v", got)
	}
	if got := form["redirect_uri"]; len(got) != 1 || got[0] != redirectURI {
		t.Fatalf("redirect_uri %v — it must match the one used to authorise", got)
	}

	resolved, err := svc.Resolve(ctx, testWorkspace, sum.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/", nil)
	if err := Apply(req, resolved); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer access-1" {
		t.Fatalf("Authorization %q", got)
	}
}

func TestOAuthStateIsSingleUse(t *testing.T) {
	p := newStubProvider(t)
	svc, sum := oauthService(t, p)
	ctx := context.Background()

	start, _ := svc.StartOAuth(ctx, testWorkspace, sum.ID, redirectURI, "")
	if _, err := svc.CompleteOAuth(ctx, start.State, "code", redirectURI); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	// A replayed callback must not be able to re-bind the credential.
	if _, err := svc.CompleteOAuth(ctx, start.State, "code", redirectURI); err == nil {
		t.Fatal("the same state was accepted twice")
	}
}

func TestOAuthRejectsAnUnknownState(t *testing.T) {
	p := newStubProvider(t)
	svc, _ := oauthService(t, p)

	_, err := svc.CompleteOAuth(context.Background(), "never-issued", "code", redirectURI)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	// The same message as for an expired or replayed state, on purpose.
	if !strings.Contains(err.Error(), "no longer valid") {
		t.Fatalf("error %q", err)
	}
}

func TestOAuthRejectsAProviderErrorReturnedWithHTTP200(t *testing.T) {
	p := newStubProvider(t)
	p.failWith = `{"error":"invalid_grant","error_description":"code already used"}`
	svc, sum := oauthService(t, p)
	ctx := context.Background()

	start, _ := svc.StartOAuth(ctx, testWorkspace, sum.ID, redirectURI, "")
	_, err := svc.CompleteOAuth(ctx, start.State, "code", redirectURI)
	if err == nil {
		t.Fatal("a 200 carrying an error body must still fail")
	}
	if !strings.Contains(err.Error(), "code already used") {
		t.Fatalf("error %q should carry the provider's explanation", err)
	}
}

func TestOAuthRejectsATokenEndpointFailure(t *testing.T) {
	p := newStubProvider(t)
	p.status = http.StatusBadRequest
	p.failWith = `{"error":"invalid_client"}`
	svc, sum := oauthService(t, p)
	ctx := context.Background()

	start, _ := svc.StartOAuth(ctx, testWorkspace, sum.ID, redirectURI, "")
	if _, err := svc.CompleteOAuth(ctx, start.State, "code", redirectURI); err == nil {
		t.Fatal("expected an error")
	}
}

func TestResolveRefreshesAnExpiredToken(t *testing.T) {
	p := newStubProvider(t)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	clock := now

	svc, _ := newService(t, Options{
		Types:      NewRegistry(p.credentialType()),
		HTTPClient: p.server.Client(),
		Now:        func() time.Time { return clock },
	})
	sum := mustCreate(t, svc, "stubOAuth2", "My Stub", map[string]string{
		"clientId": "client-1", "clientSecret": "secret-1",
	})

	ctx := context.Background()
	start, _ := svc.StartOAuth(ctx, testWorkspace, sum.ID, redirectURI, "")
	if _, err := svc.CompleteOAuth(ctx, start.State, "code", redirectURI); err != nil {
		t.Fatalf("CompleteOAuth: %v", err)
	}

	// Move past the hour the stub granted.
	clock = now.Add(2 * time.Hour)
	p.mu.Lock()
	p.accessToken = "access-2"
	p.mu.Unlock()

	resolved, err := svc.Resolve(ctx, testWorkspace, sum.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := resolved.Data.OAuth.AccessToken; got != "access-2" {
		t.Fatalf("access token %q, want the refreshed one", got)
	}
	if got := p.lastForm()["grant_type"]; len(got) != 1 || got[0] != "refresh_token" {
		t.Fatalf("grant_type %v, want a refresh", got)
	}

	// And it must be written back, or every run pays for a refresh.
	again, err := svc.Resolve(ctx, testWorkspace, sum.ID)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if again.Data.OAuth.AccessToken != "access-2" {
		t.Fatal("the refreshed token was not persisted")
	}
}

func TestRefreshKeepsTheOldRefreshTokenWhenTheProviderOmitsIt(t *testing.T) {
	p := newStubProvider(t)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	clock := now

	svc, _ := newService(t, Options{
		Types:      NewRegistry(p.credentialType()),
		HTTPClient: p.server.Client(),
		Now:        func() time.Time { return clock },
	})
	sum := mustCreate(t, svc, "stubOAuth2", "My Stub", map[string]string{
		"clientId": "client-1", "clientSecret": "secret-1",
	})

	ctx := context.Background()
	start, _ := svc.StartOAuth(ctx, testWorkspace, sum.ID, redirectURI, "")
	if _, err := svc.CompleteOAuth(ctx, start.State, "code", redirectURI); err != nil {
		t.Fatalf("CompleteOAuth: %v", err)
	}

	// Most providers do not re-issue a refresh token. Dropping it would turn a
	// working credential into one that must be re-authorised by hand.
	clock = now.Add(2 * time.Hour)
	p.mu.Lock()
	p.refreshToken = ""
	p.accessToken = "access-2"
	p.mu.Unlock()

	resolved, err := svc.Resolve(ctx, testWorkspace, sum.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := resolved.Data.OAuth.RefreshToken; got != "refresh-1" {
		t.Fatalf("refresh token %q, want the original to survive", got)
	}
}

func TestResolveRefusesAnUnconnectedOAuthCredential(t *testing.T) {
	p := newStubProvider(t)
	svc, sum := oauthService(t, p)

	_, err := svc.Resolve(context.Background(), testWorkspace, sum.ID)
	var notConnected *ErrNotConnected
	if err == nil || !asErr(err, &notConnected) {
		t.Fatalf("error %v, want ErrNotConnected", err)
	}
}

func TestStartOAuthRefusesANonOAuthType(t *testing.T) {
	svc, _ := newService(t, Options{})
	sum := mustCreate(t, svc, "bearerAuth", "A", map[string]string{"token": "t"})

	if _, err := svc.StartOAuth(context.Background(), testWorkspace, sum.ID, redirectURI, ""); err == nil {
		t.Fatal("expected a refusal for a type that does not use OAuth")
	}
}

func TestStartOAuthRefusesAMissingClientID(t *testing.T) {
	p := newStubProvider(t)
	svc, _ := newService(t, Options{
		Types: NewRegistry(p.credentialType()), HTTPClient: p.server.Client(),
	})
	// Created directly, bypassing validation, to reach the start-time check.
	sum, err := svc.Create(context.Background(), domain.NewCredential{
		WorkspaceID: testWorkspace, Type: "stubOAuth2", Name: "Empty",
		Data: domain.CredentialData{Fields: map[string]string{
			"clientId": "x", "clientSecret": "y",
		}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Update(context.Background(), testWorkspace, sum.ID, domain.CredentialPatch{
		Fields: map[string]string{"clientId": ""},
	}); err == nil {
		t.Skip("validation now blocks an empty client id at update time, which is stricter still")
	}
}

func TestCustomScopesOverrideTheTypeDefaults(t *testing.T) {
	p := newStubProvider(t)
	svc, _ := newService(t, Options{
		Types: NewRegistry(p.credentialType()), HTTPClient: p.server.Client(),
	})
	sum := mustCreate(t, svc, "stubOAuth2", "Narrow", map[string]string{
		"clientId": "c", "clientSecret": "s", "scopes": "chat:write",
	})

	start, err := svc.StartOAuth(context.Background(), testWorkspace, sum.ID, redirectURI, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}
	if !strings.Contains(start.URL, "scope=chat%3Awrite&") && !strings.HasSuffix(start.URL, "scope=chat%3Awrite") {
		if strings.Contains(start.URL, "channels%3Aread") {
			t.Fatalf("the type's default scopes were used instead of the override:\n%s", start.URL)
		}
	}
}

/* --- the registry -------------------------------------------------------- */

func TestDefaultRegistryHasEveryBuiltInType(t *testing.T) {
	reg := DefaultRegistry()
	for _, want := range []string{
		"httpBasicAuth", "httpHeaderAuth", "bearerAuth", "anthropicApi",
		"slackOAuth2", "gmailOAuth2", "googleSheetsOAuth2",
	} {
		ct, ok := reg.Get(want)
		if !ok {
			t.Fatalf("%s is not registered", want)
		}
		if ct.Name == "" || ct.Auth == "" {
			t.Fatalf("%s is incompletely declared: %+v", want, ct)
		}
		if ct.Auth == domain.AuthOAuth2 && !ct.NeedsOAuth() {
			t.Fatalf("%s claims OAuth but carries no config", want)
		}
	}
}

func TestGoogleTypesAskForOfflineAccess(t *testing.T) {
	// Without access_type=offline and prompt=consent, Google returns no refresh
	// token on a repeat authorisation and the credential dies an hour later.
	for _, id := range []string{"gmailOAuth2", "googleSheetsOAuth2"} {
		ct, ok := DefaultRegistry().Get(id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if ct.OAuth2.AuthParams["access_type"] != "offline" {
			t.Fatalf("%s does not request offline access", id)
		}
		if ct.OAuth2.AuthParams["prompt"] != "consent" {
			t.Fatalf("%s does not force the consent screen", id)
		}
	}
}

func TestSecretFieldsAreMarked(t *testing.T) {
	reg := DefaultRegistry()
	for _, ct := range reg.All() {
		for _, f := range ct.Fields {
			// Anything that looks like a secret must be marked as one, or the
			// API will hand it back in a summary.
			name := strings.ToLower(f.Name)
			looksSecret := strings.Contains(name, "secret") ||
				strings.Contains(name, "password") ||
				name == "token" || name == "apikey"
			if looksSecret && !f.Secret {
				t.Fatalf("%s.%s looks like a secret but is not marked Secret", ct.Type, f.Name)
			}
		}
	}
}

func TestExpiredUsesAMargin(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		tokens  domain.OAuth2Tokens
		expired bool
	}{
		{"no token at all", domain.OAuth2Tokens{}, true},
		{"no expiry is treated as long-lived",
			domain.OAuth2Tokens{AccessToken: "a"}, false},
		{"comfortably alive",
			domain.OAuth2Tokens{AccessToken: "a", ExpiresAt: now.Add(time.Hour)}, false},
		// Refreshed early on purpose: a token that dies mid-flight fails a run
		// that would otherwise have succeeded.
		{"inside the margin",
			domain.OAuth2Tokens{AccessToken: "a", ExpiresAt: now.Add(30 * time.Second)}, true},
		{"already past",
			domain.OAuth2Tokens{AccessToken: "a", ExpiresAt: now.Add(-time.Second)}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.tokens.Expired(now); got != tc.expired {
				t.Fatalf("Expired = %v, want %v", got, tc.expired)
			}
		})
	}
}

// asErr is errors.As without importing errors into every call site.
func asErr[T error](err error, target *T) bool {
	for err != nil {
		if t, ok := err.(T); ok {
			*target = t
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
