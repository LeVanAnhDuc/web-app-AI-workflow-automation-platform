package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/* ---------------------------------------------------------------------------
   The Anthropic adapter is exercised against an httptest stub rather than the
   real API: the interesting behaviour is the translation in both directions, and
   that is only assertable if the test can see the JSON the SDK put on the wire.

   Retries are disabled on the test client so a 429 or 500 case asserts exactly
   one request and finishes immediately; production keeps the SDK default.
   --------------------------------------------------------------------------- */

// anthropicStub is a fake /v1/messages endpoint that records what it received.
type anthropicStub struct {
	server *httptest.Server

	status int
	body   string

	mu       sync.Mutex
	requests []map[string]any
	paths    []string
}

func newAnthropicStub(t *testing.T) *anthropicStub {
	t.Helper()
	s := &anthropicStub{status: http.StatusOK, body: textReply("ok")}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var decoded map[string]any
		if len(raw) > 0 {
			require.NoError(t, json.Unmarshal(raw, &decoded), "stub received invalid JSON: %s", raw)
		}

		s.mu.Lock()
		s.requests = append(s.requests, decoded)
		s.paths = append(s.paths, r.URL.Path)
		status, body := s.status, s.body
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.server.Close)
	return s
}

// provider builds an Anthropic pointed at the stub. NewAnthropic has no seam for
// a base URL, so a same-package test assembles the struct itself.
func (s *anthropicStub) provider() *Anthropic {
	return &Anthropic{client: anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(s.server.URL),
		option.WithMaxRetries(0),
	)}
}

func (s *anthropicStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// lastRequest is the JSON body of the most recent call.
func (s *anthropicStub) lastRequest(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.requests, "the stub was never called")
	return s.requests[len(s.requests)-1]
}

func (s *anthropicStub) firstPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.paths) == 0 {
		return ""
	}
	return s.paths[0]
}

func (s *anthropicStub) reply(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

// textReply is a plain end_turn message.
func textReply(text string) string {
	return `{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-opus-5",
		"content": [{"type": "text", "text": ` + quote(text) + `}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 11, "output_tokens": 4,
		          "cache_read_input_tokens": 7, "cache_creation_input_tokens": 3}
	}`
}

func quote(s string) string {
	out, _ := json.Marshal(s)
	return string(out)
}

// apiError is the error envelope the SDK parses a status code out of.
const apiError = `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`

// oneMessage is the minimum valid request, used wherever the messages are not
// what the test is about.
func oneMessage() []Message { return []Message{UserText("hi")} }

func TestAnthropicChatText(t *testing.T) {
	stub := newAnthropicStub(t)
	stub.reply(http.StatusOK, textReply("the answer is 42"))

	resp, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
	require.NoError(t, err)

	assert.Equal(t, "the answer is 42", resp.Text)
	assert.Equal(t, StopEndTurn, resp.StopReason)
	assert.Equal(t, "claude-opus-5", resp.Model)
	assert.Equal(t, Usage{InputTokens: 11, OutputTokens: 4, CacheReadTokens: 7, CacheWriteTokens: 3}, resp.Usage)
	assert.Empty(t, resp.ToolCalls)
	assert.Empty(t, resp.Refusal)

	// Proof the SDK was redirected onto the stub rather than api.anthropic.com.
	assert.Equal(t, "/v1/messages", stub.firstPath())
}

func TestAnthropicChatConcatenatesTextAndThinkingBlocks(t *testing.T) {
	stub := newAnthropicStub(t)
	stub.reply(http.StatusOK, `{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
		"content": [
			{"type": "thinking", "thinking": "step one. ", "signature": "sig"},
			{"type": "thinking", "thinking": "step two.", "signature": "sig"},
			{"type": "text", "text": "part one "},
			{"type": "text", "text": "part two"}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 1, "output_tokens": 1}
	}`)

	resp, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
	require.NoError(t, err)

	// A reply split across blocks is one answer, not two.
	assert.Equal(t, "part one part two", resp.Text)
	assert.Equal(t, "step one. step two.", resp.Thinking)
}

func TestAnthropicChatToolUse(t *testing.T) {
	stub := newAnthropicStub(t)
	stub.reply(http.StatusOK, `{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
		"content": [
			{"type": "text", "text": "looking that up"},
			{"type": "tool_use", "id": "toolu_1", "name": "Find ticket",
			 "input": {"filter": {"status": "open", "tags": ["urgent"]}, "limit": 25}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 20, "output_tokens": 9}
	}`)

	resp, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
	require.NoError(t, err)

	assert.Equal(t, StopToolUse, resp.StopReason)
	assert.Equal(t, "looking that up", resp.Text)
	require.Len(t, resp.ToolCalls, 1)

	call := resp.ToolCalls[0]
	assert.Equal(t, "toolu_1", call.ID)
	assert.Equal(t, "Find ticket", call.Name)

	// The arguments must arrive parsed, because a node hands them straight to a
	// tool as its input item and that tool's expressions index into them.
	filter, ok := call.Input["filter"].(map[string]any)
	require.True(t, ok, "nested object survived as %T", call.Input["filter"])
	assert.Equal(t, "open", filter["status"])
	assert.Equal(t, []any{"urgent"}, filter["tags"])

	// A number must stay a number: a tool expecting an int would otherwise see
	// the string "25".
	assert.Equal(t, float64(25), call.Input["limit"])
}

func TestAnthropicChatToolUseWithUnparsableInput(t *testing.T) {
	stub := newAnthropicStub(t)
	// A tool_use whose input is not an object: the adapter must still produce a
	// call the agent can report back rather than dropping it silently.
	stub.reply(http.StatusOK, `{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
		"content": [{"type": "tool_use", "id": "toolu_1", "name": "Find", "input": "not-an-object"}],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 1, "output_tokens": 1}
	}`)

	resp, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
	require.NoError(t, err)

	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, `"not-an-object"`, resp.ToolCalls[0].Input["_unparsed"])
}

