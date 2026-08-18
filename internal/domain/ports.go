package domain

import (
	"errors"
	"time"
)

// ErrNotFound is returned by every repository lookup that finds nothing, so
// handlers can map it to 404 without knowing which table was queried.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a write loses a uniqueness race.
var ErrConflict = errors.New("conflict")

/* ---------------------------------------------------------------------------
   Filter and patch types live in domain rather than in store so that the
   engine can declare the narrow Store interface it needs and the concrete
   *store.Store satisfies it structurally, with no import between the two.
   --------------------------------------------------------------------------- */

// WorkflowSort is the ordering of the workflow list.
type WorkflowSort string

const (
	SortUpdated WorkflowSort = "updated"
	SortName    WorkflowSort = "name"
	SortCreated WorkflowSort = "created"
)

// WorkflowFilter narrows the workflow list screen.
type WorkflowFilter struct {
	Query  string       // case-insensitive name match, empty for all
	Status string       // "", "active" or "inactive"
	Sort   WorkflowSort // defaults to SortUpdated
}

// WorkflowPatch updates the mutable fields of a workflow. Nil means unchanged.
type WorkflowPatch struct {
	Name            *string
	Active          *bool
	ActiveVersionID *string
}

// NewExecution is the input of CreateExecution.
type NewExecution struct {
	WorkspaceID       string
	WorkflowID        string
	WorkflowVersionID string
	Status            Status
	TriggerType       TriggerType
	TriggerData       []Item
	ResumeFromNode    *string
}

// ExecutionFilter narrows the execution list. Cursor is the opaque value
// returned as nextCursor by the previous page.
type ExecutionFilter struct {
	WorkflowID string
	Status     Status
	Limit      int
	Cursor     string
}

// ExecutionPatch updates an execution's lifecycle fields. Nil means unchanged;
// ClearError wins over Error and blanks the column.
type ExecutionPatch struct {
	Status     *Status
	Error      *NodeError
	ClearError bool
	StartedAt  *time.Time
	FinishedAt *time.Time
}
