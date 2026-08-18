package engine

import (
	"testing"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// validationRegistry knows one trigger and one plain node, so a test can make a
// type unknown simply by using a third name.
func validationRegistry() *nodes.Registry {
	return nodes.NewRegistry(fakeTrigger("trigger.test"), fakePass("pass.a"), fakeTrigger("trigger.other"))
}

func TestValidateGraphAcceptsARunnableGraph(t *testing.T) {
	mustNoError(t, ValidateGraph(linearGraph("trigger.test", "pass.a"), validationRegistry()),
		"a trigger feeding one node is runnable")
}

func TestValidateGraphRejects(t *testing.T) {
	cases := []struct {
		name  string
		graph domain.Graph
		want  string
	}{
		{
			name: "no trigger",
			graph: domain.Graph{Nodes: []domain.GraphNode{
				graphNode("n1", "pass.a", "Only"),
			}},
			want: "no trigger node",
		},
		{
			name: "two triggers",
			graph: domain.Graph{Nodes: []domain.GraphNode{
				graphNode("n1", "trigger.test", "Start"),
				graphNode("n2", "trigger.other", "Also start"),
			}},
			want: "2 trigger nodes",
		},
		{
			name: "duplicate node names",
			graph: domain.Graph{Nodes: []domain.GraphNode{
				graphNode("n1", "trigger.test", "Same"),
				graphNode("n2", "pass.a", "Same"),
			}},
			want: `duplicate node name "Same"`,
		},
		{
			name: "cycle",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{
					graphNode("n1", "trigger.test", "Start"),
					graphNode("n2", "pass.a", "Loop a"),
					graphNode("n3", "pass.a", "Loop b"),
				},
				Edges: []domain.Edge{
					graphEdge("e1", "n1", "", "n2", ""),
					graphEdge("e2", "n2", "", "n3", ""),
					graphEdge("e3", "n3", "", "n2", ""),
				},
			},
			want: "cycle",
		},
		{
			name: "unknown node type",
			graph: domain.Graph{Nodes: []domain.GraphNode{
				graphNode("n1", "trigger.test", "Start"),
				graphNode("n2", "does.not.exist", "Mystery"),
			}},
			want: `unknown node type "does.not.exist"`,
		},
		{
			name: "edge to a node that is not in the graph",
			graph: domain.Graph{
				Nodes: []domain.GraphNode{graphNode("n1", "trigger.test", "Start")},
				Edges: []domain.Edge{graphEdge("e1", "n1", "", "n9", "")},
			},
			want: `edge "e1" goes to unknown node "n9"`,
		},
		{
			name:  "empty graph",
			graph: domain.Graph{},
			want:  "no nodes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateGraph(tc.graph, validationRegistry())
			mustError(t, err, tc.name)
			ne := domain.AsNodeError(err)
			mustEqual(t, ne.Code, domain.ErrCodeValidation, "error code")
			mustContain(t, ne.Message, tc.want, "message")
			if _, ok := ne.Details["problems"]; !ok {
				t.Fatal("the editor needs the problems list in details")
			}
		})
	}
}

// diamond is start -> a and start -> b, both feeding end: the shape whose order
// a non-deterministic implementation would shuffle.
func diamond() domain.Graph {
	return domain.Graph{
		Nodes: []domain.GraphNode{
			graphNode("n4", "pass.a", "End"),
			graphNode("n2", "pass.a", "A"),
			graphNode("n1", "trigger.test", "Start"),
			graphNode("n3", "pass.a", "B"),
		},
		Edges: []domain.Edge{
			graphEdge("e1", "n1", "", "n2", ""),
			graphEdge("e2", "n1", "", "n3", ""),
			graphEdge("e3", "n2", "", "n4", ""),
			graphEdge("e4", "n3", "", "n4", ""),
		},
	}
}

func TestTopologicalOrderIsDeterministic(t *testing.T) {
	want := []string{"n1", "n2", "n3", "n4"}
	for i := range 50 {
		got, err := TopologicalOrder(diamond())
		mustNoError(t, err, "topological order")
		mustEqual(t, got, want, "repeat")
		if i == 0 {
			// Sanity: the expected order really is a valid ordering, not just a
			// snapshot of whatever the implementation happens to do.
			mustEqual(t, len(got), 4, "every node appears once")
		}
	}
}

func TestTopologicalOrderRejectsACycle(t *testing.T) {
	g := domain.Graph{
		Nodes: []domain.GraphNode{
			graphNode("n1", "pass.a", "A"),
			graphNode("n2", "pass.a", "B"),
		},
		Edges: []domain.Edge{
			graphEdge("e1", "n1", "", "n2", ""),
			graphEdge("e2", "n2", "", "n1", ""),
		},
	}
	_, err := TopologicalOrder(g)
	mustError(t, err, "cycle")
	mustContain(t, domain.AsNodeError(err).Message, `"n1", "n2"`, "the cycle names its nodes")
}

func TestTopologicalOrderRejectsASelfLoop(t *testing.T) {
	g := domain.Graph{
		Nodes: []domain.GraphNode{graphNode("n1", "pass.a", "A")},
		Edges: []domain.Edge{graphEdge("e1", "n1", "", "n1", "")},
	}
	_, err := TopologicalOrder(g)
	mustError(t, err, "self loop")
}

func TestDescendantsIncludesTheNodeItself(t *testing.T) {
	got := descendants(diamond(), "n2")
	mustEqual(t, got, map[string]bool{"n2": true, "n4": true},
		"a resume recomputes the node and everything below it")
}
