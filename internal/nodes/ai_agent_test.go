package nodes

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/llm"
)

/* ---------------------------------------------------------------------------
   The agent is a loop over a provider and a set of bindings, so both are fakes
   here: a scripted provider (see testsupport_test.go) and recordingTool below.
   Nothing in this file touches the network or the engine.
   --------------------------------------------------------------------------- */

// recordingTool is a ToolBinding that records the arguments it was called with.
type recordingTool struct {
	name        string
	description string
	schema      map[string]any

	// out is returned on success; err, when set, is returned instead.
	out []domain.Item
	err error

	mu    sync.Mutex
	calls []map[string]any
}

func (r *recordingTool) binding() ToolBinding {
	return ToolBinding{
		Name:        r.name,
		Description: r.description,
		Schema:      r.schema,
		Invoke: func(_ context.Context, args map[string]any) ([]domain.Item, error) {
			r.mu.Lock()
			r.calls = append(r.calls, args)
			r.mu.Unlock()
			if r.err != nil {
				return nil, r.err
			}
			return r.out, nil
		},
	}
}

func (r *recordingTool) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *recordingTool) call(i int) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[i]
}

// tool builds a successful tool returning one item.
func tool(name, description string, out map[string]any) *recordingTool {
	return &recordingTool{
		name:        name,
		description: description,
		out:         []domain.Item{domain.NewItem(out)},
	}
}

// agentContext builds the ExecContext the engine would hand ai.agent.
func agentContext(provider *fakeProvider, values map[string]any, tools []*recordingTool, item map[string]any) ExecContext {
	ec := ExecContext{
		Ctx:    context.Background(),
		Node:   domain.GraphNode{ID: "n1", Type: "ai.agent", Name: "Triage"},
		Params: params(values),
		Item:   domain.NewItem(item),
	}
	for _, t := range tools {
		ec.Tools = append(ec.Tools, t.binding())
	}
	if provider != nil {
		ec.LLM = provider.registry()
	}
	return ec
}

// toolUse is a turn in which the model asks for tools.
func toolUse(text string, calls ...llm.ToolCall) llm.Response {
	return llm.Response{Text: text, ToolCalls: calls, StopReason: llm.StopToolUse,
		Model: "claude-opus-5", Usage: llm.Usage{InputTokens: 100, OutputTokens: 20}}
}

// answer is a final turn.
func answer(text string) llm.Response {
	return llm.Response{Text: text, StopReason: llm.StopEndTurn,
		Model: "claude-opus-5", Usage: llm.Usage{InputTokens: 200, OutputTokens: 40}}
}

func call(id, name string, args map[string]any) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: name, Input: args}
}

// transcript pulls the _agent block out of an output item.
func transcript(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	got, ok := out["_agent"].(map[string]any)
	require.True(t, ok, "_agent was %#v", out["_agent"])
	return got
}

func turns(t *testing.T, out map[string]any) []agentTurn {
	t.Helper()
	got, ok := transcript(t, out)["turns"].([]agentTurn)
	require.True(t, ok)
	return got
}

// describe builds a toolDescriptions parameter value.
func describe(pairs ...string) []KeyValue {
	rows := make([]KeyValue, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		rows = append(rows, KeyValue{Key: pairs[i], Value: pairs[i+1]})
	}
	return rows
}

