package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

/* ---------------------------------------------------------------------------
   The authorisation-code dance, from the browser's side.

   Two endpoints, and they must agree about one string. The redirect URI is
   computed once in config.Config.OAuthRedirectURI and read from there by both
   the start (which sends it to the authorisation endpoint) and the callback
   (which sends it again with the token exchange). A provider compares the two
   and rejects the exchange if they differ by a character, which is the single
   most common way a flow that looks right fails.

   The callback is a browser redirect rather than an API call: it arrives from
   the provider, so it answers with a Location and no body at all. Rendering the
   provider's error message as HTML would turn a query parameter an attacker can
   set into markup on our own origin.
   --------------------------------------------------------------------------- */

// handleOAuthStart begins an authorisation for one credential and hands the
// browser the provider's consent URL.
func (s *server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if !s.credsReady(w) {
		return
	}

	var req struct {
		ReturnTo string `json:"returnTo"`
	}
	// A start with no body is normal: the UI only sends one when it wants the
	// user brought back to a particular screen.
	if r.ContentLength > 0 && !decodeJSON(w, r, &req) {
		return
	}

	start, err := s.creds.StartOAuth(r.Context(), workspaceOf(r), chi.URLParam(r, "id"),
		s.cfg.OAuthRedirectURI(), s.safeReturnTo(req.ReturnTo))
	if err != nil {
		writeCredErr(w, err, "That credential does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": start.URL, "state": start.State})
}

// handleOAuthCallback completes the dance. It is unauthenticated by necessity —
// the provider redirects the browser here and carries no session — and is
// protected instead by the one-use state this process minted at start time.
func (s *server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if !s.credsReady(w) {
		return
	}

	// The provider refused, or the user pressed cancel. The reason is theirs to
	// state and ours to pass along as text, never as markup.
	if providerErr := q.Get("error"); providerErr != "" {
		message := providerErr
		if desc := q.Get("error_description"); desc != "" {
			message += ": " + desc
		}
		s.redirectOAuth(w, "", false, "", message)
		return
	}

	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		s.redirectOAuth(w, "", false, "",
			"The provider did not send an authorisation code. Start the connection again.")
		return
	}

	result, err := s.creds.CompleteOAuth(r.Context(), state, code, s.cfg.OAuthRedirectURI())
	if err != nil {
		// A bad, replayed or expired state lands here with a message that says
		// to start again, and deliberately does not distinguish the three.
		s.redirectOAuth(w, "", false, "", err.Error())
		return
	}
	s.redirectOAuth(w, result.ReturnTo, true, result.Credential.ID, "")
}

// redirectOAuth sends the browser back where the start call asked, with a flag
// the UI can turn into a banner. It writes no body: the only thing this
// response carries is a Location this process built.
func (s *server) redirectOAuth(w http.ResponseWriter, returnTo string, ok bool, credentialID, message string) {
	target, err := url.Parse(s.safeReturnTo(returnTo))
	if err != nil {
		target, _ = url.Parse(s.defaultReturnTo())
	}

	q := target.Query()
	if ok {
		q.Set("oauth", "connected")
		if credentialID != "" {
			q.Set("credentialId", credentialID)
		}
	} else {
		q.Set("oauth", "error")
		q.Set("message", message)
	}
	target.RawQuery = q.Encode()

	w.Header().Set("Location", target.String())
	w.Header().Set("Cache-Control", "no-store")
	// 303 rather than 302: what follows is a plain GET of a page, whatever the
	// provider used to get here.
	w.WriteHeader(http.StatusSeeOther)
}

// defaultReturnTo is where a connection lands when nobody said otherwise.
func (s *server) defaultReturnTo() string {
	return s.cfg.PublicBaseURL + "/credentials"
}

// safeReturnTo confines a return URL to this deployment.
//
// The value travels from a request body, through the state store, into a
// Location header, which is precisely the shape of an open redirect. Only a
// path on our own origin survives: anything absolute must match PublicBaseURL's
// scheme and host, and anything else is replaced by the credentials screen
// rather than refused, because a bad returnTo must not cost the user the
// authorisation they just granted.
func (s *server) safeReturnTo(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return s.defaultReturnTo()
	}

	base, err := url.Parse(s.cfg.PublicBaseURL)
	if err != nil {
		return s.defaultReturnTo()
	}
	ref, err := url.Parse(candidate)
	if err != nil {
		return s.defaultReturnTo()
	}

	// "//evil.example" parses as a scheme-relative URL, which a browser follows
	// off-origin. Treating it as a path would be the bug.
	if ref.Scheme == "" && ref.Host == "" {
		if !strings.HasPrefix(ref.Path, "/") {
			return s.defaultReturnTo()
		}
		return base.ResolveReference(ref).String()
	}
	if ref.Scheme == base.Scheme && ref.Host == base.Host {
		return ref.String()
	}
	return s.defaultReturnTo()
}
