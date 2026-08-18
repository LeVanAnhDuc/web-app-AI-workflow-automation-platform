package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Model ids this deployment offers. The editor's model dropdown is built from
// this list, so it is the one place to change when a new model ships.
const (
	ModelOpus5   = "claude-opus-5"
	ModelSonnet5 = "claude-sonnet-5"
	ModelHaiku45 = "claude-haiku-4-5"
)

// DefaultModel is what an AI node uses when its model field is empty.
const DefaultModel = ModelOpus5

// DefaultMaxTokens is a deliberate compromise: high enough that a normal answer
// is never truncated mid-sentence, low enough to stay under the SDK's
// non-streaming HTTP timeout.
const DefaultMaxTokens = 16000

// Anthropic is a Provider backed by the official Anthropic SDK.
type Anthropic struct {
	client anthropic.Client
}

// NewAnthropic builds a provider. An empty key is an error rather than a client
// that fails on first use, so a misconfigured deployment is visible at boot.
func NewAnthropic(apiKey string) (*Anthropic, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, &ErrNoProvider{Reason: "ANTHROPIC_API_KEY is empty"}
	}
	return &Anthropic{client: anthropic.NewClient(option.WithAPIKey(apiKey))}, nil
}

// Name identifies this provider in node config and logs.
func (a *Anthropic) Name() string { return "anthropic" }

// Models lists the ids the editor offers, most capable first.
func (a *Anthropic) Models() []string {
	return []string{ModelOpus5, ModelSonnet5, ModelHaiku45}
}

// Chat sends one request and normalises the reply.
func (a *Anthropic) Chat(ctx context.Context, req Request) (Response, error) {
	params, err := a.params(req)
	if err != nil {
		return Response{}, err
	}

	msg, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return Response{}, translateError(err)
	}
	return translateResponse(msg), nil
}

func (a *Anthropic) params(req Request) (anthropic.MessageNewParams, error) {
	model := req.Model
	if model == "" {
		model = DefaultModel
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	messages, err := toSDKMessages(req.Messages)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}

	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	// Adaptive is the only thinking mode the current models take; the older
	// fixed token budget is gone, and Effort is what tunes depth.
	if req.Thinking {
		adaptive := anthropic.ThinkingConfigAdaptiveParam{}
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
	}

	if req.Effort != "" {
		if !req.Effort.Valid() {
			return anthropic.MessageNewParams{}, fmt.Errorf("unknown effort %q", req.Effort)
		}
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(req.Effort)
	}

	if len(req.Tools) > 0 {
		params.Tools = make([]anthropic.ToolUnionParam, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.SchemaOrPermissive()
			tool := anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: schema["properties"],
				},
			}
			// required and additionalProperties are not first-class fields on
			// the SDK's schema param, so they ride along as extra fields.
			extra := map[string]any{}
			if required, ok := schema["required"]; ok {
				extra["required"] = required
			}
			if ap, ok := schema["additionalProperties"]; ok {
				extra["additionalProperties"] = ap
			}
			if len(extra) > 0 {
				tool.InputSchema.ExtraFields = extra
			}
			params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &tool})
		}
	}

	if req.Output != nil && len(req.Output.JSONSchema) > 0 {
		format, err := jsonOutputFormat(req.Output.JSONSchema)
		if err != nil {
			return anthropic.MessageNewParams{}, err
		}
		params.OutputConfig.Format = format
	}

	return params, nil
}

// jsonOutputFormat builds the structured-output config. The schema is
// round-tripped through JSON first: it arrives from a node's parameter as
// whatever the author typed, and this normalises it to the plain maps and
// float64s the SDK marshals cleanly.
func jsonOutputFormat(schema map[string]any) (anthropic.JSONOutputFormatParam, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return anthropic.JSONOutputFormatParam{}, fmt.Errorf("marshal output schema: %w", err)
	}
	var normalised map[string]any
	if err := json.Unmarshal(raw, &normalised); err != nil {
		return anthropic.JSONOutputFormatParam{}, fmt.Errorf("normalise output schema: %w", err)
	}
	return anthropic.JSONOutputFormatParam{Schema: normalised}, nil
}