func TestAIAgentHappyLoop(t *testing.T) {
	profile := tool("Fetch profile", "", map[string]any{"plan": "pro", "seats": 40})
	provider := scripted(
		toolUse("let me look that up", call("toolu_1", "Fetch profile", map[string]any{"email": "ada@example.com"})),
		answer("This is a pro customer; route to tier 2."),
	)

	res, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
		"prompt":           "Triage this ticket",
		"toolDescriptions": describe("Fetch profile", "Looks a customer up by email."),
	}, []*recordingTool{profile}, map[string]any{"id": 7}))
	require.NoError(t, err)

	out := only(t, res)
	// The final turn's text is the answer; earlier turns are working notes.
	assert.Equal(t, "This is a pro customer; route to tier 2.", out["text"])
	assert.Equal(t, 7, out["id"])

	require.Equal(t, 1, profile.callCount())
	assert.Equal(t, map[string]any{"email": "ada@example.com"}, profile.call(0))

	assert.Equal(t, 2, provider.callCount())
	assert.Equal(t, 1, transcript(t, out)["toolCalls"])
	assert.Equal(t, "end_turn", transcript(t, out)["stopReason"])
	// Usage is the whole run, not the last turn: that is what a bill is made of.
	assert.Equal(t, llm.Usage{InputTokens: 300, OutputTokens: 60}, transcript(t, out)["usage"])

	recorded := turns(t, out)
	require.Len(t, recorded, 2)
	assert.Equal(t, 1, recorded[0].Turn)
	assert.Equal(t, "let me look that up", recorded[0].Text)
	require.Len(t, recorded[0].ToolCalls, 1)
	assert.Equal(t, "Fetch profile", recorded[0].ToolCalls[0].Tool)
	assert.Equal(t, map[string]any{"email": "ada@example.com"}, recorded[0].ToolCalls[0].Args)
	// One returned item is logged as a bare object, matching what the model saw.
	assert.Equal(t, map[string]any{"plan": "pro", "seats": 40}, recorded[0].ToolCalls[0].Result)
	assert.Equal(t, 1, recorded[0].ToolCalls[0].Items)
	assert.Empty(t, recorded[0].ToolCalls[0].Error)

	assert.Equal(t, 2, recorded[1].Turn)
	assert.Empty(t, recorded[1].ToolCalls)

	// The author's description overrides the binding's default, because the
	// prompt author is the one who knows how this agent should use the tool.
	require.Len(t, provider.request(0).Tools, 1)
	assert.Equal(t, "Looks a customer up by email.", provider.request(0).Tools[0].Description)
}

// Parallel calls must come back in ONE user message. Splitting them into a
// message per result teaches the model that parallel calling does not work, and
// it stops doing it — a real and hard-to-spot performance regression.
func TestAIAgentParallelToolCallsReturnInOneUserMessage(t *testing.T) {
	profile := tool("Fetch profile", "", map[string]any{"plan": "pro"})
	history := tool("Fetch history", "", map[string]any{"tickets": 3})
	provider := scripted(
		toolUse("checking both",
			call("toolu_1", "Fetch profile", map[string]any{"email": "ada@example.com"}),
			call("toolu_2", "Fetch history", map[string]any{"email": "ada@example.com"}),
		),
		answer("done"),
	)

	res, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
		"prompt": "Triage this ticket",
		"toolDescriptions": describe(
			"Fetch profile", "Looks a customer up.",
			"Fetch history", "Lists past tickets."),
	}, []*recordingTool{profile, history}, nil))
	require.NoError(t, err)

	assert.Equal(t, 1, profile.callCount())
	assert.Equal(t, 1, history.callCount())

	// Turn 2's request is where the shape of the history is visible.
	messages := provider.request(1).Messages
	require.Len(t, messages, 3)

	userTurns := 0
	for _, m := range messages {
		if m.Role == llm.RoleUser {
			userTurns++
		}
	}
	assert.Equal(t, 2, userTurns, "the task and one results turn, not one turn per result")

	results := messages[2]
	assert.Equal(t, llm.RoleUser, results.Role)
	require.Len(t, results.Results, 2)
	assert.Equal(t, "toolu_1", results.Results[0].CallID)
	assert.Equal(t, "toolu_2", results.Results[1].CallID)
	assert.Contains(t, results.Results[0].Content, `"plan":"pro"`)
	assert.Contains(t, results.Results[1].Content, `"tickets":3`)

	assert.Equal(t, 2, transcript(t, only(t, res))["toolCalls"])
}

