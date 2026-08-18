package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/auth"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

type userResponse struct {
	User publicUser `json:"user"`
}

type publicUser struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	CreatedAt   string `json:"createdAt"`
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "invalid_credentials", "Enter your email and password.")
		return
	}

	user, err := s.store.UserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// One message for both a missing user and a wrong password, so the
			// endpoint cannot be used to enumerate accounts.
			writeErr(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
			return
		}
		writeStoreErr(w, err, "Invalid email or password.")
		return
	}

	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
		return
	}

	ttl := auth.ShortSessionTTL
	if req.Remember {
		ttl = auth.SessionTTL
	}
	token, expires, err := s.signer.Issue(user.ID, user.WorkspaceID, user.Email, user.Role, ttl)
	if err != nil {
		s.log.Error("api: issue session", "error", err)
		writeErr(w, http.StatusInternalServerError, domain.ErrCodeInternal, "Could not start a session.")
		return
	}

	s.signer.SetCookie(w, r, token, expires)
	writeJSON(w, http.StatusOK, userResponse{User: toPublicUser(user)})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	auth.ClearCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r.Context())
	user, err := s.store.UserByID(r.Context(), claims.UserID)
	if err != nil {
		writeStoreErr(w, err, "That account no longer exists.")
		return
	}
	writeJSON(w, http.StatusOK, userResponse{User: toPublicUser(user)})
}

func toPublicUser(u domain.User) publicUser {
	return publicUser{
		ID:          u.ID,
		WorkspaceID: u.WorkspaceID,
		Email:       u.Email,
		Role:        u.Role,
		CreatedAt:   u.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
}
