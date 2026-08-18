// Package expr evaluates the {{ … }} expressions a workflow author may put in
// any string parameter. It wraps github.com/expr-lang/expr — which reaches
// neither the filesystem nor the network by construction — and adds the two
// things the product needs on top of it: the $-prefixed names from the spec
// (expr-lang cannot lex a leading $, so they are rewritten before compilation)
// and template semantics, where one lone expression yields a typed value while
// several are interpolated into a string.
package expr

import (
	"fmt"
	"strings"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	exprlang "github.com/expr-lang/expr"
)

// Env is everything an expression can see. The engine builds one per node and
// per item; nothing else is reachable from an expression.
type Env struct {
	// JSON is the current item's JSON, exposed as $json.
	JSON map[string]any
	// Items is the node's whole input, exposed as $items.
	Items []domain.Item
	// Nodes maps node name -> output handle -> items, exposed as $node["Name"].
	Nodes map[string]map[string][]domain.Item
	// ItemIndex is the zero-based index of the current item, as $itemIndex.
	ItemIndex int
	// Now is the run clock; the engine injects it so a run is reproducible.
	Now time.Time
	// ExecutionID is exposed as $execution.id.
	ExecutionID string
}

// vars builds the expr-lang environment. The keys carry the _dollar_ prefix
// that Preprocess rewrites $names to, because expr-lang will not lex a $.
func (e Env) vars() map[string]any {
	now := e.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	json := e.JSON
	if json == nil {
		// A nil map would make every $json.x a nil dereference; an empty map
		// makes a missing key nil, which is what the spec asks for.
		json = map[string]any{}
	}
	return map[string]any{
		dollar + "json":      json,
		dollar + "items":     itemsToAny(e.Items),
		dollar + "node":      nodeVar(e.Nodes),
		dollar + "itemIndex": e.ItemIndex,
		dollar + "now":       now.Format(time.RFC3339),
		dollar + "execution": map[string]any{"id": e.ExecutionID},
	}
}

// itemsToAny unwraps items to their JSON so an expression sees plain data
// rather than the domain.Item wrapper.
func itemsToAny(items []domain.Item) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		if it.JSON == nil {
			out = append(out, map[string]any{})
			continue
		}
		out = append(out, it.JSON)
	}
	return out
}

// nodeVar shapes every finished node as {json, items} keyed by node name.
func nodeVar(byName map[string]map[string][]domain.Item) map[string]any {
	out := make(map[string]any, len(byName))
	for name, handles := range byName {
		items := mainItems(handles)
		first := map[string]any{}
		if len(items) > 0 && items[0].JSON != nil {
			first = items[0].JSON
		}
		out[name] = map[string]any{
			"json":  first,
			"items": itemsToAny(items),
		}
	}
	return out
}

// mainItems picks the items a node reference means: its main output, or — for a
// branching node whose main handle is unused — everything it emitted, so
// $node["If"].items is not mysteriously empty.
func mainItems(handles map[string][]domain.Item) []domain.Item {
	if items := handles[domain.MainHandle]; len(items) > 0 {
		return items
	}
	var out []domain.Item
	for _, h := range sortedKeys(handles) {
		out = append(out, handles[h]...)
	}
	return out
}

// HasExpression reports whether a parameter value needs evaluating at all.
func HasExpression(src string) bool {
	return strings.Contains(src, open)
}

// Evaluate returns the typed value when src is exactly one {{ … }}, and the
// interpolated string otherwise. A src with no {{ }} is returned unchanged.
func Evaluate(src string, env Env) (any, error) {
	segs, err := split(src)
	if err != nil {
		return nil, err
	}
	if len(segs) == 1 && segs[0].isExpr {
		return evalOne(segs[0].text, env.vars())
	}
	if !anyExpr(segs) {
		return src, nil
	}
	vars := env.vars()
	var b strings.Builder
	for _, s := range segs {
		if !s.isExpr {
			b.WriteString(s.text)
			continue
		}
		v, err := evalOne(s.text, vars)
		if err != nil {
			return nil, err
		}
		b.WriteString(Stringify(v))
	}
	return b.String(), nil
}

// EvaluateString is Evaluate for the common case of a parameter the node reads
// as text: the typed result is stringified the same way interpolation does.
func EvaluateString(src string, env Env) (string, error) {
	v, err := Evaluate(src, env)
	if err != nil {
		return "", err
	}
	return Stringify(v), nil
}

// evalOne compiles and runs a single expression body. Both failures surface as
// domain.ErrCodeExpression naming the expression, because the workflow author
// reads this message in the execution log.
func evalOne(body string, vars map[string]any) (any, error) {
	program, err := exprlang.Compile(Preprocess(body),
		exprlang.Env(vars),
		// A typo'd name is nil at runtime rather than a compile failure, which
		// matches "a missing key yields nil" for data references.
		exprlang.AllowUndefinedVariables(),
	)
	if err != nil {
		return nil, exprError(body, "cannot compile", err)
	}
	out, err := exprlang.Run(program, vars)
	if err != nil {
		return nil, exprError(body, "cannot evaluate", err)
	}
	return out, nil
}

func exprError(body, what string, err error) *domain.NodeError {
	return &domain.NodeError{
		Code:    domain.ErrCodeExpression,
		Message: fmt.Sprintf("%s expression {{%s}}: %s", what, body, oneLine(err)),
	}
}

// oneLine flattens expr-lang's multi-line caret diagnostics so the message fits
// one row of the execution log.
func oneLine(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}
