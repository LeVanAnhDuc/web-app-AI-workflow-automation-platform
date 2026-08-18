package queue

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBackoffDoublesThenCaps(t *testing.T) {
	cases := map[int]time.Duration{
		-1: time.Second, // a nonsense attempt still yields the first delay
		0:  time.Second,
		1:  time.Second,
		2:  2 * time.Second,
		3:  4 * time.Second,
		4:  8 * time.Second,
		8:  128 * time.Second,
		9:  256 * time.Second,
		10: maxBackoff, // 512s would exceed the five-minute ceiling
		30: maxBackoff, // far beyond a shift that would overflow
	}
	for attempt, want := range cases {
		assert.Equal(t, want, Backoff(attempt), "attempt %d", attempt)
	}
}

func TestTruncateKeepsValidUTF8(t *testing.T) {
	assert.Equal(t, "short", truncate("short", maxLastError))
	assert.Len(t, truncate(strings.Repeat("x", maxLastError+500), maxLastError), maxLastError)

	// Cutting mid-rune would store invalid UTF-8, which JSONB and logs reject.
	cut := truncate("aa"+strings.Repeat("é", 10), 5)
	assert.Equal(t, "aaé", cut, "the split must fall on a rune boundary, so 5 bytes yield 4")
}