func TestAnthropicChatStopReasons(t *testing.T) {
	cases := []struct {
		name    string
		stop    string
		extra   string
		want    StopReason
		refusal string
	}{
		{name: "end turn", stop: "end_turn", want: StopEndTurn},
		{name: "max tokens", stop: "max_tokens", want: StopMaxTokens},
		{name: "tool use", stop: "tool_use", want: StopToolUse},
		{name: "an unmapped reason is other", stop: "stop_sequence", want: StopOther},
		{
			name:    "refusal carries the category and explanation",
			stop:    "refusal",
			extra:   `, "stop_details": {"type": "refusal", "category": "general_harms", "explanation": "I will not help with that"}`,
			want:    StopRefusal,
			refusal: "general_harms I will not help with that",
		},
		{
			// stop_details can arrive with no explanation, and an empty Refusal
			// would leave the node reporting a refusal with no reason at all.
			name:    "refusal with no explanation still says something",
			stop:    "refusal",
			extra:   `, "stop_details": {"type": "refusal", "category": "", "explanation": ""}`,
			want:    StopRefusal,
			refusal: "the model declined this request",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := newAnthropicStub(t)
			stub.reply(http.StatusOK, `{
				"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
				"content": [{"type": "text", "text": ""}],
				"stop_reason": `+quote(c.stop)+c.extra+`,
				"usage": {"input_tokens": 1, "output_tokens": 1}
			}`)

			resp, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
			require.NoError(t, err)
			assert.Equal(t, c.want, resp.StopReason)
			assert.Equal(t, c.refusal, resp.Refusal)
		})
	}
}

