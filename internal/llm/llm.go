// Package llm is the seam between the platform and a language-model provider.
//
// Nodes talk to this interface, never to a vendor SDK, so adding a second
// provider is one file here rather than a change to every AI node. The shapes
// are deliberately the small common subset — messages, tool specs, tool calls,
// usage — rather than a passthrough of any one vendor's request type.
package llm

import (
	"context"
	"fmt"
)

// Role is who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn of a conversation. A turn either carries text, or the
// tool calls the model asked for, or the results of those calls — the three are
// kept in one type because that is how a provider's history is ordered.
type Message struct {
	Role Role `json:"role"`

	Text      string       `json:"text,omitempty"`
	ToolCalls []ToolCall   `json:"toolCalls,omitempty"`
	Results   []ToolResult `json:"toolResults,omitempty"`
}

// UserText builds a plain user turn.
func UserText(text string) Message {
	return Message{Role: RoleUser, Text: text}
}

// ToolSpec describes a tool the model may call. Schema is a JSON Schema object;
// an empty Schema means "an object with any properties", which is the right
// default for a tool whose author has not written one.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema,omitempty"`
}

// SchemaOrPermissive returns the tool's schema, or one that accepts any object.
func (t ToolSpec) SchemaOrPermissive() map[string]any {
	if len(t.Schema) > 0 {
		return t.Schema
	}
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": true,
	}
}

// ToolCall is the model asking for a tool to run.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResult is what the platform hands back for one call.
type ToolResult struct {
	CallID  string `json:"callId"`
	Content string `json:"content"`
	IsError bool   `json:"isError,omitempty"`
}

// Effort trades cost against thinking depth. It maps onto the provider's own
// effort control; a provider that has none ignores it.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// Valid reports whether e is one of the known levels.
func (e Effort) Valid() bool {
	switch e {
	case EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	}
	return false
}

// OutputFormat asks the provider to constrain the response shape.
type OutputFormat struct {
	// JSONSchema, when set, requires the reply to be JSON matching it.
	JSONSchema map[string]any `json:"jsonSchema,omitempty"`
}

// Request is one call to a model.
type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolSpec
	MaxTokens int
	Effort    Effort

	// Thinking asks for the provider's reasoning mode. It is a plain bool
	// because the platform has no use for a token budget: the current models
	// decide depth themselves, and Effort is the dial that matters.
	Thinking bool

	Output *OutputFormat
}

// StopReason is why the model stopped, normalised across providers.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopRefusal   StopReason = "refusal"
	StopOther     StopReason = "other"
)

// Usage is the token accounting of one call.
type Usage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
}

// Add accumulates another call's usage, for reporting a whole agent run.
func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.CacheWriteTokens += other.CacheWriteTokens
}

// Response is one reply from a model.
type Response struct {
	Text       string     `json:"text,omitempty"`
	Thinking   string     `json:"thinking,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	StopReason StopReason `json:"stopReason"`
	Usage      Usage      `json:"usage"`
	Model      string     `json:"model,omitempty"`

	// Refusal explains a StopRefusal, so a node can report why rather than
	// leaving the user with an empty answer.
	Refusal string `json:"refusal,omitempty"`
}

// AsAssistantMessage converts a reply into the history turn that must be sent
// back on the next request.
func (r Response) AsAssistantMessage() Message {
	return Message{Role: RoleAssistant, Text: r.Text, ToolCalls: r.ToolCalls}
}

// Provider is a language-model backend.
type Provider interface {
	// Name identifies the provider in logs and error messages.
	Name() string

	// Models lists the model ids this provider accepts, best first. The editor
	// offers them, so an empty list means the node's model field is free text.
	Models() []string

	// Chat sends one request. A provider must not retry: the engine owns retry
	// policy, and a node's settings decide it.
	Chat(ctx context.Context, req Request) (Response, error)
}

// ErrNoProvider is returned when an AI node runs with no provider configured,
// which in practice means the API key is missing.
type ErrNoProvider struct{ Reason string }

func (e *ErrNoProvider) Error() string {
	if e.Reason == "" {
		return "no language-model provider is configured"
	}
	return "no language-model provider is configured: " + e.Reason
}

// Registry holds the providers a deployment has configured.
type Registry struct {
	byName   map[string]Provider
	order    []string
	fallback string
}

// NewRegistry builds a registry. The first provider given is the default.
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{byName: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		if p == nil {
			continue
		}
		name := p.Name()
		if _, seen := r.byName[name]; !seen {
			r.order = append(r.order, name)
		}
		r.byName[name] = p
		if r.fallback == "" {
			r.fallback = name
		}
	}
	return r
}

// Default returns the provider a node uses when it names none.
func (r *Registry) Default() (Provider, error) {
	if r == nil || r.fallback == "" {
		return nil, &ErrNoProvider{Reason: "set ANTHROPIC_API_KEY to enable the AI nodes"}
	}
	return r.byName[r.fallback], nil
}

// Get looks a provider up by name, falling back to the default for an empty
// name so a node that does not care still works.
func (r *Registry) Get(name string) (Provider, error) {
	if name == "" {
		return r.Default()
	}
	if r == nil {
		return nil, &ErrNoProvider{}
	}
	p, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("unknown language-model provider %q", name)
	}
	return p, nil
}

// Names lists the configured providers in registration order.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Empty reports whether no provider is configured, which the node descriptors
// use to explain themselves rather than failing at run time.
func (r *Registry) Empty() bool {
	return r == nil || len(r.order) == 0
}
