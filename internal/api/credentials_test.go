package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// theSecret is deliberately distinctive: every test in this file greps whole
// response bodies for it, and a generic value could match by accident.
const theSecret = "sk-ant-do-not-leak-me-98f3a1"

type credentialBody struct {
	Credential domain.CredentialSummary `json:"credential"`
}

func (h *harness) createCredential(t *testing.T, credType, name string, fields map[string]string) domain.CredentialSummary {
	t.Helper()
	w := h.do(t, http.MethodPost, "/api/v1/credentials", map[string]any{
		"type": credType, "name": name, "fields": fields,
	}, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("create credential: status %d, body %s", w.Code, w.Body.String())
	}
	return decode[credentialBody](t, w).Credential
}

// --- shape ------------------------------------------------------------------

func TestCredentialTypesCarryTheFormsAndTheRedirectURI(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodGet, "/api/v1/credential-types", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	body := decode[struct {
		CredentialTypes []domain.CredentialType `json:"credentialTypes"`
		RedirectURI     string                  `json:"redirectUri"`
	}](t, w)

	if len(body.CredentialTypes) == 0 {
		t.Fatal("the editor renders its credential forms from these; an empty list is a broken screen")
	}
	// The redirect URI travels with the forms so the user can be told exactly
	// what to register with the provider, on the screen where they need it.
	if body.RedirectURI != "http://localhost:3000/oauth/callback" {
		t.Fatalf("redirectUri %q is not derived from PublicBaseURL", body.RedirectURI)
	}
	for _, ct := range body.CredentialTypes {
		if ct.Type == "" || ct.Name == "" || ct.Auth == "" {
			t.Fatalf("credential type is missing a field the UI needs: %+v", ct)
		}
	}
}

func TestOAuthRedirectURIEndpointMatchesTheOneOnCredentialTypes(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodGet, "/api/v1/oauth/redirect-uri", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]string](t, w)["redirectUri"]; got != "http://localhost:3000/oauth/callback" {
		t.Fatalf("redirectUri %q", got)
	}
}

func TestCredentialRoutesRequireASession(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/api/v1/credential-types", "/api/v1/credentials"} {
		if w := h.do(t, http.MethodGet, path, nil, false); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", path, w.Code)
		}
	}
}

// --- the secret never comes back --------------------------------------------

func TestCreatedCredentialResponseNeverContainsTheSecret(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodPost, "/api/v1/credentials", map[string]any{
		"type": "anthropicApi", "name": "Acme key",
		"fields": map[string]string{"apiKey": theSecret},
	}, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	// The whole body, not one field: this is the assertion that catches a
	// handler that starts serialising CredentialData by accident.
	if strings.Contains(w.Body.String(), theSecret) {
		t.Fatalf("the create response leaked the secret: %s", w.Body.String())
	}

	summary := decode[credentialBody](t, w).Credential
	// What may be said is *that* a field is set, which is what lets the edit
	// form show "unchanged" instead of an empty box that looks like data loss.
	if len(summary.SetFields) == 0 || summary.SetFields[0] != "apiKey" {
		t.Fatalf("setFields %v should name the fields that hold a value", summary.SetFields)
	}
}

func TestNoCredentialResponseAnywhereContainsTheSecret(t *testing.T) {
	h := newHarness(t)
	cred := h.createCredential(t, "anthropicApi", "Acme key", map[string]string{"apiKey": theSecret})

	responses := map[string]*httptest.ResponseRecorder{
		"list":  h.do(t, http.MethodGet, "/api/v1/credentials", nil, true),
		"get":   h.do(t, http.MethodGet, "/api/v1/credentials/"+cred.ID, nil, true),
		"patch": h.do(t, http.MethodPatch, "/api/v1/credentials/"+cred.ID, map[string]any{"name": "Renamed"}, true),
		"test":  h.do(t, http.MethodPost, "/api/v1/credentials/"+cred.ID+"/test", nil, true),
	}
	for what, w := range responses {
		if strings.Contains(w.Body.String(), theSecret) {
			t.Fatalf("the %s response leaked the secret: %s", what, w.Body.String())
		}
	}
}