func TestAnthropicRequestShaping(t *testing.T) {
	t.Run("model and max_tokens default", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
		require.NoError(t, err)

		got := stub.lastRequest(t)
		assert.Equal(t, DefaultModel, got["model"])
		assert.Equal(t, float64(DefaultMaxTokens), got["max_tokens"])
	})

	t.Run("an explicit model and ceiling are sent as given", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{
			Model: ModelHaiku45, MaxTokens: 256, Messages: oneMessage(),
		})
		require.NoError(t, err)

		got := stub.lastRequest(t)
		assert.Equal(t, ModelHaiku45, got["model"])
		assert.Equal(t, float64(256), got["max_tokens"])
	})

	t.Run("a non-positive ceiling falls back to the default", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{MaxTokens: -1, Messages: oneMessage()})
		require.NoError(t, err)
		assert.Equal(t, float64(DefaultMaxTokens), stub.lastRequest(t)["max_tokens"])
	})

	t.Run("system is a block list when set", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{
			System: "You classify tickets.", Messages: oneMessage(),
		})
		require.NoError(t, err)

		// A block rather than a bare string is what makes the prompt cacheable.
		blocks, ok := stub.lastRequest(t)["system"].([]any)
		require.True(t, ok, "system was %#v", stub.lastRequest(t)["system"])
		require.Len(t, blocks, 1)
		block := blocks[0].(map[string]any)
		assert.Equal(t, "text", block["type"])
		assert.Equal(t, "You classify tickets.", block["text"])
	})

	t.Run("system is absent when empty", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
		require.NoError(t, err)
		assert.NotContains(t, stub.lastRequest(t), "system")
	})

	t.Run("thinking is adaptive when asked for", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{Thinking: true, Messages: oneMessage()})
		require.NoError(t, err)

		assert.Equal(t, map[string]any{"type": "adaptive"}, stub.lastRequest(t)["thinking"])
	})

	t.Run("thinking is absent when off", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
		require.NoError(t, err)
		assert.NotContains(t, stub.lastRequest(t), "thinking")
	})

	t.Run("effort is sent only when set", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{Effort: EffortXHigh, Messages: oneMessage()})
		require.NoError(t, err)

		config, ok := stub.lastRequest(t)["output_config"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "xhigh", config["effort"])
	})

	t.Run("no effort means no output_config", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
		require.NoError(t, err)
		assert.NotContains(t, stub.lastRequest(t), "output_config")
	})

	t.Run("a tool schema keeps required and additionalProperties", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{
			Messages: oneMessage(),
			Tools: []ToolSpec{{
				Name:        "Find ticket",
				Description: "Looks a ticket up by id.",
				Schema: map[string]any{
					"type":                 "object",
					"properties":           map[string]any{"id": map[string]any{"type": "string"}},
					"required":             []any{"id"},
					"additionalProperties": false,
				},
			}},
		})
		require.NoError(t, err)

		tools, ok := stub.lastRequest(t)["tools"].([]any)
		require.True(t, ok)
		require.Len(t, tools, 1)
		tool := tools[0].(map[string]any)
		assert.Equal(t, "Find ticket", tool["name"])
		assert.Equal(t, "Looks a ticket up by id.", tool["description"])

		schema := tool["input_schema"].(map[string]any)
		assert.Equal(t, "object", schema["type"])
		assert.Equal(t, map[string]any{"id": map[string]any{"type": "string"}}, schema["properties"])
		// These two ride along as extra fields rather than typed ones, so they
		// are exactly the part that could silently go missing.
		assert.Equal(t, []any{"id"}, schema["required"])
		assert.Equal(t, false, schema["additionalProperties"])
	})

	t.Run("a tool with no schema advertises a permissive one", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{
			Messages: oneMessage(),
			Tools:    []ToolSpec{{Name: "Ping", Description: "Pings."}},
		})
		require.NoError(t, err)

		tool := stub.lastRequest(t)["tools"].([]any)[0].(map[string]any)
		schema := tool["input_schema"].(map[string]any)
		assert.Equal(t, true, schema["additionalProperties"])
	})

	t.Run("no tools means no tools field", func(t *testing.T) {
		stub := newAnthropicStub(t)
		_, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
		require.NoError(t, err)
		assert.NotContains(t, stub.lastRequest(t), "tools")
	})
}

func TestAnthropicStructuredOutput(t *testing.T) {
	stub := newAnthropicStub(t)
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"urgency": map[string]any{"type": "string"}},
		"required":   []any{"urgency"},
	}

	_, err := stub.provider().Chat(context.Background(), Request{
		Messages: oneMessage(),
		Output:   &OutputFormat{JSONSchema: schema},
	})
	require.NoError(t, err)

	config, ok := stub.lastRequest(t)["output_config"].(map[string]any)
	require.True(t, ok, "output_config was %#v", stub.lastRequest(t)["output_config"])
	format, ok := config["format"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "json_schema", format["type"])
	assert.Equal(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{"urgency": map[string]any{"type": "string"}},
		"required":   []any{"urgency"},
	}, format["schema"])
}

