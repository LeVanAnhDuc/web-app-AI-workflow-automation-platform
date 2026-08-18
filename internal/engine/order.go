package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// ValidateGraph reports the problems that make a graph unrunnable. Every
// problem is collected rather than only the first, because the editor shows
// them all at once, and the result is a domain.ErrCodeValidation error so the
// execution row records why it never started.
func ValidateGraph(g domain.Graph, reg *nodes.Registry) error {
	var problems []string

	if len(g.Nodes) == 0 {
		problems = append(problems, "graph has no nodes")
	}

	ids := make(map[string]bool, len(g.Nodes))
	names := make(map[string]bool, len(g.Nodes))
	var triggers []string
	for _, n := range g.Nodes {
		switch {
		case n.ID == "":
			problems = append(problems, "a node has an empty id")
		case ids[n.ID]:
			problems = append(problems, fmt.Sprintf("duplicate node id %q", n.ID))
		}
		ids[n.ID] = true

		switch {
		case n.Name == "":
			problems = append(problems, fmt.Sprintf("node %q has an empty name", n.ID))
		case names[n.Name]:
			// Expressions address nodes by name, so a duplicate is ambiguous.
			problems = append(problems, fmt.Sprintf("duplicate node name %q", n.Name))
		}
		names[n.Name] = true

		if reg != nil {
			if _, ok := reg.Get(n.Type); !ok {
				problems = append(problems,
					fmt.Sprintf("unknown node type %q on node %q", n.Type, n.Name))
			}
		}
		if isTriggerNode(reg, n) {
			triggers = append(triggers, n.Name)
		}
	}

	switch {
	case len(triggers) == 0 && len(g.Nodes) > 0:
		problems = append(problems, "graph has no trigger node")
	case len(triggers) > 1:
		problems = append(problems, fmt.Sprintf(
			"graph has %d trigger nodes (%s); exactly one is allowed",
			len(triggers), strings.Join(quoteAll(triggers), ", ")))
	}

	for _, e := range g.Edges {
		if !ids[e.Source] {
			problems = append(problems,
				fmt.Sprintf("edge %q comes from unknown node %q", e.ID, e.Source))
		}
		if !ids[e.Target] {
			problems = append(problems,
				fmt.Sprintf("edge %q goes to unknown node %q", e.ID, e.Target))
		}
	}

	if _, err := TopologicalOrder(g); err != nil {
		problems = append(problems, err.Error())
	}

	if len(problems) == 0 {
		return nil
	}
	err := domain.Errorf(domain.ErrCodeValidation, "%s", strings.Join(problems, "; "))
	err.Details = map[string]any{"problems": problems}
	return err
}

// isTriggerNode asks the registry, falling back to the type prefix so a graph
// with an unregistered trigger reports "unknown node type" rather than the
// misleading "no trigger node".
func isTriggerNode(reg *nodes.Registry, n domain.GraphNode) bool {
	if reg != nil {
		if impl, ok := reg.Get(n.Type); ok {
			return impl.Descriptor().IsTrigger
		}
	}
	return strings.HasPrefix(n.Type, "trigger.")
}

// TopologicalOrder returns node ids in a deterministic execution order,
// tie-breaking on node id so two runs of the same graph agree. Determinism is
// what makes an execution log comparable between runs and a resumed run pick
// the same next node as the run it continues.
func TopologicalOrder(g domain.Graph) ([]string, error) {
	indegree := make(map[string]int, len(g.Nodes))
	order := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if _, dup := indegree[n.ID]; dup {
			continue
		}
		indegree[n.ID] = 0
	}
	successors := make(map[string][]string, len(g.Nodes))
	for _, e := range g.Edges {
		if _, ok := indegree[e.Source]; !ok {
			continue
		}
		if _, ok := indegree[e.Target]; !ok {
			continue
		}
		successors[e.Source] = append(successors[e.Source], e.Target)
		indegree[e.Target]++
	}

	// A ready list kept sorted by id is the tie-break; Phase 2 can hand this
	// same list to a worker pool without changing the ordering guarantee.
	var ready []string
	for id, d := range indegree {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		for _, next := range successors[id] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = insertSorted(ready, next)
			}
		}
	}

	if len(order) != len(indegree) {
		var stuck []string
		for id, d := range indegree {
			if d > 0 {
				stuck = append(stuck, id)
			}
		}
		sort.Strings(stuck)
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"graph contains a cycle through nodes %s", strings.Join(quoteAll(stuck), ", "))
	}
	return order, nil
}

func insertSorted(list []string, v string) []string {
	i := sort.SearchStrings(list, v)
	list = append(list, "")
	copy(list[i+1:], list[i:])
	list[i] = v
	return list
}

// descendants is the node itself plus everything reachable from it, which is
// exactly the set a resume must recompute.
func descendants(g domain.Graph, id string) map[string]bool {
	seen := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.OutgoingEdges(cur) {
			if !seen[e.Target] {
				seen[e.Target] = true
				queue = append(queue, e.Target)
			}
		}
	}
	return seen
}

func quoteAll(vs []string) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = fmt.Sprintf("%q", v)
	}
	return out
}

// sortedKeys gives map iteration a deterministic order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