// The history the provider sees on turn 2 must be [task, assistant, results]:
// tool results with no preceding assistant tool_use turn are rejected outright.
func TestAIAgentHistoryShape(t *testing.T) {
	profile := tool("Fetch profile", "", map[string]any{"plan": "pro"})
	provider := scripted(
		toolUse("looking", call("toolu_1", "Fetch profile", map[string]any{"email": "a@b.c"})),
		answer("done"),
	)

	_, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
		"prompt":           "Triage this ticket",
		"toolDescriptions": describe("Fetch profile", "Looks a customer up."),
	}, []*recordingTool{profile}, nil))
	require.NoError(t, err)

	// Turn 1 is the task alone.
	first := provider.request(0).Messages
	require.Len(t, first, 1)
	assert.Equal(t, llm.RoleUser, first[0].Role)
	assert.Equal(t, "Triage this ticket", first[0].Text)

	second := provider.request(1).Messages
	require.Len(t, second, 3)

	assert.Equal(t, llm.RoleUser, second[0].Role)
	assert.Equal(t, "Triage this ticket", second[0].Text)

	assert.Equal(t, llm.RoleAssistant, second[1].Role)
	assert.Equal(t, "looking", second[1].Text)
	require.Len(t, second[1].ToolCalls, 1)
	assert.Equal(t, "toolu_1", second[1].ToolCalls[0].ID)

	assert.Equal(t, llm.RoleUser, second[2].Role)
	require.Len(t, second[2].Results, 1)
	assert.Equal(t, "toolu_1", second[2].Results[0].CallID)
	assert.Contains(t, second[2].Results[0].Content, `"plan":"pro"`)
}

// A failing tool is a correctable mistake, not the end of the run: the model is
// told what went wrong and given another turn.
func TestAIAgentToolFailureIsReportedAndTheRunContinues(t *testing.T) {
	broken := &recordingTool{name: "Fetch profile", err: errors.New("http_error: 503 from the CRM")}
	provider := scripted(
		toolUse("looking", call("toolu_1", "Fetch profile", map[string]any{"email": "a@b.c"})),
		answer("The CRM is down; escalating manually."),
	)

	res, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
		"prompt":           "Triage this ticket",
		"toolDescriptions": describe("Fetch profile", "Looks a customer up."),
	}, []*recordingTool{broken}, nil))
	require.NoError(t, err, "a tool failure must not fail the node")

	out := only(t, res)
	assert.Equal(t, "The CRM is down; escalating manually.", out["text"])

	results := provider.request(1).Messages[2].Results
	require.Len(t, results, 1)
	// is_error is the whole point: without it the model reads the failure as a
	// successful result whose payload happens to mention a problem.
	assert.True(t, results[0].IsError)
	assert.Contains(t, results[0].Content, "503 from the CRM")

	logged := turns(t, out)[0].ToolCalls
	require.Len(t, logged, 1)
	assert.Contains(t, logged[0].Error, "503 from the CRM")
	assert.Nil(t, logged[0].Result)
}

// A tool name the model invented is answered with the real names, which is what
// lets it recover on the next turn instead of guessing again.
func TestAIAgentUnknownToolListsTheRealOnes(t *testing.T) {
	profile := tool("Fetch profile", "", map[string]any{"plan": "pro"})
	history := tool("Fetch history", "", map[string]any{"tickets": 3})
	provider := scripted(
		toolUse("guessing", call("toolu_1", "search_crm", map[string]any{"q": "ada"})),
		answer("Used Fetch profile instead."),
	)

	res, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
		"prompt": "Triage this ticket",
		"toolDescriptions": describe(
			"Fetch profile", "Looks a customer up.",
			"Fetch history", "Lists past tickets."),
	}, []*recordingTool{profile, history}, nil))
	require.NoError(t, err)

	assert.Zero(t, profile.callCount())
	assert.Zero(t, history.callCount())

	results := provider.request(1).Messages[2].Results
	require.Len(t, results, 1)
	assert.True(t, results[0].IsError)
	assert.Contains(t, results[0].Content, `"search_crm"`)
	assert.Contains(t, results[0].Content, "Fetch profile")
	assert.Contains(t, results[0].Content, "Fetch history")

	assert.Equal(t, "Used Fetch profile instead.", only(t, res)["text"])
	assert.Contains(t, turns(t, only(t, res))[0].ToolCalls[0].Error, "search_crm")
}

