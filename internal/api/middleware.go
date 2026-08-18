package api

import (
	"context"
	"net/http"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/auth"
)

type ctxKey int

const claimsKey ctxKey = iota

// requireAuth rejects a request without a valid session and puts the claims on
// the context for the handlers behind it.
func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.TokenFromRequest(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "unauthenticated", "Sign in to continue.")
			return
		}
		claims, err := s.signer.Verify(token)
		if err != nil {
			// The cookie is present but unusable; clear it so the browser stops
			// sending a token that will never work again.
			auth.ClearCookie(w, r)
			writeErr(w, http.StatusUnauthorized, "unauthenticated", "Your session has expired. Sign in again.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
	})
}

// claimsFrom reads the session claims a requireAuth-protected handler can rely
// on being there.
func claimsFrom(ctx context.Context) auth.Claims {
	c, _ := ctx.Value(claimsKey).(auth.Claims)
	return c
}

// workspaceOf is the tenant scope of the current request. Every store call
// takes it, which is what keeps Phase 4 a UI change rather than an audit.
func workspaceOf(r *http.Request) string {
	return claimsFrom(r.Context()).WorkspaceID
}
