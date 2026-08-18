// Package nodes defines what a node is and holds one implementation per node
// type. The descriptor is the contract the frontend renders its config panel
// from, so adding a node is a Go-only change.
package nodes

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/llm"
)

// ParamType is the editor a parameter renders as in the config drawer.
type ParamType string

const (
	ParamString   ParamType = "string"
	ParamNumber   ParamType = "number"
	ParamBoolean  ParamType = "boolean"
	ParamSelect   ParamType = "select"
	ParamJSON     ParamType = "json"
	ParamCode     ParamType = "code"
	ParamKeyValue ParamType = "keyValue"
	ParamNotice   ParamType = "notice"
	ParamFilter   ParamType = "filter"
)

// ParamOption is one choice of a select parameter.
type ParamOption struct {
	Label string `json:"Label"`
	Value string `json:"Value"`
}

// ShowWhen makes a parameter visible only when another parameter has one of
// the listed values. The frontend evaluates this; the engine ignores it.
type ShowWhen struct {
	Param  string `json:"Param"`
	Equals []any  `json:"Equals"`
}

// ParamSpec describes one configurable parameter of a node.
type ParamSpec struct {
	Name               string        `json:"Name"`
	Label              string        `json:"Label"`
	Type               ParamType     `json:"Type"`
	Default            any           `json:"Default,omitempty"`
	Required           bool          `json:"Required,omitempty"`
	Options            []ParamOption `json:"Options,omitempty"`
	Placeholder        string        `json:"Placeholder,omitempty"`
	Description        string        `json:"Description,omitempty"`
	SupportsExpression bool          `json:"SupportsExpression,omitempty"`
	ShowWhen           *ShowWhen     `json:"ShowWhen,omitempty"`
}

// Handle is a named input or output socket on a node.
type Handle struct {
	Name  string `json:"Name"`
	Label string `json:"Label"`
}

// Well-known handle names.
const (
	HandleMain   = domain.MainHandle
	HandleTrue   = "true"
	HandleFalse  = "false"
	HandleInput1 = "input1"
	HandleInput2 = "input2"

	// HandleTool is the second input of the AI Agent node. Nodes wired into it
	// become tools the model may call, which keeps that relationship visible on
	// the canvas instead of hidden in a config field.
	HandleTool = "tool"
)

// MainIn and MainOut are the single-handle shapes most nodes use.
var (
	MainIn  = []Handle{{Name: HandleMain, Label: "Input"}}
	MainOut = []Handle{{Name: HandleMain, Label: "Output"}}
)

// ExecMode says whether the engine loops the node over each input item or
// hands it the whole set once.
type ExecMode string

const (
	ModePerItem ExecMode = "perItem"
	ModeOnce    ExecMode = "once"
)

// Node categories, matching the palette groups in the UI.
const (
	CategoryTrigger = "Triggers"
	CategoryCore    = "Core"
	CategoryFlow    = "Flow"
	CategoryAI      = "AI"
)

// Descriptor is everything the API exposes about a node type. The frontend
// renders the config drawer, the palette and the node picker from it.
type Descriptor struct {
	Type        string      `json:"Type"`
	Name        string      `json:"Name"`
	Category    string      `json:"Category"`
	Description string      `json:"Description"`
	Icon        string      `json:"Icon"`
	Mode        ExecMode    `json:"Mode"`
	Inputs      []Handle    `json:"Inputs"`
	Outputs     []Handle    `json:"Outputs"`
	Params      []ParamSpec `json:"Params"`
	Credential  string      `json:"Credential,omitempty"`
	IsTrigger   bool        `json:"IsTrigger,omitempty"`
}

// KeyValue is one row of a keyValue parameter, already expression-resolved.
type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Condition is one row of a filter parameter, already expression-resolved.
type Condition struct {
	Left     any    `json:"left"`
	Operator string `json:"operator"`
	Right    any    `json:"right"`
}

// ParamResolver reads a node's parameters with expressions already evaluated
// against the current item. The engine supplies the implementation; nodes only
// consume it. The `…Or` accessors swallow errors and fall back, for optional
// parameters; the plain accessors report a problem the run should fail on.
type ParamResolver interface {
	String(name string) (string, error)
	Int(name string) (int, error)
	Float(name string) (float64, error)
	Bool(name string) (bool, error)
	Raw(name string) (any, error)
	KeyValues(name string) ([]KeyValue, error)
	Conditions(name string) ([]Condition, error)

	StringOr(name, def string) string
	IntOr(name string, def int) int
	BoolOr(name string, def bool) bool

	// Literal returns the parameter exactly as stored, with no expression
	// evaluation. Used by the code node, whose body must not be interpolated.
	Literal(name string) any
}

// TriggerPayload is the data that started the execution, available to trigger
// nodes so they can emit it.
type TriggerPayload struct {
	Type  domain.TriggerType
	Items []domain.Item
}

// ExecContext is everything a node gets for one call.
type ExecContext struct {
	Ctx    context.Context
	Node   domain.GraphNode
	Params ParamResolver

	// Items is the input on the main handle. Inputs holds every input handle,
	// which only multi-input nodes such as merge need.
	Items  []domain.Item
	Inputs map[string][]domain.Item

	// Item and ItemIndex are set when the descriptor's Mode is perItem.
	Item      domain.Item
	ItemIndex int

	// NodeOutputs is keyed by node name, then output handle, for expressions
	// and for nodes that reach back at earlier results.
	NodeOutputs map[string]map[string][]domain.Item

	// Tools are the nodes wired into this node's tool handle, already prepared
	// for invocation. Only the AI Agent node reads them; every other node sees
	// an empty slice.
	Tools []ToolBinding

	// LLM is the provider registry the AI nodes call through. It is nil when the
	// deployment has configured none, and those nodes report that rather than
	// failing obscurely.
	LLM *llm.Registry

	Trigger     TriggerPayload
	ExecutionID string
	HTTPClient  *http.Client
	Logger      *slog.Logger
}

