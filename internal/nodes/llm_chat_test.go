package nodes

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/llm"
)

// chatContext builds the ExecContext the engine would hand llm.chat for one
// item, with the fake provider standing in for Anthropic.
func chatContext(provider *fakeProvider, values map[string]any, item map[string]any) ExecContext {
	ec := ExecContext{
		Ctx:    context.Background(),
		Node:   domain.GraphNode{ID: "n1", Type: "llm.chat", Name: "Classify"},
		Params: params(values),
		Item:   domain.NewItem(item),
	}
	if provider != nil {
		ec.LLM = provider.registry()
	}
	return ec
}

// only returns the single item a per-item node produced.
func only(t *testing.T, res Result) map[string]any {
	t.Helper()
	got := res.Get(HandleMain)
	require.Len(t, got, 1)
	return got[0].JSON
}

func TestLLMChatText(t *testing.T) {
	provider := scripted(llm.Response{
		Text:       "urgent",
		StopReason: llm.StopEndTurn,
		Model:      "claude-opus-5",
		Usage:      llm.Usage{InputTokens: 30, OutputTokens: 2},
	})

	res, err := LLMChat{}.Execute(chatContext(provider,
		map[string]any{"prompt": "Classify this ticket"},
		map[string]any{"id": 7, "subject": "printer on fire"}))
	require.NoError(t, err)

	// The reply is merged onto the input item: the classification is only useful
	// next to what it classified.
	assert.Equal(t, map[string]any{
		"id":      7,
		"subject": "printer on fire",
		"text":    "urgent",
	}, only(t, res))

	require.Equal(t, 1, provider.callCount())
	req := provider.request(0)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, llm.RoleUser, req.Messages[0].Role)
	assert.Equal(t, "Classify this ticket", req.Messages[0].Text)
	assert.Nil(t, req.Output)
	assert.Empty(t, req.Tools, "the chat node never offers tools; that is the agent's job")
}

// text overwrites an input field of the same name rather than being dropped,
// because the reply is what the author asked this node for.
func TestLLMChatTextOverwritesAnExistingTextField(t *testing.T) {
	provider := scripted(llm.Response{Text: "new", StopReason: llm.StopEndTurn})

	res, err := LLMChat{}.Execute(chatContext(provider,
		map[string]any{"prompt": "go"},
		map[string]any{"text": "old", "id": 1}))
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"text": "new", "id": 1}, only(t, res))
}

func TestLLMChatSystemPrompt(t *testing.T) {
	provider := scripted(llm.Response{Text: "ok", StopReason: llm.StopEndTurn})

	_, err := LLMChat{}.Execute(chatContext(provider, map[string]any{
		"prompt": "go",
		"system": "You classify support tickets.",
	}, nil))
	require.NoError(t, err)

	assert.Equal(t, "You classify support tickets.", provider.request(0).System)
}

