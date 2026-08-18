package llm

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider is a Provider that answers from a fixed script. The llm package
// has no node dependencies, so the double lives here rather than in a helper
// package.
type stubProvider struct {
	name   string
	models []string
	resp   Response
	err    error

	// got records the last request, which is what the request-shaping tests read.
	got Request
}

func (s *stubProvider) Name() string     { return s.name }
func (s *stubProvider) Models() []string { return s.models }

func (s *stubProvider) Chat(_ context.Context, req Request) (Response, error) {
	s.got = req
	return s.resp, s.err
}

func TestRegistryDefault(t *testing.T) {
	t.Run("empty registry names the environment variable to set", func(t *testing.T) {
		p, err := NewRegistry().Default()
		assert.Nil(t, p)

		var noProvider *ErrNoProvider
		require.ErrorAs(t, err, &noProvider)
		// The message is surfaced verbatim in a node's validation error, so it
		// has to tell the operator what to do rather than just what is wrong.
		assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
	})

	t.Run("first registered provider is the default", func(t *testing.T) {
		first := &stubProvider{name: "first"}
		second := &stubProvider{name: "second"}

		p, err := NewRegistry(first, second).Default()
		require.NoError(t, err)
		assert.Same(t, first, p)
	})

	t.Run("nil providers are ignored rather than becoming the default", func(t *testing.T) {
		real := &stubProvider{name: "real"}
		r := NewRegistry(nil, real)

		p, err := r.Default()
		require.NoError(t, err)
		assert.Same(t, real, p)
		assert.Equal(t, []string{"real"}, r.Names())
	})
}

