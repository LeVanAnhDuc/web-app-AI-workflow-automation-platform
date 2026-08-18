package crypto

import (
	"bytes"
	"testing"
)

func key(b byte) []byte {
	k := make([]byte, KeySize)
	for i := range k {
		k[i] = b
	}
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	s, err := NewSealer(key(1))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	plain := []byte(`{"token":"secret-value"}`)
	blob, err := s.Seal(plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(blob, []byte("secret-value")) {
		t.Fatal("plaintext leaked into the sealed blob")
	}

	got, err := s.Open(blob)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q, want %q", got, plain)
	}
}

func TestSealIsNonDeterministic(t *testing.T) {
	s, _ := NewSealer(key(2))
	a, _ := s.Seal([]byte("same"))
	b, _ := s.Seal([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext must differ, or the nonce is not random")
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	writer, _ := NewSealer(key(3))
	reader, _ := NewSealer(key(4))

	blob, _ := writer.Seal([]byte("secret"))
	if _, err := reader.Open(blob); err != ErrCorrupt {
		t.Fatalf("expected ErrCorrupt with a foreign key, got %v", err)
	}
}

func TestOpenRejectsTamperedBlob(t *testing.T) {
	s, _ := NewSealer(key(5))
	blob, _ := s.Seal([]byte("secret"))
	blob[len(blob)-1] ^= 0xff

	if _, err := s.Open(blob); err != ErrCorrupt {
		t.Fatalf("expected ErrCorrupt for a tampered blob, got %v", err)
	}
}

func TestOpenRejectsTruncatedBlob(t *testing.T) {
	s, _ := NewSealer(key(6))
	if _, err := s.Open([]byte{1, 2, 3}); err != ErrCorrupt {
		t.Fatalf("expected ErrCorrupt for a short blob, got %v", err)
	}
}

func TestNewSealerRejectsShortKey(t *testing.T) {
	if _, err := NewSealer([]byte("too-short")); err != ErrBadKey {
		t.Fatalf("expected ErrBadKey, got %v", err)
	}
}

func TestSealJSONRoundTrip(t *testing.T) {
	s, _ := NewSealer(key(7))

	type basicAuth struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	in := basicAuth{User: "ha", Password: "p4ssw0rd"}

	blob, err := s.SealJSON(in)
	if err != nil {
		t.Fatalf("SealJSON: %v", err)
	}

	var out basicAuth
	if err := s.OpenJSON(blob, &out); err != nil {
		t.Fatalf("OpenJSON: %v", err)
	}
	if out != in {
		t.Fatalf("got %+v, want %+v", out, in)
	}
}

func TestOpenJSONSurfacesCorruption(t *testing.T) {
	s, _ := NewSealer(key(8))
	var out map[string]string
	if err := s.OpenJSON([]byte("garbage-that-is-long-enough-to-pass-the-length-check"), &out); err != ErrCorrupt {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
}