// --- CRUD -------------------------------------------------------------------

func TestCreateListGetAndDeleteACredential(t *testing.T) {
	h := newHarness(t)
	cred := h.createCredential(t, "httpBasicAuth", "Acme staging",
		map[string]string{"username": "ha", "password": theSecret})

	list := decode[struct {
		Credentials []domain.CredentialSummary `json:"credentials"`
	}](t, h.do(t, http.MethodGet, "/api/v1/credentials", nil, true))
	if len(list.Credentials) != 1 || list.Credentials[0].ID != cred.ID {
		t.Fatalf("unexpected list: %+v", list.Credentials)
	}

	// The picker in the node drawer asks for one type at a time.
	filtered := decode[struct {
		Credentials []domain.CredentialSummary `json:"credentials"`
	}](t, h.do(t, http.MethodGet, "/api/v1/credentials?type=slackOAuth2", nil, true))
	if len(filtered.Credentials) != 0 {
		t.Fatalf("the type filter should have excluded everything, got %+v", filtered.Credentials)
	}

	got := decode[credentialBody](t, h.do(t, http.MethodGet, "/api/v1/credentials/"+cred.ID, nil, true))
	if got.Credential.Name != "Acme staging" || got.Credential.Type != "httpBasicAuth" {
		t.Fatalf("unexpected credential: %+v", got.Credential)
	}

	if w := h.do(t, http.MethodDelete, "/api/v1/credentials/"+cred.ID, nil, true); w.Code != http.StatusNoContent {
		t.Fatalf("delete status %d, body %s", w.Code, w.Body.String())
	}
	if w := h.do(t, http.MethodGet, "/api/v1/credentials/"+cred.ID, nil, true); w.Code != http.StatusNotFound {
		t.Fatalf("status %d after delete, want 404", w.Code)
	}
}

func TestCreateRejectsAnUnknownTypeAndAnEmptyName(t *testing.T) {
	h := newHarness(t)

	unknown := h.do(t, http.MethodPost, "/api/v1/credentials",
		map[string]any{"type": "notAThing", "name": "X", "fields": map[string]string{}}, true)
	if unknown.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body %s", unknown.Code, unknown.Body.String())
	}
	if !strings.Contains(unknown.Body.String(), "notAThing") {
		t.Fatalf("the message should name the type: %s", unknown.Body.String())
	}

	nameless := h.do(t, http.MethodPost, "/api/v1/credentials",
		map[string]any{"type": "bearerAuth", "name": "  ", "fields": map[string]string{"token": "t"}}, true)
	if nameless.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", nameless.Code)
	}
}

func TestCreateRejectsAMissingRequiredField(t *testing.T) {
	h := newHarness(t)

	w := h.do(t, http.MethodPost, "/api/v1/credentials",
		map[string]any{"type": "httpBasicAuth", "name": "Half", "fields": map[string]string{"username": "ha"}}, true)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Password") {
		t.Fatalf("the message should name the missing field: %s", w.Body.String())
	}
}

