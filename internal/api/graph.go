package api

import (
	"fmt"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// validateGraphStructure checks only the rules that would make the stored JSON
// meaningless — dangling edges, duplicate ids, duplicate names, a cycle.
//
// A draft is allowed to be half-built: a missing trigger or an unfilled
// required parameter is normal while editing and is caught later, when the
// workflow is run or activated. Rejecting those on save would make the editor
// hostile.
func validateGraphStructure(g domain.Graph) error {
	ids := make(map[string]struct{}, len(g.Nodes))
	names := make(map[string]struct{}, len(g.Nodes))

	for _, n := range g.Nodes {
		if n.ID == "" {
			return fmt.Errorf("a node is missing its id")
		}
		if _, dup := ids[n.ID]; dup {
			return fmt.Errorf("two nodes share the id %q", n.ID)
		}
		ids[n.ID] = struct{}{}

		if n.Name == "" {
			return fmt.Errorf("node %q is missing its name", n.ID)
		}
		if _, dup := names[n.Name]; dup {
			// Expressions address nodes by name, so a duplicate is ambiguous
			// rather than merely untidy.
			return fmt.Errorf("two nodes are both called %q; names must be unique", n.Name)
		}
		names[n.Name] = struct{}{}

		if n.Type == "" {
			return fmt.Errorf("node %q is missing its type", n.Name)
		}
	}

	for _, e := range g.Edges {
		if _, ok := ids[e.Source]; !ok {
			return fmt.Errorf("an edge starts at unknown node %q", e.Source)
		}
		if _, ok := ids[e.Target]; !ok {
			return fmt.Errorf("an edge ends at unknown node %q", e.Target)
		}
	}

	if node, looped := findCycle(g); looped {
		return fmt.Errorf("the connections form a loop through %q, which cannot be executed", node)
	}
	return nil
}

// findCycle reports the first node found on a cycle, using an iterative
// depth-first search so a pathological graph cannot blow the stack.
func findCycle(g domain.Graph) (string, bool) {
	out := make(map[string][]string, len(g.Nodes))
	for _, e := range g.Edges {
		out[e.Source] = append(out[e.Source], e.Target)
	}

	const (
		unseen = 0
		open   = 1
		closed = 2
	)
	state := make(map[string]int, len(g.Nodes))

	type frame struct {
		node string
		next int
	}

	for _, start := range g.Nodes {
		if state[start.ID] != unseen {
			continue
		}
		stack := []frame{{node: start.ID}}
		state[start.ID] = open

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			children := out[top.node]

			if top.next >= len(children) {
				state[top.node] = closed
				stack = stack[:len(stack)-1]
				continue
			}
			child := children[top.next]
			top.next++

			switch state[child] {
			case open:
				return child, true
			case unseen:
				state[child] = open
				stack = append(stack, frame{node: child})
			}
		}
	}
	return "", false
}

// webhookURLFor renders the public URL of the graph's webhook trigger, so the
// editor can show it without knowing the deployment's host.
func (s *server) webhookURLFor(g domain.Graph) string {
	for _, n := range g.Nodes {
		if n.Type != nodeTypeWebhookTrigger {
			continue
		}
		path, _ := n.Params["path"].(string)
		if path == "" {
			continue
		}
		return s.cfg.WebhookURL(path)
	}
	return ""
}