func TestRegistryGet(t *testing.T) {
	only := &stubProvider{name: "anthropic"}
	r := NewRegistry(only)

	t.Run("empty name falls back to the default", func(t *testing.T) {
		p, err := r.Get("")
		require.NoError(t, err)
		assert.Same(t, only, p)
	})

	t.Run("known name", func(t *testing.T) {
		p, err := r.Get("anthropic")
		require.NoError(t, err)
		assert.Same(t, only, p)
	})

	t.Run("unknown name names what was asked for", func(t *testing.T) {
		_, err := r.Get("nope")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"nope"`)
	})
}

func TestRegistryNamesAndEmpty(t *testing.T) {
	t.Run("names keep registration order", func(t *testing.T) {
		r := NewRegistry(
			&stubProvider{name: "one"},
			&stubProvider{name: "two"},
			&stubProvider{name: "three"},
		)
		assert.Equal(t, []string{"one", "two", "three"}, r.Names())
		assert.False(t, r.Empty())
	})

	t.Run("re-registering a name does not duplicate it", func(t *testing.T) {
		r := NewRegistry(&stubProvider{name: "one"}, &stubProvider{name: "one"})
		assert.Equal(t, []string{"one"}, r.Names())
	})

	t.Run("names is a copy the caller cannot use to reorder the registry", func(t *testing.T) {
		r := NewRegistry(&stubProvider{name: "one"}, &stubProvider{name: "two"})
		got := r.Names()
		got[0] = "clobbered"
		assert.Equal(t, []string{"one", "two"}, r.Names())
	})

	t.Run("an empty registry is Empty", func(t *testing.T) {
		assert.True(t, NewRegistry().Empty())
	})
}

// A nil registry is the normal state of a deployment with no API key: Options.LLM
// is a plain pointer and nothing fills it in, so every method must cope.
func TestNilRegistryDoesNotPanic(t *testing.T) {
	var r *Registry

	assert.True(t, r.Empty())
	assert.Nil(t, r.Names())

	_, err := r.Default()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")

	_, err = r.Get("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")

	_, err = r.Get("anthropic")
	require.Error(t, err)
	var noProvider *ErrNoProvider
	assert.ErrorAs(t, err, &noProvider)
}

func TestErrNoProviderMessage(t *testing.T) {
	assert.Equal(t, "no language-model provider is configured", (&ErrNoProvider{}).Error())
	assert.Equal(t,
		"no language-model provider is configured: key missing",
		(&ErrNoProvider{Reason: "key missing"}).Error())
}

func TestEffortValid(t *testing.T) {
	cases := []struct {
		effort Effort
		want   bool
	}{
		{EffortLow, true},
		{EffortMedium, true},
		{EffortHigh, true},
		{EffortXHigh, true},
		{EffortMax, true},
		{"", false},
		{"HIGH", false}, // the provider's values are lower-case; no normalising here
		{"turbo", false},
	}
	for _, c := range cases {
		t.Run(string(c.effort), func(t *testing.T) {
			assert.Equal(t, c.want, c.effort.Valid())
		})
	}
}

func TestToolSpecSchemaOrPermissive(t *testing.T) {
	t.Run("the author's schema wins", func(t *testing.T) {
		schema := map[string]any{
			"type":       "object",
			"properties": map[string]any{"email": map[string]any{"type": "string"}},
			"required":   []any{"email"},
		}
		spec := ToolSpec{Name: "lookup", Schema: schema}
		assert.Equal(t, schema, spec.SchemaOrPermissive())
	})

	t.Run("no schema means any object", func(t *testing.T) {
		got := ToolSpec{Name: "lookup"}.SchemaOrPermissive()
		assert.Equal(t, "object", got["type"])
		assert.Equal(t, map[string]any{}, got["properties"])
		// additionalProperties must be true, or a tool with no schema would
		// reject every argument the model sent.
		assert.Equal(t, true, got["additionalProperties"])
	})

	t.Run("an empty but non-nil schema is still permissive", func(t *testing.T) {
		got := ToolSpec{Name: "lookup", Schema: map[string]any{}}.SchemaOrPermissive()
		assert.Equal(t, true, got["additionalProperties"])
	})
}

func TestUsageAdd(t *testing.T) {
	total := Usage{InputTokens: 10, OutputTokens: 5}
	total.Add(Usage{InputTokens: 3, OutputTokens: 7, CacheReadTokens: 100, CacheWriteTokens: 2})
	total.Add(Usage{InputTokens: 1})

	assert.Equal(t, Usage{
		InputTokens:      14,
		OutputTokens:     12,
		CacheReadTokens:  100,
		CacheWriteTokens: 2,
	}, total)
}

func TestResponseAsAssistantMessage(t *testing.T) {
	calls := []ToolCall{{ID: "call-1", Name: "lookup", Input: map[string]any{"email": "a@b.c"}}}
	resp := Response{Text: "let me check", ToolCalls: calls, StopReason: StopToolUse}

	msg := resp.AsAssistantMessage()

	assert.Equal(t, RoleAssistant, msg.Role)
	assert.Equal(t, "let me check", msg.Text)
	assert.Equal(t, calls, msg.ToolCalls)
	// Results belong to a user turn; carrying them here would build an
	// impossible history.
	assert.Empty(t, msg.Results)
}

func TestUserText(t *testing.T) {
	msg := UserText("hello")
	assert.Equal(t, RoleUser, msg.Role)
	assert.Equal(t, "hello", msg.Text)
}

func TestFromAPIKey(t *testing.T) {
	// A discard logger keeps the warning out of the test output while still
	// exercising the nil-logger-free path.
	log := slog.New(slog.DiscardHandler)

	t.Run("an empty key disables the AI nodes without failing at boot", func(t *testing.T) {
		r := FromAPIKey("   ", log)
		require.NotNil(t, r)
		assert.True(t, r.Empty())

		_, err := r.Default()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
	})

	t.Run("a key produces a usable anthropic provider", func(t *testing.T) {
		r := FromAPIKey("sk-test-not-a-real-key", log)
		require.NotNil(t, r)
		assert.False(t, r.Empty())
		assert.Equal(t, []string{"anthropic"}, r.Names())

		p, err := r.Default()
		require.NoError(t, err)
		assert.Equal(t, []string{ModelOpus5, ModelSonnet5, ModelHaiku45}, p.Models())
	})

	t.Run("a nil logger is accepted", func(t *testing.T) {
		assert.True(t, FromAPIKey("", nil).Empty())
	})
}

func TestNewAnthropicRejectsAnEmptyKey(t *testing.T) {
	_, err := NewAnthropic("  ")
	var noProvider *ErrNoProvider
	require.ErrorAs(t, err, &noProvider)
	assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
}