func TestAnthropicStructuredOutputIgnoresAnEmptySchema(t *testing.T) {
	stub := newAnthropicStub(t)
	_, err := stub.provider().Chat(context.Background(), Request{
		Messages: oneMessage(),
		Output:   &OutputFormat{},
	})
	require.NoError(t, err)
	assert.NotContains(t, stub.lastRequest(t), "output_config")
}

// An invalid effort is a node author's typo, and catching it before the request
// is the difference between a validation error and a billed 400.
func TestAnthropicRejectsAnInvalidEffortBeforeCalling(t *testing.T) {
	stub := newAnthropicStub(t)

	_, err := stub.provider().Chat(context.Background(), Request{
		Messages: oneMessage(),
		Effort:   "turbo",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"turbo"`)
	assert.Zero(t, stub.callCount(), "the request must not reach the provider")
}

func TestAnthropicRejectsAnEmptyMessageListBeforeCalling(t *testing.T) {
	stub := newAnthropicStub(t)

	_, err := stub.provider().Chat(context.Background(), Request{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one message")
	assert.Zero(t, stub.callCount())
}

func TestAnthropicErrorTranslation(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		contains []string
	}{
		{
			name:   "401 points at the key",
			status: http.StatusUnauthorized,
			// The fix is an environment variable, so the message names it.
			contains: []string{"credentials", "ANTHROPIC_API_KEY", "401"},
		},
		{
			name:     "403 reads the same as 401",
			status:   http.StatusForbidden,
			contains: []string{"credentials", "ANTHROPIC_API_KEY", "403"},
		},
		{
			name:     "404 blames the model id",
			status:   http.StatusNotFound,
			contains: []string{"does not know that model", "404"},
		},
		{
			name:     "429 says to slow down",
			status:   http.StatusTooManyRequests,
			contains: []string{"rate limit", "429"},
		},
		{
			name:     "400 blames the request",
			status:   http.StatusBadRequest,
			contains: []string{"rejected the request", "400"},
		},
		{
			name:     "500 blames the provider",
			status:   http.StatusInternalServerError,
			contains: []string{"unavailable", "500"},
		},
		{
			name:     "an unexpected status still reports the code",
			status:   http.StatusTeapot,
			contains: []string{"request failed", "418"},
		},
	}

	messages := map[string]bool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := newAnthropicStub(t)
			stub.reply(c.status, apiError)

			_, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
			require.Error(t, err)
			for _, want := range c.contains {
				assert.Contains(t, err.Error(), want)
			}
			// Retries are off, so each case is exactly one request.
			assert.Equal(t, 1, stub.callCount())
			messages[err.Error()] = true
		})
	}
	// Every case must read differently, or the execution log cannot tell an
	// author whose problem it is.
	assert.Len(t, messages, len(cases))
}

func TestAnthropicErrorTranslationForATransportFailure(t *testing.T) {
	stub := newAnthropicStub(t)
	stub.server.Close() // nothing is listening, so no request reaches any API

	_, err := stub.provider().Chat(context.Background(), Request{Messages: oneMessage()})
	require.Error(t, err)
	// A transport failure must not be dressed up as a provider response.
	assert.Contains(t, err.Error(), "anthropic request failed")
	assert.NotContains(t, err.Error(), "HTTP ")
}

func TestAnthropicChatHonoursACancelledContext(t *testing.T) {
	stub := newAnthropicStub(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := stub.provider().Chat(ctx, Request{Messages: oneMessage()})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestToSDKMessages(t *testing.T) {
	t.Run("an empty list is an error rather than an empty request", func(t *testing.T) {
		_, err := toSDKMessages(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one message")
	})

	t.Run("an unknown role is refused", func(t *testing.T) {
		_, err := toSDKMessages([]Message{{Role: "system", Text: "nope"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"system"`)
	})

	t.Run("a missing role is treated as a user turn", func(t *testing.T) {
		out, err := toSDKMessages([]Message{{Text: "hi"}})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, anthropic.MessageParamRoleUser, out[0].Role)
	})

	t.Run("an empty assistant turn is dropped, not sent", func(t *testing.T) {
		// An assistant turn with no blocks is rejected by the API with an opaque
		// 400, so it must never leave the process.
		out, err := toSDKMessages([]Message{
			UserText("hi"),
			{Role: RoleAssistant},
			{Role: RoleAssistant, Text: "answer"},
		})
		require.NoError(t, err)
		require.Len(t, out, 2)
		assert.Equal(t, anthropic.MessageParamRoleUser, out[0].Role)
		assert.Equal(t, anthropic.MessageParamRoleAssistant, out[1].Role)
	})

	t.Run("an empty user turn is dropped", func(t *testing.T) {
		out, err := toSDKMessages([]Message{UserText("hi"), {Role: RoleUser}})
		require.NoError(t, err)
		assert.Len(t, out, 1)
	})

	t.Run("dropping every turn is an error", func(t *testing.T) {
		_, err := toSDKMessages([]Message{{Role: RoleAssistant}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one message")
	})

	// A user turn that carries both results and text must send both, because an
	// agent's follow-up instruction rides alongside the results.
	t.Run("a user turn with results and text keeps both, results first", func(t *testing.T) {
		out, err := toSDKMessages([]Message{
			{Role: RoleUser, Text: "now summarise", Results: []ToolResult{{CallID: "toolu_1", Content: "{}"}}},
		})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Len(t, out[0].Content, 2)
		assert.NotNil(t, out[0].Content[0].OfToolResult)
		assert.NotNil(t, out[0].Content[1].OfText)
	})
}

// The tool-calling history is the shape most easily broken, so it is asserted on
// the wire: tool_use blocks in the assistant turn and matching tool_result
// blocks in the user turn that follows.
func TestAnthropicToolHistoryRoundTrips(t *testing.T) {
	stub := newAnthropicStub(t)

	_, err := stub.provider().Chat(context.Background(), Request{
		Messages: []Message{
			UserText("triage this"),
			{Role: RoleAssistant, Text: "checking", ToolCalls: []ToolCall{
				{ID: "toolu_1", Name: "Find", Input: map[string]any{"id": "T-1"}},
				{ID: "toolu_2", Name: "Find", Input: map[string]any{"id": "T-2"}},
			}},
			{Role: RoleUser, Results: []ToolResult{
				{CallID: "toolu_1", Content: `{"ok":true}`},
				{CallID: "toolu_2", Content: "boom", IsError: true},
			}},
		},
	})
	require.NoError(t, err)

	messages, ok := stub.lastRequest(t)["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 3)

	assistant := messages[1].(map[string]any)
	assert.Equal(t, "assistant", assistant["role"])
	blocks := assistant["content"].([]any)
	require.Len(t, blocks, 3)
	assert.Equal(t, "text", blocks[0].(map[string]any)["type"])
	use := blocks[1].(map[string]any)
	assert.Equal(t, "tool_use", use["type"])
	assert.Equal(t, "toolu_1", use["id"])
	assert.Equal(t, "Find", use["name"])
	assert.Equal(t, map[string]any{"id": "T-1"}, use["input"])

	results := messages[2].(map[string]any)
	assert.Equal(t, "user", results["role"])
	resultBlocks := results["content"].([]any)
	require.Len(t, resultBlocks, 2)

	first := resultBlocks[0].(map[string]any)
	assert.Equal(t, "tool_result", first["type"])
	assert.Equal(t, "toolu_1", first["tool_use_id"])
	assert.Contains(t, contentText(t, first), `{"ok":true}`)

	second := resultBlocks[1].(map[string]any)
	// is_error is how the model learns the call failed; without it a failure
	// reads as a successful result that happens to mention an error.
	assert.Equal(t, true, second["is_error"])
	assert.Contains(t, contentText(t, second), "boom")
}

// contentText flattens a tool_result's content, which the SDK may send as a
// string or as a list of text blocks.
func contentText(t *testing.T, block map[string]any) string {
	t.Helper()
	switch typed := block["content"].(type) {
	case string:
		return typed
	case []any:
		var b strings.Builder
		for _, part := range typed {
			if m, ok := part.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	default:
		t.Fatalf("unexpected tool_result content %#v", block["content"])
		return ""
	}
}

func TestAnthropicNameAndModels(t *testing.T) {
	a, err := NewAnthropic("sk-test")
	require.NoError(t, err)
	assert.Equal(t, "anthropic", a.Name())
	assert.Equal(t, []string{ModelOpus5, ModelSonnet5, ModelHaiku45}, a.Models())
}