func TestAIAgentIterationCeiling(t *testing.T) {
	cases := []struct {
		name          string
		maxIterations any
		wantCalls     int
	}{
		{name: "explicit limit", maxIterations: 3, wantCalls: 3},
		{name: "default limit", maxIterations: nil, wantCalls: DefaultAgentIterations},
		// A limit below one would mean "never call the model", which is never
		// what an author meant, so it is clamped to a single turn.
		{name: "zero is clamped to one", maxIterations: 0, wantCalls: 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			looping := tool("Fetch profile", "", map[string]any{"plan": "pro"})
			provider := repeating(toolUse("again",
				call("toolu_1", "Fetch profile", map[string]any{"email": "a@b.c"})))

			values := map[string]any{
				"prompt":           "Triage this ticket",
				"toolDescriptions": describe("Fetch profile", "Looks a customer up."),
			}
			if c.maxIterations != nil {
				values["maxIterations"] = c.maxIterations
			}

			_, err := AIAgent{}.Execute(agentContext(provider, values, []*recordingTool{looping}, nil))

			var nodeErr *domain.NodeError
			require.ErrorAs(t, err, &nodeErr)
			assert.Equal(t, domain.ErrCodeAgentBudget, nodeErr.Code)
			// The ceiling counts provider calls, which is what costs money.
			assert.Equal(t, c.wantCalls, provider.callCount())
			// The last turn's tools are not run: the budget is already spent.
			assert.Equal(t, c.wantCalls-1, looping.callCount())
		})
	}
}

func TestAIAgentUndescribedToolIsRefused(t *testing.T) {
	described := tool("Fetch profile", "Looks a customer up.", map[string]any{"plan": "pro"})
	// No description on the binding and no row in toolDescriptions.
	bare := &recordingTool{name: "Delete account"}
	provider := scripted(answer("never reached"))

	_, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
		"prompt":           "Triage this ticket",
		"toolDescriptions": describe("Fetch profile", "Looks a customer up."),
	}, []*recordingTool{described, bare}, nil))

	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
	// Naming the tool is the point: an undescribed tool is simply never chosen,
	// so refusing up front is the only way the author finds out.
	assert.Contains(t, nodeErr.Message, "Delete account")
	assert.NotContains(t, nodeErr.Message, "Fetch profile")
	assert.Zero(t, provider.callCount())
}

func TestAIAgentBlankDescriptionRowIsStillUndescribed(t *testing.T) {
	bare := &recordingTool{name: "Fetch profile"}
	provider := scripted(answer("never reached"))

	_, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
		"prompt":           "Triage this ticket",
		"toolDescriptions": describe("Fetch profile", "   "),
	}, []*recordingTool{bare}, nil))

	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
	assert.Contains(t, nodeErr.Message, "Fetch profile")
}

// A binding that already carries the node type's own description is enough; the
// toolDescriptions row is an override, not a requirement.
func TestAIAgentBindingDescriptionSatisfiesTheCheck(t *testing.T) {
	profile := tool("Fetch profile", "Makes an HTTP request.", map[string]any{"plan": "pro"})
	provider := scripted(answer("done"))

	_, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
		"prompt": "Triage this ticket",
	}, []*recordingTool{profile}, nil))
	require.NoError(t, err)

	require.Len(t, provider.request(0).Tools, 1)
	assert.Equal(t, "Makes an HTTP request.", provider.request(0).Tools[0].Description)
}