// ToolBinding is one node offered to a model as a callable tool. The engine
// builds these: Invoke runs the target node through the same retry, timeout and
// persistence path as any other node, so a tool call is as inspectable as a
// step of the main flow.
type ToolBinding struct {
	// Name is the node's name, which is what the model sees and calls.
	Name string

	// Description tells the model when to reach for this tool. It comes from the
	// agent's own configuration, where the prompt author is already working.
	Description string

	// Schema is a JSON Schema for the arguments. An empty schema means the tool
	// accepts any object, which is the honest default when nobody wrote one.
	Schema map[string]any

	// Invoke runs the tool. The arguments arrive as one item; the tool's main
	// output comes back. An error is reported to the model as a failed tool
	// result rather than ending the run, so it can correct itself.
	Invoke func(ctx context.Context, args map[string]any) ([]domain.Item, error)
}

// ToolByName finds a binding, so a node need not scan the slice itself.
func (ec ExecContext) ToolByName(name string) (ToolBinding, bool) {
	for _, t := range ec.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return ToolBinding{}, false
}

// ToolSpecs is what the LLM layer needs to advertise this node's tools.
func (ec ExecContext) ToolSpecs() []llm.ToolSpec {
	out := make([]llm.ToolSpec, 0, len(ec.Tools))
	for _, t := range ec.Tools {
		out = append(out, llm.ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			Schema:      t.Schema,
		})
	}
	return out
}

// ToolNames lists the bound tool names, for log lines and error messages.
func (ec ExecContext) ToolNames() []string {
	out := make([]string, 0, len(ec.Tools))
	for _, t := range ec.Tools {
		out = append(out, t.Name)
	}
	return out
}

// Log returns a logger that never panics on a zero context.
func (ec ExecContext) Log() *slog.Logger {
	if ec.Logger == nil {
		return slog.Default()
	}
	return ec.Logger
}

// Client returns the HTTP client to use, defaulting to the shared one.
func (ec ExecContext) Client() *http.Client {
	if ec.HTTPClient == nil {
		return http.DefaultClient
	}
	return ec.HTTPClient
}

// Result is what a node returns: items per output handle. A single-output node
// uses Main; a branching node fills several handles.
type Result struct {
	Outputs map[string][]domain.Item
}

// Main builds a result carrying items on the main handle.
func Main(items ...domain.Item) Result {
	return Result{Outputs: map[string][]domain.Item{HandleMain: items}}
}

// MainSlice is Main for an already-built slice.
func MainSlice(items []domain.Item) Result {
	return Result{Outputs: map[string][]domain.Item{HandleMain: items}}
}

// Empty is a result with no items on any handle.
func Empty() Result {
	return Result{Outputs: map[string][]domain.Item{HandleMain: nil}}
}

// Get returns the items on one handle.
func (r Result) Get(handle string) []domain.Item {
	if r.Outputs == nil {
		return nil
	}
	return r.Outputs[handle]
}

// Node is one node type: a descriptor plus the work it does.
type Node interface {
	Descriptor() Descriptor
	Execute(ec ExecContext) (Result, error)
}

// Registry maps node types to implementations and preserves registration
// order so GET /node-types returns a stable palette.
type Registry struct {
	byType map[string]Node
	order  []string
}

// NewRegistry builds a registry from the given nodes.
func NewRegistry(ns ...Node) *Registry {
	r := &Registry{byType: make(map[string]Node, len(ns))}
	for _, n := range ns {
		r.Register(n)
	}
	return r
}

// Register adds or replaces a node type.
func (r *Registry) Register(n Node) {
	t := n.Descriptor().Type
	if _, exists := r.byType[t]; !exists {
		r.order = append(r.order, t)
	}
	r.byType[t] = n
}

// Get looks a node type up.
func (r *Registry) Get(nodeType string) (Node, bool) {
	n, ok := r.byType[nodeType]
	return n, ok
}

// MustGet is Get for callers that treat a missing type as a programming error.
func (r *Registry) MustGet(nodeType string) Node {
	n, ok := r.Get(nodeType)
	if !ok {
		panic(fmt.Sprintf("nodes: unknown node type %q", nodeType))
	}
	return n
}

// Descriptors returns every descriptor in registration order.
func (r *Registry) Descriptors() []Descriptor {
	out := make([]Descriptor, 0, len(r.order))
	for _, t := range r.order {
		out = append(out, r.byType[t].Descriptor())
	}
	return out
}

// Types returns every registered node type in registration order.
func (r *Registry) Types() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// ErrUnknownParam is returned by a resolver for a parameter a node did not
// declare, which is always a bug in the node rather than in user data.
func ErrUnknownParam(name string) error {
	return domain.Errorf(domain.ErrCodeValidation, "unknown parameter %q", name)
}

// ErrRequiredParam is returned when a required parameter resolved to empty.
func ErrRequiredParam(name string) error {
	return domain.Errorf(domain.ErrCodeValidation, "parameter %q is required", name)
}
