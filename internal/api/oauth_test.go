package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/credentials"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

/* ---------------------------------------------------------------------------
   The authorisation-code grant, end to end, against a provider we control.

   A stub provider is what makes this testable at all: the round trip is three
   HTTP exchanges that have to agree about a redirect URI and a one-use state,
   and no amount of unit testing the pieces proves that they do.
   --------------------------------------------------------------------------- */

// stubProvider plays the authorize and token endpoints.
type stubProvider struct {
	server *httptest.Server

	mu sync.Mutex
	// tokenReply is the JSON body the token endpoint answers with; tokenStatus
	// is its HTTP status. Both are settable so one test can make the provider
	// refuse and another can make it omit a refresh token.
	tokenReply  string
	tokenStatus int
	// exchanges records the form of every token request, which is how the
	// redirect_uri sent at exchange time is checked against the one sent at
	// authorisation time.
	exchanges []url.Values
}

func newStubProvider(t *testing.T) *stubProvider {
	t.Helper()
	p := &stubProvider{
		tokenReply:  `{"access_token":"at-1","refresh_token":"rt-1","token_type":"Bearer","expires_in":3600,"scope":"read write"}`,
		tokenStatus: http.StatusOK,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		// A real provider would render consent here; nothing in this suite
		// follows the URL, it only inspects it.
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		p.exchanges = append(p.exchanges, r.PostForm)
		reply, status := p.tokenReply, p.tokenStatus
		p.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer at-") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

func (p *stubProvider) reply(status int, body string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokenStatus, p.tokenReply = status, body
}

func (p *stubProvider) lastExchange() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.exchanges) == 0 {
		return nil
	}
	return p.exchanges[len(p.exchanges)-1]
}

func (p *stubProvider) credentialType() domain.CredentialType {
	return domain.CredentialType{
		Type: "stubOAuth2", Name: "Stub OAuth", Icon: "lock",
		Auth:    domain.AuthOAuth2,
		TestURL: p.server.URL + "/me",
		Fields: []domain.CredentialField{
			{Name: "clientId", Label: "Client ID", Type: "string", Required: true},
			{Name: "clientSecret", Label: "Client Secret", Type: "password", Required: true, Secret: true},
			{Name: "scopes", Label: "Scopes", Type: "string"},
		},
		OAuth2: &domain.OAuth2Config{
			AuthorizeURL: p.server.URL + "/authorize",
			TokenURL:     p.server.URL + "/token",
			Scopes:       []string{"read", "write"},
		},
	}
}

// oauthHarness is a harness with the stub provider's type registered and one
// unconnected credential of it already created.
func oauthHarness(t *testing.T) (*harness, *stubProvider, domain.CredentialSummary) {
	t.Helper()
	h := newHarness(t)
	p := newStubProvider(t)
	h.credTypes.Register(p.credentialType())
	cred := h.createCredential(t, "stubOAuth2", "Stub account", map[string]string{
		"clientId": "client-abc", "clientSecret": theSecret,
	})
	if cred.Connected {
		t.Fatal("a freshly created OAuth credential must not claim to be connected")
	}
	return h, p, cred
}

// startOAuth runs the start call and returns the consent URL and the state.
func startOAuth(t *testing.T, h *harness, credID string, body any) (*url.URL, string) {
	t.Helper()
	w := h.do(t, http.MethodPost, "/api/v1/credentials/"+credID+"/oauth/start", body, true)
	if w.Code != http.StatusOK {
		t.Fatalf("oauth start: status %d, body %s", w.Code, w.Body.String())
	}
	got := decode[struct {
		URL   string `json:"url"`
		State string `json:"state"`
	}](t, w)

	parsed, err := url.Parse(got.URL)
	if err != nil {
		t.Fatalf("parse consent URL %q: %v", got.URL, err)
	}
	return parsed, got.State
}