func TestAIAgentToolSchemas(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"email": map[string]any{"type": "string"}},
		"required":   []any{"email"},
	}

	t.Run("a schema reaches the provider's tool spec", func(t *testing.T) {
		profile := tool("Fetch profile", "Looks a customer up.", map[string]any{"plan": "pro"})
		provider := scripted(answer("done"))

		_, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
			"prompt":      "Triage this ticket",
			"toolSchemas": map[string]any{"Fetch profile": schema},
		}, []*recordingTool{profile}, nil))
		require.NoError(t, err)

		require.Len(t, provider.request(0).Tools, 1)
		assert.Equal(t, schema, provider.request(0).Tools[0].Schema)
	})

	t.Run("a tool with no schema entry keeps an empty one", func(t *testing.T) {
		profile := tool("Fetch profile", "Looks a customer up.", map[string]any{"plan": "pro"})
		other := tool("Fetch history", "Lists past tickets.", map[string]any{"tickets": 1})
		provider := scripted(answer("done"))

		_, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
			"prompt":      "Triage this ticket",
			"toolSchemas": map[string]any{"Fetch profile": schema},
		}, []*recordingTool{profile, other}, nil))
		require.NoError(t, err)

		specs := provider.request(0).Tools
		require.Len(t, specs, 2)
		assert.Equal(t, schema, specs[0].Schema)
		// Empty rather than invented: the provider layer turns this into a
		// permissive schema, which is the honest default.
		assert.Empty(t, specs[1].Schema)
	})

	t.Run("a non-object schema is a validation error naming the tool", func(t *testing.T) {
		profile := tool("Fetch profile", "Looks a customer up.", map[string]any{"plan": "pro"})
		provider := scripted(answer("never reached"))

		_, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
			"prompt":      "Triage this ticket",
			"toolSchemas": map[string]any{"Fetch profile": "a string is not a schema"},
		}, []*recordingTool{profile}, nil))

		var nodeErr *domain.NodeError
		require.ErrorAs(t, err, &nodeErr)
		assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
		assert.Contains(t, nodeErr.Message, "Fetch profile")
		assert.Zero(t, provider.callCount())
	})

	t.Run("toolSchemas that is not an object at all is refused", func(t *testing.T) {
		profile := tool("Fetch profile", "Looks a customer up.", map[string]any{"plan": "pro"})
		provider := scripted(answer("never reached"))

		_, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
			"prompt":      "Triage this ticket",
			"toolSchemas": "[]",
		}, []*recordingTool{profile}, nil))

		var nodeErr *domain.NodeError
		require.ErrorAs(t, err, &nodeErr)
		assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
	})
}

func TestAIAgentRefusal(t *testing.T) {
	t.Run("on the first turn", func(t *testing.T) {
		provider := scripted(llm.Response{StopReason: llm.StopRefusal, Refusal: "cyber I will not do that"})

		_, err := AIAgent{}.Execute(agentContext(provider,
			map[string]any{"prompt": "Triage this ticket"}, nil, nil))

		var nodeErr *domain.NodeError
		require.ErrorAs(t, err, &nodeErr)
		assert.Equal(t, domain.ErrCodeLLMRefusal, nodeErr.Code)
		assert.Contains(t, nodeErr.Message, "I will not do that")
		assert.Contains(t, nodeErr.Message, "turn 1")
	})

	t.Run("after a tool call", func(t *testing.T) {
		profile := tool("Fetch profile", "Looks a customer up.", map[string]any{"plan": "pro"})
		provider := scripted(
			toolUse("looking", call("toolu_1", "Fetch profile", map[string]any{"email": "a@b.c"})),
			llm.Response{StopReason: llm.StopRefusal, Refusal: "general_harms no"},
		)

		_, err := AIAgent{}.Execute(agentContext(provider,
			map[string]any{"prompt": "Triage this ticket"}, []*recordingTool{profile}, nil))

		var nodeErr *domain.NodeError
		require.ErrorAs(t, err, &nodeErr)
		assert.Equal(t, domain.ErrCodeLLMRefusal, nodeErr.Code)
		// Naming the turn matters: the refusal came after real work, and the
		// transcript is gone, so the message is all the author has.
		assert.Contains(t, nodeErr.Message, "turn 2")
	})
}

func TestAIAgentProviderError(t *testing.T) {
	provider := failing(errors.New("anthropic is unavailable (HTTP 503)"))

	_, err := AIAgent{}.Execute(agentContext(provider,
		map[string]any{"prompt": "Triage this ticket"}, nil, nil))

	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeLLM, nodeErr.Code)
	assert.Contains(t, nodeErr.Message, "turn 1")
	assert.Contains(t, nodeErr.Message, "503")
}

