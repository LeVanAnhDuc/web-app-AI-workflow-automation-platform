package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	s, err := NewSigner([]byte("test-secret"))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	token, expires, err := s.Issue("user-1", "ws-1", "ha@acme.vn", "owner", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !expires.After(time.Now()) {
		t.Fatalf("expiry %v is not in the future", expires)
	}

	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.UserID != "user-1" || claims.WorkspaceID != "ws-1" || claims.Email != "ha@acme.vn" {
		t.Fatalf("claims round-tripped wrong: %+v", claims)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	issuer, _ := NewSigner([]byte("test-secret"))
	issuer = issuer.WithClock(func() time.Time { return base })

	token, _, err := issuer.Issue("user-1", "ws-1", "ha@acme.vn", "owner", time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	later := issuer.WithClock(func() time.Time { return base.Add(2 * time.Minute) })
	if _, err := later.Verify(token); err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken for an expired token, got %v", err)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	a, _ := NewSigner([]byte("secret-a"))
	b, _ := NewSigner([]byte("secret-b"))

	token, _, _ := a.Issue("user-1", "ws-1", "ha@acme.vn", "owner", time.Hour)
	if _, err := b.Verify(token); err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken for a foreign secret, got %v", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	s, _ := NewSigner([]byte("test-secret"))
	for _, token := range []string{"", "not.a.token", "a.b.c"} {
		if _, err := s.Verify(token); err != ErrInvalidToken {
			t.Fatalf("token %q: expected ErrInvalidToken, got %v", token, err)
		}
	}
}

func TestNewSignerRejectsEmptySecret(t *testing.T) {
	if _, err := NewSigner(nil); err == nil {
		t.Fatal("expected an error for an empty secret")
	}
}

func TestTokenFromRequestPrefersCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "from-cookie"})
	r.Header.Set("Authorization", "Bearer from-header")

	if got := TokenFromRequest(r); got != "from-cookie" {
		t.Fatalf("got %q, want the cookie value", got)
	}
}

func TestTokenFromRequestFallsBackToBearer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	r.Header.Set("Authorization", "bearer  from-header ")

	if got := TokenFromRequest(r); got != "from-header" {
		t.Fatalf("got %q, want the trimmed header value", got)
	}
}

func TestTokenFromRequestEmptyWhenAbsent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	if got := TokenFromRequest(r); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestSetCookieMarksSecureOnlyForTLS(t *testing.T) {
	s, _ := NewSigner([]byte("test-secret"))

	plain := httptest.NewRecorder()
	s.SetCookie(plain, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil), "t", time.Now().Add(time.Hour))
	if got := plain.Result().Cookies()[0]; got.Secure {
		t.Fatal("cookie must not be Secure over plain http, or local development breaks")
	}

	forwarded := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	s.SetCookie(forwarded, r, "t", time.Now().Add(time.Hour))
	if got := forwarded.Result().Cookies()[0]; !got.Secure {
		t.Fatal("cookie must be Secure behind an https proxy")
	}
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("flowgrid123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "flowgrid123" {
		t.Fatal("hash must not be the plaintext")
	}
	if !CheckPassword(hash, "flowgrid123") {
		t.Fatal("CheckPassword rejected the correct password")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("CheckPassword accepted a wrong password")
	}
	if _, err := HashPassword(""); err == nil {
		t.Fatal("expected an error for an empty password")
	}
}
