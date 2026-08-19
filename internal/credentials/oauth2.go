package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

/* ---------------------------------------------------------------------------
   The authorisation-code grant.

   Start builds the provider URL and remembers a one-time state; Complete
   exchanges the code and seals the tokens. State lives in memory rather than in
   the database because it is worthless after a few minutes and a lost one costs
   the user a single retry — but it is checked, because skipping it is how an
   attacker attaches their own account to someone else's credential.
   --------------------------------------------------------------------------- */

// stateTTL bounds how long an authorisation may sit unfinished.
const stateTTL = 15 * time.Minute

type pendingAuth struct {
	credentialID string
	workspaceID  string
	verifier     string
	returnTo     string
	expiresAt    time.Time
}

// stateStore holds in-flight authorisations.
type stateStore struct {
	mu      sync.Mutex
	pending map[string]pendingAuth
}

func newStateStore() *stateStore {
	return &stateStore{pending: map[string]pendingAuth{}}
}

func (s *stateStore) put(state string, p pendingAuth) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.pending[state] = p
}

func (s *stateStore) take(state string) (pendingAuth, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[state]
	// One use only: a replayed callback must not be able to re-bind anything.
	delete(s.pending, state)
	if !ok || time.Now().After(p.expiresAt) {
		return pendingAuth{}, false
	}
	return p, true
}

// sweep drops expired entries. Called on write, which is often enough for a map
// that only ever holds a handful of in-flight authorisations.
func (s *stateStore) sweep() {
	now := time.Now()
	for k, v := range s.pending {
		if now.After(v.expiresAt) {
			delete(s.pending, k)
		}
	}
}

// AuthStart is what the API hands the browser to begin an authorisation.
type AuthStart struct {
	// URL is the provider's consent screen.
	URL string `json:"url"`
	// State is returned so a test can complete the flow without a browser.
	State string `json:"state"`
}

// StartOAuth prepares an authorisation for one credential.
//
// redirectURI must be the exact string registered with the provider: an
// authorisation and its token exchange are both checked against it, and a
// mismatch is the single most common reason a working-looking flow fails.
func (s *Service) StartOAuth(ctx context.Context, workspaceID, id, redirectURI, returnTo string) (AuthStart, error) {
	rec, data, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return AuthStart{}, err
	}
	ct, ok := s.types.Get(rec.Type)
	if !ok {
		return AuthStart{}, &ErrUnknownType{Type: rec.Type}
	}
	if !ct.NeedsOAuth() {
		return AuthStart{}, domain.Errorf(domain.ErrCodeValidation,
			"the credential type %q does not use OAuth", ct.Name)
	}

	clientID := data.Field("clientId")
	if clientID == "" {
		return AuthStart{}, domain.Errorf(domain.ErrCodeValidation,
			"fill in the client ID before connecting")
	}

	state, err := randomToken()
	if err != nil {
		return AuthStart{}, err
	}

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	if scopes := scopeString(ct, data); scopes != "" {
		q.Set("scope", scopes)
	}
	for k, v := range ct.OAuth2.AuthParams {
		q.Set(k, v)
	}

	var verifier string
	if ct.OAuth2.UsePKCE {
		if verifier, err = randomToken(); err != nil {
			return AuthStart{}, err
		}
		sum := sha256.Sum256([]byte(verifier))
		q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
		q.Set("code_challenge_method", "S256")
	}

	s.states().put(state, pendingAuth{
		credentialID: id,
		workspaceID:  workspaceID,
		verifier:     verifier,
		returnTo:     returnTo,
		expiresAt:    time.Now().Add(stateTTL),
	})

	sep := "?"
	if strings.Contains(ct.OAuth2.AuthorizeURL, "?") {
		sep = "&"
	}
	return AuthStart{URL: ct.OAuth2.AuthorizeURL + sep + q.Encode(), State: state}, nil
}

// AuthResult is the outcome of a completed authorisation.
type AuthResult struct {
	Credential domain.CredentialSummary
	ReturnTo   string
}