// A cancelled run must stop before spending anything, and must report
// cancellation rather than an LLM fault.
func TestAIAgentCancellationBeforeTheFirstTurn(t *testing.T) {
	provider := scripted(answer("never reached"))
	ec := agentContext(provider, map[string]any{"prompt": "Triage this ticket"}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ec.Ctx = ctx

	_, err := AIAgent{}.Execute(ec)

	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeCancelled, nodeErr.Code)
	assert.Contains(t, nodeErr.Message, "0 turn(s)")
	assert.Zero(t, provider.callCount())
}

func TestAIAgentIncludeTranscript(t *testing.T) {
	t.Run("on by default", func(t *testing.T) {
		res, err := AIAgent{}.Execute(agentContext(scripted(answer("done")),
			map[string]any{"prompt": "Triage this ticket"}, nil, nil))
		require.NoError(t, err)
		assert.Contains(t, only(t, res), "_agent")
	})

	t.Run("off omits _agent entirely", func(t *testing.T) {
		res, err := AIAgent{}.Execute(agentContext(scripted(answer("done")),
			map[string]any{"prompt": "Triage this ticket", "includeTranscript": false}, nil, nil))
		require.NoError(t, err)

		out := only(t, res)
		assert.NotContains(t, out, "_agent")
		// The answer is still there; only the working notes go.
		assert.Equal(t, "done", out["text"])
	})
}

// An agent with nothing wired to its tool handle is legal and behaves like the
// chat node, which is what the descriptor's notice promises.
func TestAIAgentWithZeroTools(t *testing.T) {
	provider := scripted(answer("no tools needed"))

	res, err := AIAgent{}.Execute(agentContext(provider,
		map[string]any{"prompt": "Summarise this ticket"}, nil, map[string]any{"id": 7}))
	require.NoError(t, err)

	out := only(t, res)
	assert.Equal(t, "no tools needed", out["text"])
	assert.Equal(t, 7, out["id"])
	assert.Equal(t, 0, transcript(t, out)["toolCalls"])

	assert.Empty(t, provider.request(0).Tools)
	assert.Equal(t, 1, provider.callCount())
}

// A model that stops with tool_use but names no tool would otherwise loop
// forever waiting for calls that never come.
func TestAIAgentToolUseWithNoCallsEndsTheRun(t *testing.T) {
	provider := scripted(llm.Response{Text: "hmm", StopReason: llm.StopToolUse})

	res, err := AIAgent{}.Execute(agentContext(provider,
		map[string]any{"prompt": "Triage this ticket"}, nil, nil))
	require.NoError(t, err)

	assert.Equal(t, "hmm", only(t, res)["text"])
	assert.Equal(t, 1, provider.callCount())
}

func TestAIAgentRequiresAPrompt(t *testing.T) {
	for name, prompt := range map[string]any{"absent": nil, "blank": "  "} {
		t.Run(name, func(t *testing.T) {
			provider := scripted(answer("never reached"))
			values := map[string]any{}
			if prompt != nil {
				values["prompt"] = prompt
			}

			_, err := AIAgent{}.Execute(agentContext(provider, values, nil, nil))

			var nodeErr *domain.NodeError
			require.ErrorAs(t, err, &nodeErr)
			assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
			assert.Contains(t, nodeErr.Message, `"prompt"`)
			assert.Zero(t, provider.callCount())
		})
	}
}

func TestAIAgentWithNoProviderConfigured(t *testing.T) {
	for name, registry := range map[string]*llm.Registry{
		"nil registry":   nil,
		"empty registry": llm.NewRegistry(),
	} {
		t.Run(name, func(t *testing.T) {
			ec := agentContext(nil, map[string]any{"prompt": "Triage this ticket"}, nil, nil)
			ec.LLM = registry

			_, err := AIAgent{}.Execute(ec)

			var nodeErr *domain.NodeError
			require.ErrorAs(t, err, &nodeErr)
			assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
			assert.Contains(t, nodeErr.Message, "ANTHROPIC_API_KEY")
		})
	}
}

