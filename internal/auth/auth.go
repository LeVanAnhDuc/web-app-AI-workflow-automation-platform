// Package auth issues and verifies the session token, and hashes passwords.
// Phase 1 authenticates with email and password only; the token shape already
// carries the workspace so nothing changes when Phase 4 adds more members.
package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// CookieName is the browser session cookie. The frontend never reads it; Next
// proxies /api to the Go server so the cookie stays same-origin and HttpOnly.
const CookieName = "fg_session"

// SessionTTL is how long a session stays valid. The login screen's "keep me
// signed in" checkbox selects between this and ShortSessionTTL.
const (
	SessionTTL      = 30 * 24 * time.Hour
	ShortSessionTTL = 12 * time.Hour
)

// ErrInvalidToken covers every reason a token was not accepted; callers must
// not distinguish them, so that a probe learns nothing.
var ErrInvalidToken = errors.New("invalid session token")

// Claims is the payload of a session token.
type Claims struct {
	UserID      string `json:"sub"`
	WorkspaceID string `json:"wid"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	jwt.RegisteredClaims
}

// Signer mints and verifies session tokens with one secret.
type Signer struct {
	secret []byte
	now    func() time.Time
}

// NewSigner builds a Signer. A short secret is a configuration error the caller
// should have caught, so this only guards against the empty case.
func NewSigner(secret []byte) (*Signer, error) {
	if len(secret) == 0 {
		return nil, errors.New("auth: session secret must not be empty")
	}
	return &Signer{secret: secret, now: time.Now}, nil
}

// WithClock replaces the clock, for tests.
func (s *Signer) WithClock(now func() time.Time) *Signer {
	return &Signer{secret: s.secret, now: now}
}

// Issue mints a token for one user.
func (s *Signer) Issue(userID, workspaceID, email, role string, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 {
		ttl = SessionTTL
	}
	now := s.now()
	expires := now.Add(ttl)

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID:      userID,
		WorkspaceID: workspaceID,
		Email:       email,
		Role:        role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
			Issuer:    "flowgrid",
		},
	})

	signed, err := tok.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, expires, nil
}

// Verify parses and validates a token, returning its claims.
func (s *Signer) Verify(token string) (Claims, error) {
	var claims Claims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method %q", t.Method.Alg())
		}
		return s.secret, nil
	}, jwt.WithTimeFunc(s.now), jwt.WithIssuer("flowgrid"))

	if err != nil || !parsed.Valid {
		return Claims{}, ErrInvalidToken
	}
	if claims.UserID == "" || claims.WorkspaceID == "" {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

// SetCookie writes the session cookie. Secure is set only for https so local
// development over http still works.
func (s *Signer) SetCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie expires the session cookie.
func ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func isTLS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// TokenFromRequest reads the token from the session cookie, falling back to a
// bearer header so scripts can use the same API as the browser.
func TokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value
	}
	h := r.Header.Get("Authorization")
	if after, ok := cutPrefixFold(h, "bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}

// HashPassword returns a bcrypt hash at the default cost.
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("auth: password must not be empty")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(h), nil
}

// CheckPassword compares a plaintext password against a stored hash.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
