package nodes

import (
	"encoding/json"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/llm"
)

// LLMChat sends one prompt to a model and passes the reply on. It is the
// workhorse AI node: classify, summarise, extract, rewrite. Anything that needs
// the model to reach for a tool belongs in the Agent node instead.
type LLMChat struct{}

// Descriptor implements Node.
func (LLMChat) Descriptor() Descriptor {
	return Descriptor{
		Type:        "llm.chat",
		Name:        "LLM",
		Category:    CategoryAI,
		Description: "Sends a prompt to a language model and returns its reply.",
		Icon:        "sparkle",
		Mode:        ModePerItem,
		Inputs:      MainIn,
		Outputs:     MainOut,
		Credential:  "anthropicApi",
		// Phase 2 reads the key from the environment, so a workspace credential
		// is an upgrade rather than a prerequisite.
		CredentialOptional: true,
		Params: []ParamSpec{
			{
				Name:  "model",
				Label: "Model",
				Type:  ParamSelect,
				// The list is the provider's, kept in one place in internal/llm.
				Options:     modelOptions(),
				Default:     llm.DefaultModel,
				Description: "Which model answers. Opus is the most capable; Haiku is the cheapest.",
			},
			{
				Name:               "system",
				Label:              "System prompt",
				Type:               ParamString,
				SupportsExpression: true,
				Placeholder:        "You classify support tickets by urgency.",
				Description:        "Standing instructions. Kept out of the per-item prompt so it stays cacheable.",
			},
			{
				Name:               "prompt",
				Label:              "Prompt",
				Type:               ParamCode,
				Required:           true,
				SupportsExpression: true,
				Placeholder:        "Classify this ticket:\n\n{{ $json.subject }}\n{{ $json.body }}",
				Description:        "The message for this item. Use {{ }} to pull fields in.",
			},
			{
				Name:    "outputFormat",
				Label:   "Reply format",
				Type:    ParamSelect,
				Default: "text",
				Options: []ParamOption{
					{Label: "Text", Value: "text"},
					{Label: "JSON matching a schema", Value: "json"},
				},
				Description: "JSON constrains the reply to a schema, so downstream nodes can rely on its shape.",
			},
			{
				Name:        "jsonSchema",
				Label:       "Reply schema",
				Type:        ParamJSON,
				ShowWhen:    &ShowWhen{Param: "outputFormat", Equals: []any{"json"}},
				Placeholder: `{"type":"object","properties":{"urgency":{"type":"string"}},"required":["urgency"]}`,
				Description: "A JSON Schema. The parsed reply is merged into the output item.",
			},
			{
				Name:        "thinking",
				Label:       "Let the model think",
				Type:        ParamBoolean,
				Default:     true,
				Description: "Reasoning before answering. Worth leaving on for anything but the simplest classification.",
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
					{Label: "Extra high", Value: string(llm.EffortXHigh)},
					{Label: "Max — correctness over cost", Value: string(llm.EffortMax)},
				},
				Description: "How much thinking and token spend to allow.",
			},
			{
				Name:        "maxTokens",
				Label:       "Max reply tokens",
				Type:        ParamNumber,
				Default:     llm.DefaultMaxTokens,
				Description: "A ceiling on the reply. Too low truncates it mid-sentence.",
			},
			{
				Name:        "includeUsage",
				Label:       "Include token usage",
				Type:        ParamBoolean,
				Default:     false,
				Description: "Adds an _llm field with the model, token counts and stop reason.",
			},
		},
	}
}

// Execute implements Node.
func (LLMChat) Execute(ec ExecContext) (Result, error) {
	provider, err := ec.LLM.Default()
	if err != nil {
		return Result{}, domain.Errorf(domain.ErrCodeValidation, "%s", err.Error())
	}

	prompt, err := ec.Params.String("prompt")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(prompt) == "" {
		return Result{}, ErrRequiredParam("prompt")
	}

	req := llm.Request{
		Model:     ec.Params.StringOr("model", llm.DefaultModel),
		System:    ec.Params.StringOr("system", ""),
		Messages:  []llm.Message{llm.UserText(prompt)},
		MaxTokens: ec.Params.IntOr("maxTokens", llm.DefaultMaxTokens),
		Effort:    llm.Effort(ec.Params.StringOr("effort", string(llm.EffortHigh))),
		Thinking:  ec.Params.BoolOr("thinking", true),
	}

	wantJSON := ec.Params.StringOr("outputFormat", "text") == "json"
	if wantJSON {
		schema, err := jsonObjectParam(ec, "jsonSchema")
		if err != nil {
			return Result{}, err
		}
		if len(schema) == 0 {
			return Result{}, domain.Errorf(domain.ErrCodeValidation,
				"a reply schema is required when the reply format is JSON")
		}
		req.Output = &llm.OutputFormat{JSONSchema: schema}
	}

	resp, err := provider.Chat(ec.Ctx, req)
	if err != nil {
		return Result{}, domain.Errorf(domain.ErrCodeLLM, "%s", err.Error())
	}
	if resp.StopReason == llm.StopRefusal {
		return Result{}, domain.Errorf(domain.ErrCodeLLMRefusal,
			"the model declined this request: %s", resp.Refusal)
	}

	out := map[string]any{}
	// Merging onto the input item keeps the record intact, which is almost
	// always what a workflow wants: the classification next to what it classified.
	for k, v := range ec.Item.JSON {
		out[k] = v
	}

	if wantJSON {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(resp.Text), &parsed); err != nil {
			return Result{}, domain.Errorf(domain.ErrCodeLLM,
				"the model's reply was not the JSON the schema asked for: %s", truncate(resp.Text, 200))
		}
		for k, v := range parsed {
			out[k] = v
		}
	} else {
		out["text"] = resp.Text
	}

	if ec.Params.BoolOr("includeUsage", false) {
		out["_llm"] = map[string]any{
			"model":        resp.Model,
			"stopReason":   string(resp.StopReason),
			"inputTokens":  resp.Usage.InputTokens,
			"outputTokens": resp.Usage.OutputTokens,
		}
	}

	// A truncated reply is a silent data-quality problem, so it is worth a line
	// in the log even though the node succeeds.
	if resp.StopReason == llm.StopMaxTokens {
		ec.Log().Warn("llm.chat: reply hit the token ceiling and was truncated",
			"node", ec.Node.Name, "maxTokens", req.MaxTokens)
	}

	return Main(domain.NewItem(out)), nil
}

// modelOptions turns the provider's model list into select options.
func modelOptions() []ParamOption {
	labels := map[string]string{
		llm.ModelOpus5:   "Claude Opus 5 — most capable",
		llm.ModelSonnet5: "Claude Sonnet 5 — balanced",
		llm.ModelHaiku45: "Claude Haiku 4.5 — fastest and cheapest",
	}
	ids := []string{llm.ModelOpus5, llm.ModelSonnet5, llm.ModelHaiku45}

	out := make([]ParamOption, 0, len(ids))
	for _, id := range ids {
		label := labels[id]
		if label == "" {
			label = id
		}
		out = append(out, ParamOption{Label: label, Value: id})
	}
	return out
}

// jsonObjectParam reads a json parameter as an object, tolerating both a real
// object and the string a textarea produces.
func jsonObjectParam(ec ExecContext, name string) (map[string]any, error) {
	raw, err := ec.Params.Raw(name)
	if err != nil {
		return nil, err
	}

	switch typed := raw.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return typed, nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil, nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
			return nil, domain.Errorf(domain.ErrCodeValidation,
				"%s is not valid JSON: %s", name, err.Error())
		}
		return out, nil
	default:
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"%s must be a JSON object", name)
	}
}
