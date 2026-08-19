package nodes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/llm"
)

// DefaultAgentIterations bounds the tool-calling loop. A model that keeps asking
// for tools past this point is usually stuck rather than making progress, and an
// unbounded loop bills real money.
const DefaultAgentIterations = 10

// AIAgent runs a model in a loop, letting it call the nodes wired into its tool
// input until it has an answer.
//
// The whole transcript — every model turn and every tool call with its arguments
// and result — is returned in the output item. That is deliberate: an agent that
// gives a wrong answer is only debuggable if you can see what it looked at, and
// the execution log shows one row for this node no matter how many turns it took.
type AIAgent struct{}

// Descriptor implements Node.
func (AIAgent) Descriptor() Descriptor {
	return Descriptor{
		Type:        "ai.agent",
		Name:        "AI Agent",
		Category:    CategoryAI,
		Description: "Runs a model that can call the nodes wired to its tool input.",
		Icon:        "robot",
		Mode:        ModePerItem,
		Inputs: []Handle{
			{Name: HandleMain, Label: "Input"},
			{Name: HandleTool, Label: "Tools"},
		},
		Outputs:    MainOut,
		Credential: "anthropicApi",
		// Phase 2 reads the key from the environment, so a workspace credential
		// is an upgrade rather than a prerequisite.
		CredentialOptional: true,
		Params: []ParamSpec{
			{
				Name:        "model",
				Label:       "Model",
				Type:        ParamSelect,
				Options:     modelOptions(),
				Default:     llm.DefaultModel,
				Description: "Tool-calling rewards a capable model; Haiku will struggle with a long tool list.",
			},
			{
				Name:               "system",
				Label:              "System prompt",
				Type:               ParamCode,
				SupportsExpression: true,
				Placeholder:        "You triage support tickets. Look up the customer before deciding.",
				Description:        "Who the agent is and how it should work. Describe when to use each tool here.",
			},
			{
				Name:               "prompt",
				Label:              "Task",
				Type:               ParamCode,
				Required:           true,
				SupportsExpression: true,
				Placeholder:        "Triage this ticket:\n\n{{ $json.subject }}\n{{ $json.body }}",
				Description:        "What to do with this item.",
			},
			{
				Name:  "toolDescriptions",
				Label: "Tool descriptions",
				Type:  ParamKeyValue,
				Description: "One row per node wired to the Tools input: the node's name, " +
					"and what it does. The model chooses tools from these descriptions, so " +
					"a vague one gets ignored.",
			},
			{
				Name:        "toolSchemas",
				Label:       "Tool argument schemas",
				Type:        ParamJSON,
				Placeholder: `{"Fetch profile": {"type":"object","properties":{"email":{"type":"string"}},"required":["email"]}}`,
				Description: "Optional. Node name to JSON Schema. A tool with no schema accepts any object.",
			},
			{
				Name:        "maxIterations",
				Label:       "Max tool rounds",
				Type:        ParamNumber,
				Default:     DefaultAgentIterations,
				Description: "How many times the model may call tools before the run is cut off.",
			},
			{
				Name:    "effort",
				Label:   "Effort",
				Type:    ParamSelect,
				Default: string(llm.EffortHigh),
				Options: []ParamOption{
					{Label: "Low — cheapest", Value: string(llm.EffortLow)},
					{Label: "Medium", Value: string(llm.EffortMedium)},
					{Label: "High — default", Value: string(llm.EffortHigh)},
					{Label: "Extra high — best for agents", Value: string(llm.EffortXHigh)},
					{Label: "Max — correctness over cost", Value: string(llm.EffortMax)},
				},
			},
			{
				Name:        "maxTokens",
				Label:       "Max tokens per turn",
				Type:        ParamNumber,
				Default:     llm.DefaultMaxTokens,
				Description: "A ceiling on each model turn, not the whole run.",
			},
			{
				Name:        "includeTranscript",
				Label:       "Include the transcript",
				Type:        ParamBoolean,
				Default:     true,
				Description: "Adds _agent with every turn and tool call. Leave on until the agent is trusted.",
			},
			{
				Name:  "toolNotice",
				Label: "",
				Type:  ParamNotice,
				Default: "Wire nodes into the Tools input to give the agent tools. " +
					"An agent with no tools is just an LLM node.",
			},
		},
	}
}