func TestLLMChatJSONMode(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"urgency": map[string]any{"type": "string"}},
		"required":   []any{"urgency"},
	}

	t.Run("a valid reply is merged into the item", func(t *testing.T) {
		provider := scripted(llm.Response{
			Text:       `{"urgency":"high","score":0.9}`,
			StopReason: llm.StopEndTurn,
		})

		res, err := LLMChat{}.Execute(chatContext(provider, map[string]any{
			"prompt":       "go",
			"outputFormat": "json",
			"jsonSchema":   schema,
		}, map[string]any{"id": 7}))
		require.NoError(t, err)

		assert.Equal(t, map[string]any{"id": 7, "urgency": "high", "score": 0.9}, only(t, res))
		// In JSON mode there is no "text" field at all: the parsed object *is*
		// the reply, and a duplicate copy would only confuse downstream nodes.
		assert.NotContains(t, only(t, res), "text")

		require.NotNil(t, provider.request(0).Output)
		assert.Equal(t, schema, provider.request(0).Output.JSONSchema)
	})

	t.Run("a schema written as JSON text is accepted", func(t *testing.T) {
		provider := scripted(llm.Response{Text: `{"urgency":"low"}`, StopReason: llm.StopEndTurn})

		_, err := LLMChat{}.Execute(chatContext(provider, map[string]any{
			"prompt":       "go",
			"outputFormat": "json",
			// The JSON parameter editor hands the engine a string.
			"jsonSchema": `{"type":"object","properties":{"urgency":{"type":"string"}}}`,
		}, nil))
		require.NoError(t, err)

		require.NotNil(t, provider.request(0).Output)
		assert.Equal(t, "object", provider.request(0).Output.JSONSchema["type"])
	})

	t.Run("a missing schema is refused before the call", func(t *testing.T) {
		cases := map[string]any{
			"absent":       nil,
			"empty string": "   ",
			"empty object": map[string]any{},
		}
		for name, value := range cases {
			t.Run(name, func(t *testing.T) {
				provider := scripted(llm.Response{Text: "{}", StopReason: llm.StopEndTurn})
				values := map[string]any{"prompt": "go", "outputFormat": "json"}
				if value != nil {
					values["jsonSchema"] = value
				}

				_, err := LLMChat{}.Execute(chatContext(provider, values, nil))

				var nodeErr *domain.NodeError
				require.ErrorAs(t, err, &nodeErr)
				assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
				assert.Contains(t, nodeErr.Message, "schema")
				// JSON mode without a schema is not worth a billed request.
				assert.Zero(t, provider.callCount())
			})
		}
	})

	t.Run("an unparsable schema is a validation error", func(t *testing.T) {
		provider := scripted(llm.Response{Text: "{}", StopReason: llm.StopEndTurn})

		_, err := LLMChat{}.Execute(chatContext(provider, map[string]any{
			"prompt":       "go",
			"outputFormat": "json",
			"jsonSchema":   "{not json",
		}, nil))

		var nodeErr *domain.NodeError
		require.ErrorAs(t, err, &nodeErr)
		assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
		assert.Contains(t, nodeErr.Message, "jsonSchema")
	})

	t.Run("a reply that is not JSON reports what the model said", func(t *testing.T) {
		provider := scripted(llm.Response{
			Text:       "I think this ticket is fairly urgent, actually.",
			StopReason: llm.StopEndTurn,
		})

		_, err := LLMChat{}.Execute(chatContext(provider, map[string]any{
			"prompt":       "go",
			"outputFormat": "json",
			"jsonSchema":   schema,
		}, nil))

		var nodeErr *domain.NodeError
		require.ErrorAs(t, err, &nodeErr)
		assert.Equal(t, domain.ErrCodeLLM, nodeErr.Code)
		// Quoting the reply is what makes this debuggable: the author needs to
		// see that the model answered in prose.
		assert.Contains(t, nodeErr.Message, "fairly urgent")
	})

	t.Run("a JSON array reply is refused because it cannot merge into an item", func(t *testing.T) {
		provider := scripted(llm.Response{Text: `["high"]`, StopReason: llm.StopEndTurn})

		_, err := LLMChat{}.Execute(chatContext(provider, map[string]any{
			"prompt":       "go",
			"outputFormat": "json",
			"jsonSchema":   schema,
		}, nil))

		var nodeErr *domain.NodeError
		require.ErrorAs(t, err, &nodeErr)
		assert.Equal(t, domain.ErrCodeLLM, nodeErr.Code)
	})
}

func TestLLMChatIncludeUsage(t *testing.T) {
	response := llm.Response{
		Text:       "ok",
		StopReason: llm.StopEndTurn,
		Model:      "claude-haiku-4-5",
		Usage:      llm.Usage{InputTokens: 120, OutputTokens: 8},
	}

	t.Run("off by default", func(t *testing.T) {
		res, err := LLMChat{}.Execute(chatContext(scripted(response),
			map[string]any{"prompt": "go"}, nil))
		require.NoError(t, err)
		assert.NotContains(t, only(t, res), "_llm")
	})

	t.Run("on adds the model, stop reason and token counts", func(t *testing.T) {
		res, err := LLMChat{}.Execute(chatContext(scripted(response),
			map[string]any{"prompt": "go", "includeUsage": true}, nil))
		require.NoError(t, err)

		assert.Equal(t, map[string]any{
			"model":        "claude-haiku-4-5",
			"stopReason":   "end_turn",
			"inputTokens":  120,
			"outputTokens": 8,
		}, only(t, res)["_llm"])
	})
}

// A truncated reply still succeeds — the data is real, just short — so the only
// signal is the log line, and the node must not fail here.
func TestLLMChatTruncatedReplyStillSucceeds(t *testing.T) {
	provider := scripted(llm.Response{Text: "half an ans", StopReason: llm.StopMaxTokens})

	res, err := LLMChat{}.Execute(chatContext(provider,
		map[string]any{"prompt": "go", "maxTokens": 4}, nil))
	require.NoError(t, err)
	assert.Equal(t, "half an ans", only(t, res)["text"])
}

func TestLLMChatRefusal(t *testing.T) {
	provider := scripted(llm.Response{
		StopReason: llm.StopRefusal,
		Refusal:    "general_harms I will not write that",
	})

	_, err := LLMChat{}.Execute(chatContext(provider, map[string]any{"prompt": "go"}, nil))

	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	// A refusal is not a retryable fault, which is why it has its own code.
	assert.Equal(t, domain.ErrCodeLLMRefusal, nodeErr.Code)
	assert.Contains(t, nodeErr.Message, "I will not write that")
}

