// Package domain holds the types shared by every layer: the item that flows
// between nodes, the graph a workflow is made of, and the status vocabulary.
package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// Item is the unit of data passing between nodes. Every node receives and
// returns a slice of items; a node that conceptually handles one record still
// works on a slice of length one.
type Item struct {
	JSON map[string]any `json:"json"`
}

// NewItem wraps a map as an item, tolerating a nil map.
func NewItem(m map[string]any) Item {
	if m == nil {
		m = map[string]any{}
	}
	return Item{JSON: m}
}

// Position is a node's place on the canvas. Only the frontend interprets it.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// NodeSettings are the per-node execution controls every node type supports.
type NodeSettings struct {
	RetryOnFail        bool `json:"retryOnFail"`
	MaxTries           int  `json:"maxTries"`
	WaitBetweenTriesMs int  `json:"waitBetweenTriesMs"`
	ContinueOnFail     bool `json:"continueOnFail"`
	TimeoutMs          int  `json:"timeoutMs"`
}

// Defaults for the settings a graph omits.
const (
	DefaultMaxTries           = 3
	DefaultWaitBetweenTriesMs = 1000
	DefaultTimeoutMs          = 60000
)

// WithDefaults returns a copy with zero values replaced by the defaults.
func (s NodeSettings) WithDefaults() NodeSettings {
	if s.MaxTries <= 0 {
		s.MaxTries = DefaultMaxTries
	}
	if s.WaitBetweenTriesMs <= 0 {
		s.WaitBetweenTriesMs = DefaultWaitBetweenTriesMs
	}
	if s.TimeoutMs <= 0 {
		s.TimeoutMs = DefaultTimeoutMs
	}
	return s
}

// Tries is the number of attempts the engine should make for this node.
func (s NodeSettings) Tries() int {
	if !s.RetryOnFail {
		return 1
	}
	if s.MaxTries < 1 {
		return 1
	}
	return s.MaxTries
}

// GraphNode is one node as stored in a workflow version.
type GraphNode struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	Position     Position       `json:"position"`
	Params       map[string]any `json:"params"`
	CredentialID *string        `json:"credentialId,omitempty"`
	Settings     NodeSettings   `json:"settings"`
	Disabled     bool           `json:"disabled,omitempty"`
}

// MainHandle is the default input and output handle name.
const MainHandle = "main"

// Edge connects one node's output handle to another node's input handle.
type Edge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	SourceHandle string `json:"sourceHandle"`
	Target       string `json:"target"`
	TargetHandle string `json:"targetHandle"`
}

// SourceHandleOrMain defaults an empty handle to "main".
func (e Edge) SourceHandleOrMain() string {
	if e.SourceHandle == "" {
		return MainHandle
	}
	return e.SourceHandle
}

// TargetHandleOrMain defaults an empty handle to "main".
func (e Edge) TargetHandleOrMain() string {
	if e.TargetHandle == "" {
		return MainHandle
	}
	return e.TargetHandle
}

// Graph is the whole workflow definition stored as JSONB.
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []Edge      `json:"edges"`
}

// NodeByID finds a node by its graph id.
func (g Graph) NodeByID(id string) (GraphNode, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return GraphNode{}, false
}

// NodeByName finds a node by its display name, which expressions use.
func (g Graph) NodeByName(name string) (GraphNode, bool) {
	for _, n := range g.Nodes {
		if n.Name == name {
			return n, true
		}
	}
	return GraphNode{}, false
}

// IncomingEdges returns the edges targeting a node, in graph order.
func (g Graph) IncomingEdges(nodeID string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.Target == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// OutgoingEdges returns the edges leaving a node, in graph order.
func (g Graph) OutgoingEdges(nodeID string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.Source == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// Status is the lifecycle state of an execution or a single node execution.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
	StatusCancelled Status = "cancelled"
)

// Terminal reports whether no further transition is expected.
func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusSkipped, StatusCancelled:
		return true
	}
	return false
}

// TriggerType records what started an execution.
type TriggerType string

const (
	TriggerManual   TriggerType = "manual"
	TriggerWebhook  TriggerType = "webhook"
	TriggerSchedule TriggerType = "schedule"
)

// Error codes used across the engine and the API.
const (
	ErrCodeHTTP       = "http_error"
	ErrCodeExpression = "expression_error"
	ErrCodeScript     = "script_error"
	ErrCodeTimeout    = "timeout"
	ErrCodeValidation = "validation_error"
	ErrCodeInternal   = "internal_error"
	ErrCodeCancelled  = "cancelled"
)