// CompleteOAuth exchanges the code for tokens and seals them.
func (s *Service) CompleteOAuth(ctx context.Context, state, code, redirectURI string) (AuthResult, error) {
	pending, ok := s.states().take(state)
	if !ok {
		// Deliberately the same answer for an unknown, expired and replayed
		// state: distinguishing them tells an attacker which one they hit.
		return AuthResult{}, domain.Errorf(domain.ErrCodeValidation,
			"this authorisation link is no longer valid; start again from the credential")
	}

	rec, data, err := s.load(ctx, pending.workspaceID, pending.credentialID)
	if err != nil {
		return AuthResult{}, err
	}
	ct, ok := s.types.Get(rec.Type)
	if !ok {
		return AuthResult{}, &ErrUnknownType{Type: rec.Type}
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	if pending.verifier != "" {
		form.Set("code_verifier", pending.verifier)
	}

	tokens, err := s.exchange(ctx, ct, data, form, nil)
	if err != nil {
		return AuthResult{}, err
	}

	data.OAuth = tokens
	if err := s.persistTokens(ctx, pending.workspaceID, pending.credentialID, data); err != nil {
		return AuthResult{}, err
	}

	updated, err := s.store.Credential(ctx, pending.workspaceID, pending.credentialID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{
		Credential: s.summarise(updated, data, 0),
		ReturnTo:   pending.returnTo,
	}, nil
}

// refresh trades the refresh token for a new access token.
func (s *Service) refresh(ctx context.Context, ct domain.CredentialType, data domain.CredentialData) (*domain.OAuth2Tokens, error) {
	if data.OAuth == nil || data.OAuth.RefreshToken == "" {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"this credential's access has expired and it has no refresh token; connect it again")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", data.OAuth.RefreshToken)

	tokens, err := s.exchange(ctx, ct, data, form, data.OAuth)
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

// exchange posts to the token endpoint and reads the reply.
//
// previous carries the tokens being replaced: providers commonly omit the
// refresh token on a refresh, and dropping it would turn a working credential
// into one that must be re-authorised by hand.
func (s *Service) exchange(ctx context.Context, ct domain.CredentialType, data domain.CredentialData,
	form url.Values, previous *domain.OAuth2Tokens) (*domain.OAuth2Tokens, error) {

	clientID := data.Field("clientId")
	clientSecret := data.Field("clientSecret")
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ct.OAuth2.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("credentials: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if clientSecret != "" {
		// Sent both ways on purpose: the spec allows either, and providers
		// disagree about which they accept.
		req.Header.Set("Authorization", basicAuthHeader(clientID, clientSecret))
	}

	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("credentials: token request failed: %w", err)
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"the provider rejected the token request (HTTP %d): %s",
			res.StatusCode, truncate(string(body), 300))
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"the provider's token reply was not JSON: %s", truncate(string(body), 200))
	}
	// Several providers answer 200 with an error body.
	if payload.Error != "" {
		msg := payload.Error
		if payload.ErrorDesc != "" {
			msg += ": " + payload.ErrorDesc
		}
		return nil, domain.Errorf(domain.ErrCodeValidation, "the provider refused: %s", msg)
	}
	if payload.AccessToken == "" {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"the provider returned no access token")
	}

	out := &domain.OAuth2Tokens{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
		Scope:        payload.Scope,
	}
	if payload.ExpiresIn > 0 {
		out.ExpiresAt = s.now().Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	if out.RefreshToken == "" && previous != nil {
		out.RefreshToken = previous.RefreshToken
	}
	if out.Scope == "" && previous != nil {
		out.Scope = previous.Scope
	}
	return out, nil
}

// scopeString is the scope list to request: the credential's own override when
// the user set one, and the type's default otherwise.
func scopeString(ct domain.CredentialType, data domain.CredentialData) string {
	if custom := strings.TrimSpace(data.Field("scopes")); custom != "" {
		return custom
	}
	return strings.Join(ct.OAuth2.Scopes, " ")
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("credentials: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// states lazily builds the in-flight state store, so a zero Service is usable
// in a test that never touches OAuth.
func (s *Service) states() *stateStore {
	s.stateOnce.Do(func() { s.state = newStateStore() })
	return s.state
}