func TestLLMChatProviderError(t *testing.T) {
	provider := failing(errors.New("anthropic rate limit reached (HTTP 429)"))

	_, err := LLMChat{}.Execute(chatContext(provider, map[string]any{"prompt": "go"}, nil))

	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeLLM, nodeErr.Code)
	assert.Contains(t, nodeErr.Message, "429")
}

// No API key is the state of a fresh deployment, so the message has to name the
// variable to set rather than reading as an internal fault.
func TestLLMChatWithNoProviderConfigured(t *testing.T) {
	cases := map[string]*llm.Registry{
		"nil registry":   nil,
		"empty registry": llm.NewRegistry(),
	}
	for name, registry := range cases {
		t.Run(name, func(t *testing.T) {
			ec := chatContext(nil, map[string]any{"prompt": "go"}, nil)
			ec.LLM = registry

			_, err := LLMChat{}.Execute(ec)

			var nodeErr *domain.NodeError
			require.ErrorAs(t, err, &nodeErr)
			assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
			assert.Contains(t, nodeErr.Message, "ANTHROPIC_API_KEY")
		})
	}
}

func TestLLMChatRequiresAPrompt(t *testing.T) {
	for name, prompt := range map[string]any{"absent": nil, "blank": "   \n "} {
		t.Run(name, func(t *testing.T) {
			provider := scripted(llm.Response{Text: "ok", StopReason: llm.StopEndTurn})
			values := map[string]any{}
			if prompt != nil {
				values["prompt"] = prompt
			}

			_, err := LLMChat{}.Execute(chatContext(provider, values, nil))

			var nodeErr *domain.NodeError
			require.ErrorAs(t, err, &nodeErr)
			assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
			assert.Contains(t, nodeErr.Message, `"prompt"`)
			assert.Zero(t, provider.callCount())
		})
	}
}

// The node's fallbacks and the descriptor's Default values are two copies of the
// same decision, and a config panel that shows "high" while the node sends
// something else would be a lie.
func TestLLMChatDefaults(t *testing.T) {
	provider := scripted(llm.Response{Text: "ok", StopReason: llm.StopEndTurn})

	_, err := LLMChat{}.Execute(chatContext(provider, map[string]any{"prompt": "go"}, nil))
	require.NoError(t, err)

	req := provider.request(0)
	assert.Equal(t, llm.DefaultModel, req.Model)
	assert.Equal(t, llm.DefaultMaxTokens, req.MaxTokens)
	assert.Equal(t, llm.EffortHigh, req.Effort)
	assert.True(t, req.Thinking)
	assert.Empty(t, req.System)

	defaults := paramDefaults(LLMChat{}.Descriptor())
	assert.Equal(t, defaults["model"], req.Model)
	assert.Equal(t, defaults["maxTokens"], req.MaxTokens)
	assert.Equal(t, defaults["effort"], string(req.Effort))
	assert.Equal(t, defaults["thinking"], req.Thinking)
	assert.Equal(t, defaults["outputFormat"], "text")
	assert.Equal(t, defaults["includeUsage"], false)
}

func TestLLMChatExplicitSettingsReachTheProvider(t *testing.T) {
	provider := scripted(llm.Response{Text: "ok", StopReason: llm.StopEndTurn})

	_, err := LLMChat{}.Execute(chatContext(provider, map[string]any{
		"prompt":    "go",
		"model":     llm.ModelHaiku45,
		"maxTokens": 512,
		"effort":    string(llm.EffortLow),
		"thinking":  false,
	}, nil))
	require.NoError(t, err)

	req := provider.request(0)
	assert.Equal(t, llm.ModelHaiku45, req.Model)
	assert.Equal(t, 512, req.MaxTokens)
	assert.Equal(t, llm.EffortLow, req.Effort)
	assert.False(t, req.Thinking)
}

func TestLLMChatDescriptor(t *testing.T) {
	d := LLMChat{}.Descriptor()

	assert.Equal(t, "llm.chat", d.Type)
	assert.Equal(t, CategoryAI, d.Category)
	// Per-item, because a prompt is written against one record.
	assert.Equal(t, ModePerItem, d.Mode)
	assert.Equal(t, MainIn, d.Inputs)
	assert.Equal(t, MainOut, d.Outputs)
	// The chat node takes no tools: a tool edge into it is a graph validation
	// error, and that check reads the descriptor's handles.
	for _, h := range d.Inputs {
		assert.NotEqual(t, HandleTool, h.Name)
	}
	assert.False(t, d.IsTrigger)
}

// paramDefaults indexes a descriptor's Default values by parameter name.
func paramDefaults(d Descriptor) map[string]any {
	out := make(map[string]any, len(d.Params))
	for _, p := range d.Params {
		out[p.Name] = p.Default
	}
	return out
}