// NodeError is the persisted shape of any node or execution failure. It is
// stored verbatim in node_executions.error and executions.error.
type NodeError struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Status   int            `json:"status,omitempty"`
	NodeID   string         `json:"node_id,omitempty"`
	NodeName string         `json:"node_name,omitempty"`
	Attempts int            `json:"attempts,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
}

func (e *NodeError) Error() string {
	if e == nil {
		return ""
	}
	if e.NodeName != "" {
		return fmt.Sprintf("%s at %q: %s", e.Code, e.NodeName, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Errorf builds a NodeError with a formatted message.
func Errorf(code, format string, args ...any) *NodeError {
	return &NodeError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// AsNodeError converts any error into a NodeError, preserving one if present.
func AsNodeError(err error) *NodeError {
	if err == nil {
		return nil
	}
	if ne, ok := err.(*NodeError); ok {
		return ne
	}
	return &NodeError{Code: ErrCodeInternal, Message: err.Error()}
}

// Workspace is the tenant boundary. Phase 1 seeds exactly one.
type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// User is a member of a workspace.
type User struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspaceId"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Workflow is the persisted workflow record without its graph.
type Workflow struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspaceId"`
	Name            string    `json:"name"`
	Active          bool      `json:"active"`
	ActiveVersionID *string   `json:"activeVersionId,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// WorkflowVersion is one immutable snapshot of a workflow graph.
type WorkflowVersion struct {
	ID         string    `json:"id"`
	WorkflowID string    `json:"workflowId"`
	Version    int       `json:"version"`
	Graph      Graph     `json:"graph"`
	CreatedBy  *string   `json:"createdBy,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// LastRun summarises a workflow's most recent execution for the list screen.
type LastRun struct {
	ID     string    `json:"id"`
	Status Status    `json:"status"`
	At     time.Time `json:"at"`
}

// WorkflowSummary is the row shape of the workflow list screen.
type WorkflowSummary struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Active         bool        `json:"active"`
	NodeCount      int         `json:"nodeCount"`
	TriggerType    TriggerType `json:"triggerType"`
	TriggerDetail  string      `json:"triggerDetail,omitempty"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	LastRun        *LastRun    `json:"lastRun,omitempty"`
	SuccessRate7d  *float64    `json:"successRate7d,omitempty"`
	ExecutionCount int         `json:"executionCount"`
}

// Execution is one run of one workflow version.
type Execution struct {
	ID                string      `json:"id"`
	WorkspaceID       string      `json:"workspaceId"`
	WorkflowID        string      `json:"workflowId"`
	WorkflowName      string      `json:"workflowName,omitempty"`
	WorkflowVersionID string      `json:"workflowVersionId"`
	Version           int         `json:"version,omitempty"`
	Status            Status      `json:"status"`
	TriggerType       TriggerType `json:"triggerType"`
	TriggerData       []Item      `json:"triggerData,omitempty"`
	Error             *NodeError  `json:"error,omitempty"`
	ResumeFromNode    *string     `json:"resumeFromNode,omitempty"`
	CancelRequested   bool        `json:"cancelRequested"`
	CreatedAt         time.Time   `json:"createdAt"`
	StartedAt         *time.Time  `json:"startedAt,omitempty"`
	FinishedAt        *time.Time  `json:"finishedAt,omitempty"`
}

// DurationMs is the wall-clock run time, or nil when it has not finished.
func (e Execution) DurationMs() *int64 {
	if e.StartedAt == nil || e.FinishedAt == nil {
		return nil
	}
	ms := e.FinishedAt.Sub(*e.StartedAt).Milliseconds()
	return &ms
}

// NodeExecution is the record of one node inside one execution. Outputs are
// keyed by output handle so a branching node keeps both sides.
type NodeExecution struct {
	ID          string            `json:"id"`
	ExecutionID string            `json:"executionId"`
	NodeID      string            `json:"nodeId"`
	NodeName    string            `json:"nodeName"`
	NodeType    string            `json:"nodeType"`
	Status      Status            `json:"status"`
	Attempt     int               `json:"attempt"`
	Input       []Item            `json:"input,omitempty"`
	Output      map[string][]Item `json:"output,omitempty"`
	Error       *NodeError        `json:"error,omitempty"`
	StartedAt   *time.Time        `json:"startedAt,omitempty"`
	FinishedAt  *time.Time        `json:"finishedAt,omitempty"`
}

// ItemCount is how many items the node produced in total, across every output
// handle. Summing rather than reading the main handle matters for a branching
// node such as IF, whose items leave on "true" and "false" — counting only
// "main" would report every branch as having produced nothing.
func (n NodeExecution) ItemCount() int {
	total := 0
	for _, items := range n.Output {
		total += len(items)
	}
	return total
}

// DurationMs is the node's wall-clock run time, or nil when unfinished.
func (n NodeExecution) DurationMs() *int64 {
	if n.StartedAt == nil || n.FinishedAt == nil {
		return nil
	}
	ms := n.FinishedAt.Sub(*n.StartedAt).Milliseconds()
	return &ms
}

// Webhook is a public ingress path bound to one trigger node.
type Webhook struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	WorkflowID  string    `json:"workflowId"`
	NodeID      string    `json:"nodeId"`
	Path        string    `json:"path"`
	Method      string    `json:"method"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Schedule is a cron binding for one schedule trigger node.
type Schedule struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	WorkflowID  string     `json:"workflowId"`
	NodeID      string     `json:"nodeId"`
	Cron        string     `json:"cron"`
	Timezone    string     `json:"timezone"`
	NextRunAt   time.Time  `json:"nextRunAt"`
	LastRunAt   *time.Time `json:"lastRunAt,omitempty"`
}

// Credential is an encrypted secret blob. Phase 1 stores none; the table and
// type exist so Phase 3 is a fill-in rather than a migration.
type Credential struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Type        string    `json:"type"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ItemsFromJSON decodes a JSONB items column, tolerating null.
func ItemsFromJSON(raw []byte) ([]Item, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []Item
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}