func TestAIAgentDefaults(t *testing.T) {
	provider := scripted(answer("done"))

	_, err := AIAgent{}.Execute(agentContext(provider,
		map[string]any{"prompt": "Triage this ticket"}, nil, nil))
	require.NoError(t, err)

	req := provider.request(0)
	assert.Equal(t, llm.DefaultModel, req.Model)
	assert.Equal(t, llm.DefaultMaxTokens, req.MaxTokens)
	assert.Equal(t, llm.EffortHigh, req.Effort)
	// Thinking is not a parameter on this node: a non-thinking model tends to
	// narrate a tool call instead of making one.
	assert.True(t, req.Thinking)

	defaults := paramDefaults(AIAgent{}.Descriptor())
	assert.Equal(t, defaults["model"], req.Model)
	assert.Equal(t, defaults["maxTokens"], req.MaxTokens)
	assert.Equal(t, defaults["effort"], string(req.Effort))
	assert.Equal(t, defaults["maxIterations"], DefaultAgentIterations)
	assert.Equal(t, defaults["includeTranscript"], true)
	assert.NotContains(t, defaults, "thinking")
}

func TestAIAgentExplicitSettingsReachTheProvider(t *testing.T) {
	provider := scripted(answer("done"))

	_, err := AIAgent{}.Execute(agentContext(provider, map[string]any{
		"prompt":    "Triage this ticket",
		"system":    "You triage support tickets.",
		"model":     llm.ModelSonnet5,
		"maxTokens": 4096,
		"effort":    string(llm.EffortMax),
	}, nil, nil))
	require.NoError(t, err)

	req := provider.request(0)
	assert.Equal(t, "You triage support tickets.", req.System)
	assert.Equal(t, llm.ModelSonnet5, req.Model)
	assert.Equal(t, 4096, req.MaxTokens)
	assert.Equal(t, llm.EffortMax, req.Effort)
}

// A tool that returns several items is handed over as an array, and none as an
// empty object — never a bare null, which reads to the model as a failure.
func TestAIAgentToolResultShapes(t *testing.T) {
	cases := []struct {
		name string
		out  []domain.Item
		want string
	}{
		{name: "one item is a bare object", out: items(map[string]any{"a": 1}), want: `{"a":1}`},
		{
			name: "several items are an array",
			out:  items(map[string]any{"a": 1}, map[string]any{"a": 2}),
			want: `[{"a":1},{"a":2}]`,
		},
		{name: "no items is an empty object", out: nil, want: `{}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			returning := &recordingTool{name: "Fetch", description: "Fetches.", out: c.out}
			provider := scripted(
				toolUse("looking", call("toolu_1", "Fetch", map[string]any{})),
				answer("done"),
			)

			res, err := AIAgent{}.Execute(agentContext(provider,
				map[string]any{"prompt": "go"}, []*recordingTool{returning}, nil))
			require.NoError(t, err)

			results := provider.request(1).Messages[2].Results
			require.Len(t, results, 1)
			assert.Equal(t, c.want, results[0].Content)
			assert.False(t, results[0].IsError)
			assert.Equal(t, len(c.out), turns(t, only(t, res))[0].ToolCalls[0].Items)
		})
	}
}

func TestAIAgentDescriptor(t *testing.T) {
	d := AIAgent{}.Descriptor()

	assert.Equal(t, "ai.agent", d.Type)
	assert.Equal(t, CategoryAI, d.Category)
	assert.Equal(t, ModePerItem, d.Mode)
	assert.Equal(t, MainOut, d.Outputs)

	// The tool handle is what the engine's graph validation looks for, and what
	// makes the tool wiring visible on the canvas.
	names := make([]string, 0, len(d.Inputs))
	for _, h := range d.Inputs {
		names = append(names, h.Name)
	}
	assert.Equal(t, []string{HandleMain, HandleTool}, names)
}
