package api

import (
	"strings"
	"testing"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

func TestValidateGraphStructure(t *testing.T) {
	tests := []struct {
		name    string
		graph   domain.Graph
		wantErr string
	}{
		{
			name:  "empty graph is a valid draft",
			graph: domain.Graph{},
		},
		{
			name: "well-formed graph",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					{ID: "n1", Type: "trigger.manual", Name: "Start"},
					{ID: "n2", Type: "set", Name: "Shape"},
				},
				Edges: []domain.Edge{{ID: "e1", Source: "n1", Target: "n2"}},
			},
		},
		{
			name:    "missing node id",
			graph:   domain.Graph{Nodes: []domain.GraphNode{{Type: "set", Name: "A"}}},
			wantErr: "missing its id",
		},
		{
			name: "duplicate node id",
			graph: domain.Graph{Nodes: []domain.GraphNode{
				{ID: "n1", Type: "set", Name: "A"},
				{ID: "n1", Type: "set", Name: "B"},
			}},
			wantErr: "share the id",
		},
		{
			name: "duplicate node name",
			graph: domain.Graph{Nodes: []domain.GraphNode{
				{ID: "n1", Type: "set", Name: "A"},
				{ID: "n2", Type: "set", Name: "A"},
			}},
			wantErr: "names must be unique",
		},
		{
			name:    "missing name",
			graph:   domain.Graph{Nodes: []domain.GraphNode{{ID: "n1", Type: "set"}}},
			wantErr: "missing its name",
		},
		{
			name:    "missing type",
			graph:   domain.Graph{Nodes: []domain.GraphNode{{ID: "n1", Name: "A"}}},
			wantErr: "missing its type",
		},
		{
			name: "edge from an unknown node",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{{ID: "n1", Type: "set", Name: "A"}},
				Edges: []domain.Edge{{ID: "e1", Source: "ghost", Target: "n1"}},
			},
			wantErr: "starts at unknown node",
		},
		{
			name: "edge to an unknown node",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{{ID: "n1", Type: "set", Name: "A"}},
				Edges: []domain.Edge{{ID: "e1", Source: "n1", Target: "ghost"}},
			},
			wantErr: "ends at unknown node",
		},
		{
			name: "two-node cycle",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					{ID: "n1", Type: "set", Name: "A"},
					{ID: "n2", Type: "set", Name: "B"},
				},
				Edges: []domain.Edge{
					{ID: "e1", Source: "n1", Target: "n2"},
					{ID: "e2", Source: "n2", Target: "n1"},
				},
			},
			wantErr: "form a loop",
		},
		{
			name: "self loop",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{{ID: "n1", Type: "set", Name: "A"}},
				Edges: []domain.Edge{{ID: "e1", Source: "n1", Target: "n1"}},
			},
			wantErr: "form a loop",
		},
		{
			name: "diamond is not a cycle",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					{ID: "n1", Type: "trigger.manual", Name: "Start"},
					{ID: "n2", Type: "set", Name: "Left"},
					{ID: "n3", Type: "set", Name: "Right"},
					{ID: "n4", Type: "merge", Name: "Join"},
				},
				Edges: []domain.Edge{
					{ID: "e1", Source: "n1", Target: "n2"},
					{ID: "e2", Source: "n1", Target: "n3"},
					{ID: "e3", Source: "n2", Target: "n4"},
					{ID: "e4", Source: "n3", Target: "n4"},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGraphStructure(tc.graph)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestTriggersFromGraph(t *testing.T) {
	now := time.Date(2026, 8, 18, 8, 0, 0, 0, time.UTC)

	graph := domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: nodeTypeWebhookTrigger, Name: "New lead",
			Params: map[string]any{"path": "/lead-in/", "method": "post"}},
		{ID: "n2", Type: nodeTypeScheduleTrigger, Name: "Nightly",
			Params: map[string]any{"cron": "0 9 * * *", "timezone": "UTC"}},
		{ID: "n3", Type: nodeTypeManualTrigger, Name: "By hand"},
		{ID: "n4", Type: "set", Name: "Shape"},
	}}

	hooks, schedules, err := triggersFromGraph("ws-1", "wf-1", graph, now)
	if err != nil {
		t.Fatalf("triggersFromGraph: %v", err)
	}

	if len(hooks) != 1 {
		t.Fatalf("got %d webhooks, want 1", len(hooks))
	}
	// A user pasting "/lead-in/" should not end up with a double-slashed URL.
	if hooks[0].Path != "lead-in" {
		t.Fatalf("path %q, want the trimmed form", hooks[0].Path)
	}
	if hooks[0].Method != "POST" {
		t.Fatalf("method %q, want it normalised to upper case", hooks[0].Method)
	}

	if len(schedules) != 1 {
		t.Fatalf("got %d schedules, want 1", len(schedules))
	}
	want := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	if !schedules[0].NextRunAt.Equal(want) {
		t.Fatalf("next run %v, want %v", schedules[0].NextRunAt, want)
	}
}

func TestTriggersFromGraphSkipsDisabledNodes(t *testing.T) {
	graph := domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: nodeTypeWebhookTrigger, Name: "New lead", Disabled: true,
			Params: map[string]any{"path": "lead-in"}},
	}}

	hooks, _, err := triggersFromGraph("ws-1", "wf-1", graph, time.Now())
	if err != nil {
		t.Fatalf("triggersFromGraph: %v", err)
	}
	// A disabled trigger with a live public URL would be a nasty surprise.
	if len(hooks) != 0 {
		t.Fatalf("got %d webhooks, want none for a disabled node", len(hooks))
	}
}

func TestTriggersFromGraphRejectsIncompleteTriggers(t *testing.T) {
	tests := []struct {
		name  string
		node  domain.GraphNode
		wants string
	}{
		{
			name:  "webhook without a path",
			node:  domain.GraphNode{ID: "n1", Type: nodeTypeWebhookTrigger, Name: "New lead"},
			wants: "no path",
		},
		{
			name:  "schedule without a cron",
			node:  domain.GraphNode{ID: "n1", Type: nodeTypeScheduleTrigger, Name: "Nightly"},
			wants: "no cron expression",
		},
		{
			name: "schedule with a nonsense cron",
			node: domain.GraphNode{ID: "n1", Type: nodeTypeScheduleTrigger, Name: "Nightly",
				Params: map[string]any{"cron": "not a cron"}},
			wants: "not a valid cron",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := triggersFromGraph("ws-1", "wf-1", domain.Graph{Nodes: []domain.GraphNode{tc.node}}, time.Now())
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wants)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Fatalf("error %q does not contain %q", err, tc.wants)
			}
		})
	}
}

func TestTriggersFromGraphDefaultsTimezoneAndMethod(t *testing.T) {
	graph := domain.Graph{Nodes: []domain.GraphNode{
		{ID: "n1", Type: nodeTypeWebhookTrigger, Name: "Hook", Params: map[string]any{"path": "p"}},
		{ID: "n2", Type: nodeTypeScheduleTrigger, Name: "Cron", Params: map[string]any{"cron": "*/5 * * * *"}},
	}}

	hooks, schedules, err := triggersFromGraph("ws-1", "wf-1", graph, time.Now())
	if err != nil {
		t.Fatalf("triggersFromGraph: %v", err)
	}
	if hooks[0].Method != "POST" {
		t.Fatalf("method %q, want the POST default", hooks[0].Method)
	}
	if schedules[0].Timezone != "UTC" {
		t.Fatalf("timezone %q, want the UTC default", schedules[0].Timezone)
	}
}