func TestOAuthStartBuildsTheProvidersConsentURL(t *testing.T) {
	h, p, cred := oauthHarness(t)

	consent, state := startOAuth(t, h, cred.ID, nil)
	q := consent.Query()

	if !strings.HasPrefix(consent.String(), p.server.URL+"/authorize") {
		t.Fatalf("the consent URL should point at the provider: %s", consent)
	}
	if q.Get("client_id") != "client-abc" {
		t.Fatalf("client_id %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://localhost:3000/oauth/callback" {
		t.Fatalf("redirect_uri %q must be the one computed from PublicBaseURL", q.Get("redirect_uri"))
	}
	if q.Get("scope") != "read write" {
		t.Fatalf("scope %q", q.Get("scope"))
	}
	if q.Get("state") == "" || q.Get("state") != state {
		t.Fatalf("state %q does not match the one returned to the caller (%q)", q.Get("state"), state)
	}
	if q.Get("response_type") != "code" {
		t.Fatalf("response_type %q", q.Get("response_type"))
	}
	// The consent URL is handed to a browser, and the client secret has no
	// business being in it.
	if strings.Contains(consent.String(), theSecret) {
		t.Fatal("the consent URL leaked the client secret")
	}
}

func TestOAuthStartRefusesATypeThatDoesNotUseOAuth(t *testing.T) {
	h := newHarness(t)
	cred := h.createCredential(t, "bearerAuth", "Static", map[string]string{"token": theSecret})

	w := h.do(t, http.MethodPost, "/api/v1/credentials/"+cred.ID+"/oauth/start", nil, true)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body %s", w.Code, w.Body.String())
	}
}

// TestOAuthRoundTrip is the test this whole flow exists for: start, callback,
// and a credential that afterwards signs a real request.
func TestOAuthRoundTripConnectsAndThenSignsRequests(t *testing.T) {
	h, p, cred := oauthHarness(t)

	_, state := startOAuth(t, h, cred.ID, map[string]any{"returnTo": "/credentials?highlight=1"})

	callback := h.do(t, http.MethodGet, "/oauth/callback?code=auth-code-1&state="+url.QueryEscape(state), nil, false)
	if callback.Code != http.StatusSeeOther {
		t.Fatalf("callback status %d, want 303; body %s", callback.Code, callback.Body.String())
	}
	// A browser redirect, not a rendered page: nothing the provider sent is
	// echoed into a body.
	if callback.Body.Len() != 0 {
		t.Fatalf("the callback must render nothing, got %q", callback.Body.String())
	}
	location, err := url.Parse(callback.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if location.Host != "localhost:3000" || location.Path != "/credentials" {
		t.Fatalf("the callback should return to where start asked: %s", location)
	}
	if location.Query().Get("oauth") != "connected" {
		t.Fatalf("the UI needs a flag it can show: %s", location)
	}
	if location.Query().Get("highlight") != "1" {
		t.Fatalf("the returnTo's own query should survive: %s", location)
	}

	// The exchange must repeat the redirect URI the authorisation used.
	if got := p.lastExchange().Get("redirect_uri"); got != "http://localhost:3000/oauth/callback" {
		t.Fatalf("token exchange redirect_uri %q differs from the authorisation's", got)
	}

	// The credential now reports itself connected, and still says nothing about
	// its secrets.
	w := h.do(t, http.MethodGet, "/api/v1/credentials/"+cred.ID, nil, true)
	summary := decode[credentialBody](t, w).Credential
	if !summary.Connected {
		t.Fatalf("connected false after a completed authorisation: %s", w.Body.String())
	}
	if summary.ExpiresAt == nil {
		t.Fatal("the UI warns before a token dies, so the expiry must be reported")
	}
	if strings.Contains(w.Body.String(), theSecret) || strings.Contains(w.Body.String(), "at-1") {
		t.Fatalf("the summary leaked a token: %s", w.Body.String())
	}

	// And the point of all of it: a request signed with the access token.
	resolved, err := h.creds.Resolve(t.Context(), testWorkspace, cred.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	req, err := http.NewRequest(http.MethodGet, p.server.URL+"/me", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := credentials.Apply(req, resolved); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer at-1" {
		t.Fatalf("Authorization %q, want Bearer at-1", got)
	}

	// End to end, through the endpoint a user actually presses.
	if got := decode[testResult](t, h.do(t, http.MethodPost, "/api/v1/credentials/"+cred.ID+"/test", nil, true)); !got.OK {
		t.Fatalf("the connected credential should test green: %+v", got)
	}
}

// TestOAuthCallbackRefusesAReplayedOrUnknownState covers the three ways a state
// can be bad. They share one code path — the state store's take() deletes on
// read and checks the expiry — and deliberately share one message, because
// telling an attacker which of the three they hit is telling them something.
func TestOAuthCallbackRefusesAReplayedOrUnknownState(t *testing.T) {
	h, _, cred := oauthHarness(t)
	_, state := startOAuth(t, h, cred.ID, nil)

	first := h.do(t, http.MethodGet, "/oauth/callback?code=c1&state="+url.QueryEscape(state), nil, false)
	if q := locationQuery(t, first); q.Get("oauth") != "connected" {
		t.Fatalf("the first callback should succeed: %v", q)
	}

	replay := h.do(t, http.MethodGet, "/oauth/callback?code=c1&state="+url.QueryEscape(state), nil, false)
	replayed := locationQuery(t, replay)
	if replayed.Get("oauth") != "error" {
		t.Fatalf("a replayed state must be refused: %v", replayed)
	}

	unknown := locationQuery(t, h.do(t, http.MethodGet, "/oauth/callback?code=c1&state=never-issued", nil, false))
	if unknown.Get("oauth") != "error" {
		t.Fatalf("an unknown state must be refused: %v", unknown)
	}
	if unknown.Get("message") != replayed.Get("message") {
		t.Fatalf("an unknown and a replayed state must be indistinguishable:\n%q\n%q",
			unknown.Get("message"), replayed.Get("message"))
	}
}

func TestOAuthCallbackTreatsA200WithAnErrorBodyAsAFailure(t *testing.T) {
	h, p, cred := oauthHarness(t)
	// Several real providers answer 200 with an error body; taking the status
	// code at face value would store an empty token and report success.
	p.reply(http.StatusOK, `{"error":"invalid_grant","error_description":"code already used"}`)

	_, state := startOAuth(t, h, cred.ID, nil)
	q := locationQuery(t, h.do(t, http.MethodGet, "/oauth/callback?code=c1&state="+url.QueryEscape(state), nil, false))

	if q.Get("oauth") != "error" {
		t.Fatalf("a 200 with an error body must not count as connected: %v", q)
	}
	if !strings.Contains(q.Get("message"), "invalid_grant") {
		t.Fatalf("the message should carry the provider's reason: %q", q.Get("message"))
	}

	summary := decode[credentialBody](t, h.do(t, http.MethodGet, "/api/v1/credentials/"+cred.ID, nil, true)).Credential
	if summary.Connected {
		t.Fatal("a failed exchange must leave the credential unconnected")
	}
}

func TestOAuthCallbackPassesTheProvidersOwnErrorThroughAsText(t *testing.T) {
	h, _, _ := oauthHarness(t)

	w := h.do(t, http.MethodGet,
		"/oauth/callback?error=access_denied&error_description=%3Cscript%3Ealert(1)%3C%2Fscript%3E", nil, false)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", w.Code)
	}
	// Nothing is rendered, so the provider's error cannot become markup on our
	// own origin however it is spelled.
	if w.Body.Len() != 0 {
		t.Fatalf("the callback must render nothing, got %q", w.Body.String())
	}
	q := locationQuery(t, w)
	if q.Get("oauth") != "error" || !strings.Contains(q.Get("message"), "access_denied") {
		t.Fatalf("the provider's refusal should reach the UI as text: %v", q)
	}
}

// TestOAuthCallbackWillNotRedirectOffOrigin closes the open redirect this flow
// would otherwise be: returnTo travels from a request body into a Location
// header, which is exactly the shape of one.
func TestOAuthCallbackWillNotRedirectOffOrigin(t *testing.T) {
	h, _, cred := oauthHarness(t)

	for _, returnTo := range []string{"https://evil.example/steal", "//evil.example/steal", "javascript:alert(1)"} {
		_, state := startOAuth(t, h, cred.ID, map[string]any{"returnTo": returnTo})
		w := h.do(t, http.MethodGet, "/oauth/callback?code=c1&state="+url.QueryEscape(state), nil, false)

		location, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatalf("parse Location: %v", err)
		}
		if location.Host != "localhost:3000" {
			t.Fatalf("returnTo %q escaped the origin: %s", returnTo, location)
		}
	}
}

// TestRefreshKeepsAnOmittedRefreshToken guards the failure that turns a working
// credential into one that must be re-authorised by hand: most providers omit
// the refresh token on a refresh, and dropping it loses the only way back.
func TestRefreshKeepsAnOmittedRefreshToken(t *testing.T) {
	h, p, cred := oauthHarness(t)

	// expires_in of one second puts the token inside the 60-second refresh
	// margin immediately, so the next Resolve has to refresh.
	p.reply(http.StatusOK, `{"access_token":"at-1","refresh_token":"rt-1","token_type":"Bearer","expires_in":1}`)
	_, state := startOAuth(t, h, cred.ID, nil)
	if q := locationQuery(t, h.do(t, http.MethodGet, "/oauth/callback?code=c1&state="+url.QueryEscape(state), nil, false)); q.Get("oauth") != "connected" {
		t.Fatalf("setup: the authorisation should have completed: %v", q)
	}

	// The refresh answers with a new access token and no refresh token at all.
	p.reply(http.StatusOK, `{"access_token":"at-2","token_type":"Bearer","expires_in":3600}`)

	resolved, err := h.creds.Resolve(t.Context(), testWorkspace, cred.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.lastExchange().Get("grant_type") != "refresh_token" {
		t.Fatalf("an expired token should have been refreshed, last exchange was %v", p.lastExchange().Get("grant_type"))
	}
	if resolved.Data.OAuth.AccessToken != "at-2" {
		t.Fatalf("access token %q, want the refreshed one", resolved.Data.OAuth.AccessToken)
	}
	if resolved.Data.OAuth.RefreshToken != "rt-1" {
		t.Fatalf("refresh token %q — an omitted one must keep its predecessor",
			resolved.Data.OAuth.RefreshToken)
	}
}

// locationQuery reads the query of a redirect's Location header.
func locationQuery(t *testing.T, w *httptest.ResponseRecorder) url.Values {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303; body %s", w.Code, w.Body.String())
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location %q: %v", w.Header().Get("Location"), err)
	}
	return location.Query()
}