func toSDKMessages(in []Message) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(in))

	for _, m := range in {
		switch m.Role {
		case RoleAssistant:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(m.ToolCalls))
			if m.Text != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Text))
			}
			for _, call := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(call.ID, call.Input, call.Name))
			}
			if len(blocks) == 0 {
				// An assistant turn with nothing in it is not a valid history
				// entry, and sending one produces a confusing 400.
				continue
			}
			out = append(out, anthropic.NewAssistantMessage(blocks...))

		case RoleUser, "":
			blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(m.Results))
			for _, r := range m.Results {
				blocks = append(blocks, anthropic.NewToolResultBlock(r.CallID, r.Content, r.IsError))
			}
			if m.Text != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Text))
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropic.NewUserMessage(blocks...))

		default:
			return nil, fmt.Errorf("unknown message role %q", m.Role)
		}
	}

	if len(out) == 0 {
		return nil, errors.New("a request needs at least one message")
	}
	return out, nil
}

func translateResponse(msg *anthropic.Message) Response {
	out := Response{
		Model:      string(msg.Model),
		StopReason: translateStopReason(msg.StopReason),
		Usage: Usage{
			InputTokens:      int(msg.Usage.InputTokens),
			OutputTokens:     int(msg.Usage.OutputTokens),
			CacheReadTokens:  int(msg.Usage.CacheReadInputTokens),
			CacheWriteTokens: int(msg.Usage.CacheCreationInputTokens),
		},
	}

	var text, thinking strings.Builder
	for _, block := range msg.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			text.WriteString(variant.Text)
		case anthropic.ThinkingBlock:
			thinking.WriteString(variant.Thinking)
		case anthropic.ToolUseBlock:
			call := ToolCall{ID: variant.ID, Name: variant.Name}
			// Input is raw JSON, and the current models vary their escaping, so
			// it must be parsed rather than string-matched.
			if raw := variant.JSON.Input.Raw(); raw != "" {
				if err := json.Unmarshal([]byte(raw), &call.Input); err != nil {
					call.Input = map[string]any{"_unparsed": raw}
				}
			}
			out.ToolCalls = append(out.ToolCalls, call)
		}
	}
	out.Text = text.String()
	out.Thinking = thinking.String()

	// stop_details is populated only for a refusal, so it must be read behind
	// that check rather than unconditionally.
	if out.StopReason == StopRefusal {
		out.Refusal = strings.TrimSpace(
			string(msg.StopDetails.Category) + " " + msg.StopDetails.Explanation)
		if out.Refusal == "" {
			out.Refusal = "the model declined this request"
		}
	}

	return out
}

func translateStopReason(r anthropic.StopReason) StopReason {
	switch r {
	case anthropic.StopReasonEndTurn:
		return StopEndTurn
	case anthropic.StopReasonToolUse:
		return StopToolUse
	case anthropic.StopReasonMaxTokens:
		return StopMaxTokens
	case anthropic.StopReasonRefusal:
		return StopRefusal
	default:
		return StopOther
	}
}

// translateError turns an SDK error into something a workflow author can act
// on. The status code is what distinguishes "your key is wrong" from "slow
// down", and both read very differently in an execution log.
func translateError(err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("anthropic request failed: %w", err)
	}

	switch apiErr.StatusCode {
	case 401, 403:
		return fmt.Errorf("anthropic rejected the credentials (HTTP %d): check ANTHROPIC_API_KEY", apiErr.StatusCode)
	case 404:
		return fmt.Errorf("anthropic does not know that model (HTTP 404): %s", apiErr.Error())
	case 429:
		return fmt.Errorf("anthropic rate limit reached (HTTP 429): %s", apiErr.Error())
	case 400:
		return fmt.Errorf("anthropic rejected the request (HTTP 400): %s", apiErr.Error())
	default:
		if apiErr.StatusCode >= 500 {
			return fmt.Errorf("anthropic is unavailable (HTTP %d): %s", apiErr.StatusCode, apiErr.Error())
		}
		return fmt.Errorf("anthropic request failed (HTTP %d): %s", apiErr.StatusCode, apiErr.Error())
	}
}