// TestPatchWithoutTheSecretKeepsIt is the whole reason the edit form is usable:
// a user renaming a credential must not have to retype a password they cannot
// see.
func TestPatchWithoutTheSecretKeepsIt(t *testing.T) {
	h := newHarness(t)
	cred := h.createCredential(t, "httpBasicAuth", "Acme staging",
		map[string]string{"username": "ha", "password": theSecret})

	// A rename with no fields at all.
	w := h.do(t, http.MethodPatch, "/api/v1/credentials/"+cred.ID, map[string]any{"name": "Acme production"}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	if decode[credentialBody](t, w).Credential.Name != "Acme production" {
		t.Fatalf("the rename did not take: %s", w.Body.String())
	}

	// And a field edit that sends the secret back as the empty string, which is
	// what an editor showing "unchanged" has to send.
	w = h.do(t, http.MethodPatch, "/api/v1/credentials/"+cred.ID, map[string]any{
		"fields": map[string]string{"username": "duc", "password": ""},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	resolved, err := h.creds.Resolve(t.Context(), testWorkspace, cred.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Data.Field("username") != "duc" {
		t.Fatalf("the username edit was lost: %q", resolved.Data.Field("username"))
	}
	if resolved.Data.Field("password") != theSecret {
		t.Fatal("an omitted secret must keep its stored value, not be blanked")
	}
}

// --- workspace scoping ------------------------------------------------------

func TestCredentialsAreScopedToTheWorkspace(t *testing.T) {
	h := newHarness(t)
	mine := h.createCredential(t, "bearerAuth", "Mine", map[string]string{"token": theSecret})

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/credentials/" + mine.ID, nil},
		{http.MethodPatch, "/api/v1/credentials/" + mine.ID, map[string]any{"name": "Stolen"}},
		{http.MethodDelete, "/api/v1/credentials/" + mine.ID, nil},
		{http.MethodPost, "/api/v1/credentials/" + mine.ID + "/oauth/start", nil},
	} {
		w := h.doAs(t, "ws-other", tc.method, tc.path, tc.body)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s: status %d, want 404 — another workspace's credential must be invisible",
				tc.method, tc.path, w.Code)
		}
		if strings.Contains(w.Body.String(), theSecret) {
			t.Fatalf("%s %s leaked the secret across workspaces", tc.method, tc.path)
		}
	}

	// And it must not even be listed.
	other := decode[struct {
		Credentials []domain.CredentialSummary `json:"credentials"`
	}](t, h.doAs(t, "ws-other", http.MethodGet, "/api/v1/credentials", nil))
	if len(other.Credentials) != 0 {
		t.Fatalf("another workspace saw %d credentials", len(other.Credentials))
	}
}

// --- test connection --------------------------------------------------------

func TestTestConnectionReportsTheProvidersStatus(t *testing.T) {
	h := newHarness(t)

	var sawAuth string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if sawAuth != "Bearer "+theSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()

	h.credTypes.Register(domain.CredentialType{
		Type: "stubBearer", Name: "Stub", Auth: domain.AuthBearer, TestURL: provider.URL,
		Fields: []domain.CredentialField{{Name: "token", Label: "Token", Type: "password", Required: true, Secret: true}},
	})
	good := h.createCredential(t, "stubBearer", "Good", map[string]string{"token": theSecret})
	bad := h.createCredential(t, "stubBearer", "Bad", map[string]string{"token": "wrong"})

	w := h.do(t, http.MethodPost, "/api/v1/credentials/"+good.ID+"/test", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	ok := decode[testResult](t, w)
	if !ok.OK || ok.Status != http.StatusOK {
		t.Fatalf("a working credential should test green: %+v", ok)
	}
	if sawAuth != "Bearer "+theSecret {
		t.Fatalf("the test request was not signed: %q", sawAuth)
	}

	failed := decode[testResult](t, h.do(t, http.MethodPost, "/api/v1/credentials/"+bad.ID+"/test", nil, true))
	if failed.OK || failed.Status != http.StatusUnauthorized {
		t.Fatalf("a rejected credential should report the status: %+v", failed)
	}
}

func TestTestConnectionSaysSoWhenATypeCannotBeTested(t *testing.T) {
	h := newHarness(t)
	// bearerAuth ships with no TestURL: there is no endpoint to prove anything
	// against, and a green tick would be a lie.
	cred := h.createCredential(t, "bearerAuth", "Untestable", map[string]string{"token": theSecret})

	w := h.do(t, http.MethodPost, "/api/v1/credentials/"+cred.ID+"/test", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	got := decode[testResult](t, w)
	if got.OK {
		t.Fatal("a type with no test endpoint must not report success")
	}
	if !strings.Contains(got.Message, "cannot be tested") {
		t.Fatalf("the message should explain why: %q", got.Message)
	}
}

func TestTestConnectionOnAMissingCredentialIsA404(t *testing.T) {
	h := newHarness(t)
	w := h.do(t, http.MethodPost, "/api/v1/credentials/cred-nope/test", nil, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", w.Code)
	}
}
