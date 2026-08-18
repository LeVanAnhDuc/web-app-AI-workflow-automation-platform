package engine

import (
	"testing"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/expr"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

func resolverSpecs() []nodes.ParamSpec {
	return []nodes.ParamSpec{
		{Name: "url", Type: nodes.ParamString, SupportsExpression: true, Required: true},
		{Name: "method", Type: nodes.ParamString, Default: "GET"},
		{Name: "note", Type: nodes.ParamString},
		{Name: "literalString", Type: nodes.ParamString}, // no SupportsExpression
		{Name: "timeout", Type: nodes.ParamNumber, SupportsExpression: true},
		{Name: "retries", Type: nodes.ParamNumber, Default: float64(4)},
		{Name: "flag", Type: nodes.ParamBoolean},
		{Name: "body", Type: nodes.ParamJSON, SupportsExpression: true},
		{Name: "headers", Type: nodes.ParamKeyValue, SupportsExpression: true},
		{Name: "conditions", Type: nodes.ParamFilter, SupportsExpression: true},
		{Name: "jsCode", Type: nodes.ParamCode},
	}
}

func newTestResolver(params map[string]any) nodes.ParamResolver {
	env := expr.Env{
		JSON: map[string]any{
			"id":   float64(42),
			"name": "Ada",
			"host": "api.example.com",
		},
		ExecutionID: "exec-1",
	}
	return NewParamResolver(resolverSpecs(), params, env)
}

func TestResolverEvaluatesExpressionsOnlyWhereTheSpecAllowsIt(t *testing.T) {
	r := newTestResolver(map[string]any{
		"url":           "https://{{ $json.host }}/v1/users/{{ $json.id }}",
		"literalString": "keep {{ $json.name }} as typed",
		"jsCode":        "return items.map(i => i.json)",
	})

	got, err := r.String("url")
	mustNoError(t, err, "url")
	mustEqual(t, got, "https://api.example.com/v1/users/42", "interpolated url")

	got, err = r.String("literalString")
	mustNoError(t, err, "literalString")
	mustEqual(t, got, "keep {{ $json.name }} as typed",
		"a spec without SupportsExpression is never evaluated")

	mustEqual(t, r.Literal("jsCode"), "return items.map(i => i.json)",
		"Literal returns the stored value verbatim")
}

func TestResolverTypedAccessors(t *testing.T) {
	r := newTestResolver(map[string]any{
		"timeout": "{{ 5 * 200 }}",
		"flag":    true,
		"body":    map[string]any{"a": 1},
		"note":    "plain",
	})

	n, err := r.Int("timeout")
	mustNoError(t, err, "timeout")
	mustEqual(t, n, 1000, "a numeric expression stays numeric")

	f, err := r.Float("timeout")
	mustNoError(t, err, "timeout as float")
	mustEqual(t, f, float64(1000), "float accessor")

	b, err := r.Bool("flag")
	mustNoError(t, err, "flag")
	mustEqual(t, b, true, "bool accessor")

	raw, err := r.Raw("body")
	mustNoError(t, err, "body")
	mustEqual(t, raw, map[string]any{"a": 1}, "Raw keeps the stored type")

	s, err := r.String("note")
	mustNoError(t, err, "note")
	mustEqual(t, s, "plain", "string accessor")
}

func TestResolverStringifiesANonStringValue(t *testing.T) {
	r := newTestResolver(map[string]any{"note": float64(3)})
	got, err := r.String("note")
	mustNoError(t, err, "note")
	mustEqual(t, got, "3", "an integral number loses its .0")
}

func TestResolverDefaultsAndMissingValues(t *testing.T) {
	r := newTestResolver(map[string]any{"url": "https://example.com", "note": ""})

	method, err := r.String("method")
	mustNoError(t, err, "method")
	mustEqual(t, method, "GET", "the spec default fills in")

	retries, err := r.Int("retries")
	mustNoError(t, err, "retries")
	mustEqual(t, retries, 4, "numeric default")

	note, err := r.String("note")
	mustNoError(t, err, "note")
	mustEqual(t, note, "", "an optional parameter left empty is the zero value")

	flag, err := r.Bool("flag")
	mustNoError(t, err, "flag")
	mustEqual(t, flag, false, "an absent boolean is false, not an error")

	mustEqual(t, r.StringOr("note", "fallback"), "fallback", "StringOr falls back")
	mustEqual(t, r.IntOr("timeout", 30), 30, "IntOr falls back")
	mustEqual(t, r.BoolOr("flag", true), true, "BoolOr falls back")
	mustEqual(t, r.StringOr("method", "POST"), "GET", "a default beats the fallback")
}

func TestResolverRequiredAndUnknownParameters(t *testing.T) {
	r := newTestResolver(map[string]any{})

	_, err := r.String("url")
	mustError(t, err, "required parameter")
	mustContain(t, domain.AsNodeError(err).Message, `parameter "url" is required`, "message")

	_, err = r.String("nope")
	mustError(t, err, "unknown parameter")
	mustContain(t, domain.AsNodeError(err).Message, `unknown parameter "nope"`, "message")
}

func TestResolverKeyValues(t *testing.T) {
	// The shape the frontend stores: a []any of {key, value} maps.
	r := newTestResolver(map[string]any{
		"headers": []any{
			map[string]any{"key": "X-Name", "value": "{{ $json.name }}"},
			map[string]any{"key": "{{ 'X-' + 'Id' }}", "value": "{{ $json.id }}"},
		},
	})
	rows, err := r.KeyValues("headers")
	mustNoError(t, err, "headers")
	mustEqual(t, rows, []nodes.KeyValue{
		{Key: "X-Name", Value: "Ada"},
		{Key: "X-Id", Value: "42"},
	}, "both sides of every row are resolved")
}

func TestResolverKeyValuesAcceptsAnAlreadyTypedSlice(t *testing.T) {
	r := newTestResolver(map[string]any{
		"headers": []nodes.KeyValue{{Key: "X-Name", Value: "{{ $json.name }}"}},
	})
	rows, err := r.KeyValues("headers")
	mustNoError(t, err, "headers")
	mustEqual(t, rows, []nodes.KeyValue{{Key: "X-Name", Value: "Ada"}}, "typed rows resolve too")
}

func TestResolverConditionsKeepOperandTypes(t *testing.T) {
	r := newTestResolver(map[string]any{
		"conditions": []any{
			map[string]any{"left": "{{ $json.id }}", "operator": "equals", "right": float64(42)},
			map[string]any{"left": "{{ $json.name }}", "operator": "contains", "right": "Ad"},
		},
	})
	rows, err := r.Conditions("conditions")
	mustNoError(t, err, "conditions")
	mustEqual(t, rows, []nodes.Condition{
		{Left: float64(42), Operator: "equals", Right: float64(42)},
		{Left: "Ada", Operator: "contains", Right: "Ad"},
	}, "a numeric operand is not turned into a string")
}

func TestResolverPropagatesAnExpressionError(t *testing.T) {
	r := newTestResolver(map[string]any{"url": "{{ $json.host"})
	_, err := r.String("url")
	mustError(t, err, "malformed expression")
	mustEqual(t, domain.AsNodeError(err).Code, domain.ErrCodeExpression, "error code")
}

func TestResolverRejectsAWrongType(t *testing.T) {
	r := newTestResolver(map[string]any{"timeout": "not a number", "headers": "not a list"})

	_, err := r.Int("timeout")
	mustError(t, err, "non-numeric value")
	mustEqual(t, domain.AsNodeError(err).Code, domain.ErrCodeValidation, "error code")

	_, err = r.KeyValues("headers")
	mustError(t, err, "non-list value")
}

func TestResolverLiteralFallsBackToTheSpecDefault(t *testing.T) {
	r := newTestResolver(map[string]any{})
	mustEqual(t, r.Literal("method"), "GET", "the default is the literal when nothing is stored")
	mustEqual(t, r.Literal("unknown"), nil, "an unknown name has no literal")
}
