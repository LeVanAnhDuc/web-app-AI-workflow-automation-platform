package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/credentials"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/crypto"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

/* ---------------------------------------------------------------------------
   The credential vault's HTTP surface.

   One rule governs every handler here: a stored secret never appears in a
   response. The service returns domain.CredentialSummary — which names the
   fields that hold a value but never their values — and nothing in this file
   ever serialises a domain.CredentialData. The "Test connection" endpoint is
   the closest call: it reports the provider's status code and nothing of the
   provider's body, because a body can echo back what was sent to it.
   --------------------------------------------------------------------------- */

// handleCredentialTypes serves the descriptors the editor renders its credential
// forms from, alongside the redirect URI the user must register with a provider.
// They travel together on purpose: the form and the URL it depends on should
// never be one screen apart.
func (s *server) handleCredentialTypes(w http.ResponseWriter, r *http.Request) {
	if !s.credsReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"credentialTypes": s.creds.Types().All(),
		"redirectUri":     s.cfg.OAuthRedirectURI(),
	})
}

// handleOAuthRedirectURI serves the callback URL on its own, so a screen that
// only needs to tell the user what to paste into a provider console can ask for
// exactly that.
func (s *server) handleOAuthRedirectURI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"redirectUri": s.cfg.OAuthRedirectURI()})
}

func (s *server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	if !s.credsReady(w) {
		return
	}
	list, err := s.creds.List(r.Context(), workspaceOf(r), strings.TrimSpace(r.URL.Query().Get("type")))
	if err != nil {
		writeCredErr(w, err, "No credentials found.")
		return
	}
	if list == nil {
		list = []domain.CredentialSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": list})
}

func (s *server) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	if !s.credsReady(w) {
		return
	}

	var req struct {
		Type   string            `json:"type"`
		Name   string            `json:"name"`
		Fields map[string]string `json:"fields"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusUnprocessableEntity, domain.ErrCodeValidation, "A credential needs a name.")
		return
	}

	summary, err := s.creds.Create(r.Context(), domain.NewCredential{
		WorkspaceID: workspaceOf(r),
		Type:        strings.TrimSpace(req.Type),
		Name:        name,
		Data:        domain.CredentialData{Fields: req.Fields},
	})
	if err != nil {
		writeCredErr(w, err, "Could not create the credential.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"credential": summary})
}

func (s *server) handleGetCredential(w http.ResponseWriter, r *http.Request) {
	if !s.credsReady(w) {
		return
	}
	summary, err := s.creds.Get(r.Context(), workspaceOf(r), chi.URLParam(r, "id"))
	if err != nil {
		writeCredErr(w, err, "That credential does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential": summary})
}

// handlePatchCredential updates a name, a set of fields, or both. Omitting
// "fields" leaves every stored value alone, and sending a secret field as an
// empty string means "unchanged" rather than "erase it" — the editor cannot
// show a stored secret, so it cannot round-trip one either.
func (s *server) handlePatchCredential(w http.ResponseWriter, r *http.Request) {
	if !s.credsReady(w) {
		return
	}

	var req struct {
		Name   *string           `json:"name"`
		Fields map[string]string `json:"fields"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeErr(w, http.StatusUnprocessableEntity, domain.ErrCodeValidation, "A credential needs a name.")
		return
	}

	summary, err := s.creds.Update(r.Context(), workspaceOf(r), chi.URLParam(r, "id"),
		domain.CredentialPatch{Name: req.Name, Fields: req.Fields})
	if err != nil {
		writeCredErr(w, err, "That credential does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential": summary})
}

func (s *server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if !s.credsReady(w) {
		return
	}
	if err := s.creds.Delete(r.Context(), workspaceOf(r), chi.URLParam(r, "id")); err != nil {
		writeCredErr(w, err, "That credential does not exist.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testResult is the answer to "Test connection". It carries a status code and a
// sentence, never the provider's body: a body may quote the request that was
// sent to it, and that request was signed with the secret.
type testResult struct {
	OK      bool   `json:"ok"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

func (s *server) handleTestCredential(w http.ResponseWriter, r *http.Request) {
	if !s.credsReady(w) {
		return
	}
	id := chi.URLParam(r, "id")

	resolved, err := s.creds.Resolve(r.Context(), workspaceOf(r), id)
	if err != nil {
		// A credential that does not exist is a routing problem; anything else
		// is something the user can fix, and the button should say so in place
		// rather than turn red with an HTTP code.
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "That credential does not exist.")
			return
		}
		writeJSON(w, http.StatusOK, testResult{OK: false, Message: err.Error()})
		return
	}

	if resolved.Type.TestURL == "" {
		// Honest rather than green: there is no cheap authenticated endpoint to
		// call for this type, so nothing was proved.
		writeJSON(w, http.StatusOK, testResult{
			OK: false,
			Message: "Credentials of type " + resolved.Type.Name +
				" cannot be tested, because the provider offers no endpoint to check them against.",
		})
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, resolved.Type.TestURL, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, testResult{OK: false, Message: "The test URL for this type is not usable."})
		return
	}
	req.Header.Set("Accept", "application/json")
	if err := credentials.Apply(req, resolved); err != nil {
		writeJSON(w, http.StatusOK, testResult{OK: false, Message: err.Error()})
		return
	}

	res, err := s.testClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, testResult{OK: false,
			Message: "The provider could not be reached: " + err.Error()})
		return
	}
	defer res.Body.Close()

	if res.StatusCode >= 200 && res.StatusCode <= 299 {
		writeJSON(w, http.StatusOK, testResult{OK: true, Status: res.StatusCode,
			Message: "The provider accepted this credential."})
		return
	}
	writeJSON(w, http.StatusOK, testResult{OK: false, Status: res.StatusCode,
		Message: "The provider answered " + res.Status + "."})
}

// credsReady guards the routes that need a vault. A build wired without one
// should say so plainly instead of panicking on a nil pointer.
func (s *server) credsReady(w http.ResponseWriter) bool {
	if s.creds == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable",
			"Credentials are not configured on this deployment.")
		return false
	}
	return true
}

// writeCredErr maps the vault's errors onto status codes. Its cases are the
// four things that actually go wrong with a credential, each with a message a
// person can act on.
func writeCredErr(w http.ResponseWriter, err error, notFoundMessage string) {
	var (
		unknownType  *credentials.ErrUnknownType
		notConnected *credentials.ErrNotConnected
		nodeErr      *domain.NodeError
	)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", notFoundMessage)
	case errors.Is(err, crypto.ErrCorrupt):
		// A rotated or mistyped CREDENTIAL_KEY. Nothing the user typed is wrong,
		// so an opaque 500 would send them hunting in the wrong place.
		writeErr(w, http.StatusUnprocessableEntity, domain.ErrCodeValidation,
			"This credential cannot be decrypted, which usually means CREDENTIAL_KEY changed since it was saved. Re-enter it to store it under the current key.")
	case errors.As(err, &unknownType):
		writeErr(w, http.StatusUnprocessableEntity, domain.ErrCodeValidation,
			"There is no credential type "+unknownType.Type+".")
	case errors.As(err, &notConnected):
		writeErr(w, http.StatusUnprocessableEntity, domain.ErrCodeValidation, notConnected.Error())
	case errors.As(err, &nodeErr):
		writeErr(w, http.StatusUnprocessableEntity, nodeErr.Code, nodeErr.Message)
	default:
		writeStoreErr(w, err, notFoundMessage)
	}
}
