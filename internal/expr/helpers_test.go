package expr

import (
	"reflect"
	"strings"
	"testing"
)

/* ---------------------------------------------------------------------------
   Tiny assertion helpers. The repository's go.sum carries no checksum for
   testify's yaml dependency, so these packages assert with the standard
   library only rather than dragging in a matcher library.
   --------------------------------------------------------------------------- */

func mustEqual(t *testing.T, got, want any, what string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: got %#v (%T), want %#v (%T)", what, got, got, want, want)
	}
}

func mustNoError(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", what, err)
	}
}

func mustError(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", what)
	}
}

func mustContain(t *testing.T, s, sub, what string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("%s: %q does not contain %q", what, s, sub)
	}
}