// agentTurn is one model turn plus the tool calls it asked for.
type agentTurn struct {
	Turn      int             `json:"turn"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ToolCalls []agentToolCall `json:"toolCalls,omitempty"`
	Usage     llm.Usage       `json:"usage"`
}

// agentToolCall records one invocation as the UI shows it.
type agentToolCall struct {
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args"`
	Result any            `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
	Items  int            `json:"items"`
}

// Execute implements Node.
func (AIAgent) Execute(ec ExecContext) (Result, error) {
	provider, err := ec.LLM.Default()
	if err != nil {
		return Result{}, domain.Errorf(domain.ErrCodeValidation, "%s", err.Error())
	}

	task, err := ec.Params.String("prompt")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(task) == "" {
		return Result{}, ErrRequiredParam("prompt")
	}

	tools, err := agentTools(ec)
	if err != nil {
		return Result{}, err
	}

	maxIterations := ec.Params.IntOr("maxIterations", DefaultAgentIterations)
	if maxIterations < 1 {
		maxIterations = 1
	}

	req := llm.Request{
		Model:     ec.Params.StringOr("model", llm.DefaultModel),
		System:    ec.Params.StringOr("system", ""),
		Messages:  []llm.Message{llm.UserText(task)},
		MaxTokens: ec.Params.IntOr("maxTokens", llm.DefaultMaxTokens),
		Effort:    llm.Effort(ec.Params.StringOr("effort", string(llm.EffortHigh))),
		// Tool-calling is reasoning work, so thinking is on and not configurable
		// here: turning it off makes a model more likely to describe a tool call
		// in prose instead of making one.
		Thinking: true,
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, llm.ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			Schema:      t.Schema,
		})
	}

	var (
		transcript []agentTurn
		total      llm.Usage
		final      llm.Response
	)

	for turn := 1; ; turn++ {
		// Cancellation is checked between turns rather than mid-call, so an
		// in-flight request is allowed to finish and be billed once, not twice.
		if err := ec.Ctx.Err(); err != nil {
			return Result{}, domain.Errorf(domain.ErrCodeCancelled,
				"the agent was stopped after %d turn(s)", turn-1)
		}

		resp, err := provider.Chat(ec.Ctx, req)
		if err != nil {
			return Result{}, domain.Errorf(domain.ErrCodeLLM, "turn %d: %s", turn, err.Error())
		}
		total.Add(resp.Usage)
		final = resp

		if resp.StopReason == llm.StopRefusal {
			return Result{}, domain.Errorf(domain.ErrCodeLLMRefusal,
				"the model declined this request on turn %d: %s", turn, resp.Refusal)
		}

		record := agentTurn{Turn: turn, Text: resp.Text, Thinking: resp.Thinking, Usage: resp.Usage}

		if resp.StopReason != llm.StopToolUse || len(resp.ToolCalls) == 0 {
			transcript = append(transcript, record)
			break
		}

		if turn >= maxIterations {
			transcript = append(transcript, record)
			return Result{}, domain.Errorf(domain.ErrCodeAgentBudget,
				"the agent still wanted to call tools after %d rounds; raise the limit or narrow the task",
				maxIterations)
		}

		// The assistant turn must go back before its results, or the provider
		// rejects the history.
		req.Messages = append(req.Messages, resp.AsAssistantMessage())

		results := make([]llm.ToolResult, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			logged, result := runAgentTool(ec, tools, call)
			record.ToolCalls = append(record.ToolCalls, logged)
			results = append(results, result)
		}
		transcript = append(transcript, record)

		// Every result goes back in one user turn: splitting them teaches the
		// model to stop asking for tools in parallel.
		req.Messages = append(req.Messages, llm.Message{Role: llm.RoleUser, Results: results})
	}

	out := map[string]any{}
	for k, v := range ec.Item.JSON {
		out[k] = v
	}
	out["text"] = final.Text

	if ec.Params.BoolOr("includeTranscript", true) {
		out["_agent"] = map[string]any{
			"turns":      transcript,
			"toolCalls":  countToolCalls(transcript),
			"model":      final.Model,
			"stopReason": string(final.StopReason),
			"usage":      total,
		}
	}

	return Main(domain.NewItem(out)), nil
}

// runAgentTool invokes one tool and shapes both the transcript entry and the
// result the model sees.
//
// A tool failure is reported back to the model rather than failing the node: a
// wrong argument is something the model can correct on the next turn, and
// killing the run would waste everything it had already worked out.
func runAgentTool(ec ExecContext, tools []ToolBinding, call llm.ToolCall) (agentToolCall, llm.ToolResult) {
	logged := agentToolCall{Tool: call.Name, Args: call.Input}

	binding, ok := findTool(tools, call.Name)
	if !ok {
		// The model invented a tool name. Telling it which ones exist is far
		// more useful than a bare "unknown tool".
		msg := fmt.Sprintf("There is no tool called %q. Available tools: %s.",
			call.Name, strings.Join(toolNames(tools), ", "))
		logged.Error = msg
		return logged, llm.ToolResult{CallID: call.ID, Content: msg, IsError: true}
	}

	items, err := binding.Invoke(ec.Ctx, call.Input)
	if err != nil {
		logged.Error = err.Error()
		return logged, llm.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("The tool failed: %s", err.Error()),
			IsError: true,
		}
	}

	logged.Items = len(items)

	payload := make([]map[string]any, 0, len(items))
	for _, it := range items {
		payload = append(payload, it.JSON)
	}

	// One item is the common case, and handing the model a bare object rather
	// than a one-element array keeps its follow-up reasoning simpler.
	var content any = payload
	if len(payload) == 1 {
		content = payload[0]
	} else if len(payload) == 0 {
		content = map[string]any{}
	}
	logged.Result = content

	encoded, err := json.Marshal(content)
	if err != nil {
		msg := fmt.Sprintf("The tool returned something that could not be encoded: %s", err.Error())
		logged.Error = msg
		return logged, llm.ToolResult{CallID: call.ID, Content: msg, IsError: true}
	}

	return logged, llm.ToolResult{CallID: call.ID, Content: string(encoded)}
}

// agentTools merges the engine's bindings with the descriptions and schemas the
// author wrote, and refuses a configuration the model could not use.
func agentTools(ec ExecContext) ([]ToolBinding, error) {
	if len(ec.Tools) == 0 {
		return nil, nil
	}

	descriptions, err := ec.Params.KeyValues("toolDescriptions")
	if err != nil {
		return nil, err
	}
	byName := make(map[string]string, len(descriptions))
	for _, row := range descriptions {
		byName[row.Key] = row.Value
	}

	schemas, err := jsonObjectParam(ec, "toolSchemas")
	if err != nil {
		return nil, err
	}

	out := make([]ToolBinding, 0, len(ec.Tools))
	var undescribed []string

	for _, t := range ec.Tools {
		binding := t
		if desc, ok := byName[t.Name]; ok && strings.TrimSpace(desc) != "" {
			binding.Description = desc
		}
		if binding.Description == "" {
			undescribed = append(undescribed, t.Name)
		}
		if raw, ok := schemas[t.Name]; ok {
			schema, ok := raw.(map[string]any)
			if !ok {
				return nil, domain.Errorf(domain.ErrCodeValidation,
					"the schema for tool %q must be a JSON object", t.Name)
			}
			binding.Schema = schema
		}
		out = append(out, binding)
	}

	if len(undescribed) > 0 {
		// A tool the model cannot tell apart from the others will simply not be
		// used, and that is a silent failure worth refusing up front.
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"these tools need a description before the model can choose them: %s",
			strings.Join(undescribed, ", "))
	}
	return out, nil
}

func findTool(tools []ToolBinding, name string) (ToolBinding, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return ToolBinding{}, false
}

func toolNames(tools []ToolBinding) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func countToolCalls(turns []agentTurn) int {
	n := 0
	for _, t := range turns {
		n += len(t.ToolCalls)
	}
	return n
}

// Compile-time assertion that both AI nodes satisfy the interface.
var (
	_ Node = LLMChat{}
	_ Node = AIAgent{}
)
