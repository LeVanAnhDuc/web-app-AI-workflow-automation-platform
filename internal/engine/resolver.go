package engine

import (
	"strconv"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/expr"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// NewParamResolver builds the nodes.ParamResolver for one node and one item.
// The node's ParamSpec list is the contract: a name the node did not declare is
// a bug in the node, and only a spec that says SupportsExpression gets its
// strings evaluated, so the code node's body is never interpolated by accident.
func NewParamResolver(specs []nodes.ParamSpec, params map[string]any, env expr.Env) nodes.ParamResolver {
	byName := make(map[string]nodes.ParamSpec, len(specs))
	for _, s := range specs {
		byName[s.Name] = s
	}
	return &paramResolver{specs: byName, params: params, env: env}
}

type paramResolver struct {
	specs  map[string]nodes.ParamSpec
	params map[string]any
	env    expr.Env
}

// resolve is the one place a parameter turns into a value. present is false for
// an optional parameter the user left empty, which is how the …Or accessors
// tell "empty" apart from "legitimately zero".
func (r *paramResolver) resolve(name string) (value any, present bool, err error) {
	spec, ok := r.specs[name]
	if !ok {
		return nil, false, nodes.ErrUnknownParam(name)
	}
	raw, stored := r.params[name]
	if !stored || isBlank(raw) {
		switch {
		case !isBlank(spec.Default):
			raw = spec.Default
		case spec.Required:
			return nil, false, nodes.ErrRequiredParam(name)
		default:
			return nil, false, nil
		}
	}

	switch spec.Type {
	case nodes.ParamKeyValue:
		rows, err := r.keyValues(name, raw)
		return rows, true, err
	case nodes.ParamFilter:
		rows, err := r.conditions(name, raw)
		return rows, true, err
	}

	if s, isString := raw.(string); isString && spec.SupportsExpression {
		v, err := expr.Evaluate(s, r.env)
		if err != nil {
			return nil, false, err
		}
		return v, true, nil
	}
	return raw, true, nil
}

// String returns the parameter as text, stringifying a typed expression result
// the same way interpolation does.
func (r *paramResolver) String(name string) (string, error) {
	v, present, err := r.resolve(name)
	if err != nil || !present {
		return "", err
	}
	return expr.Stringify(v), nil
}

// Int returns the parameter as an integer, tolerating the float64 that JSON
// decoding produces and a numeric string from an expression.
func (r *paramResolver) Int(name string) (int, error) {
	f, err := r.Float(name)
	return int(f), err
}

// Float returns the parameter as a number.
func (r *paramResolver) Float(name string) (float64, error) {
	v, present, err := r.resolve(name)
	if err != nil || !present {
		return 0, err
	}
	f, ok := toFloat(v)
	if !ok {
		return 0, domain.Errorf(domain.ErrCodeValidation,
			"parameter %q is not a number: %v", name, v)
	}
	return f, nil
}

// Bool returns the parameter as a boolean.
func (r *paramResolver) Bool(name string) (bool, error) {
	v, present, err := r.resolve(name)
	if err != nil || !present {
		return false, err
	}
	b, ok := toBool(v)
	if !ok {
		return false, domain.Errorf(domain.ErrCodeValidation,
			"parameter %q is not a boolean: %v", name, v)
	}
	return b, nil
}

// Raw returns the resolved value with its type intact, for a json parameter or
// a node that wants the typed result of an expression.
func (r *paramResolver) Raw(name string) (any, error) {
	v, _, err := r.resolve(name)
	return v, err
}

// KeyValues returns a keyValue parameter's rows, each side resolved.
func (r *paramResolver) KeyValues(name string) ([]nodes.KeyValue, error) {
	v, present, err := r.resolve(name)
	if err != nil || !present {
		return nil, err
	}
	rows, ok := v.([]nodes.KeyValue)
	if !ok {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"parameter %q is not a key/value list", name)
	}
	return rows, nil
}

// Conditions returns a filter parameter's rows, each operand resolved.
func (r *paramResolver) Conditions(name string) ([]nodes.Condition, error) {
	v, present, err := r.resolve(name)
	if err != nil || !present {
		return nil, err
	}
	rows, ok := v.([]nodes.Condition)
	if !ok {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"parameter %q is not a condition list", name)
	}
	return rows, nil
}

// StringOr is String with a fallback for anything the user left empty.
func (r *paramResolver) StringOr(name, def string) string {
	v, present, err := r.resolve(name)
	if err != nil || !present {
		return def
	}
	s := expr.Stringify(v)
	if s == "" {
		return def
	}
	return s
}

// IntOr is Int with a fallback. A stored zero is honoured; only an absent or
// unparsable value falls back.
func (r *paramResolver) IntOr(name string, def int) int {
	v, present, err := r.resolve(name)
	if err != nil || !present {
		return def
	}
	f, ok := toFloat(v)
	if !ok {
		return def
	}
	return int(f)
}

// BoolOr is Bool with a fallback.
func (r *paramResolver) BoolOr(name string, def bool) bool {
	v, present, err := r.resolve(name)
	if err != nil || !present {
		return def
	}
	b, ok := toBool(v)
	if !ok {
		return def
	}
	return b
}

// Literal returns the parameter exactly as stored, with no expression
// evaluation, because the code node's body must reach goja verbatim.
func (r *paramResolver) Literal(name string) any {
	if v, ok := r.params[name]; ok && !isBlank(v) {
		return v
	}
	if spec, ok := r.specs[name]; ok {
		return spec.Default
	}
	return nil
}

// keyValues resolves the rows the frontend stores as [{key, value}, …]. Both
// sides go through the evaluator so a header value can be an expression.
func (r *paramResolver) keyValues(name string, raw any) ([]nodes.KeyValue, error) {
	rows, ok := rowsOf(raw)
	if !ok {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"parameter %q is not a key/value list", name)
	}
	out := make([]nodes.KeyValue, 0, len(rows))
	for _, row := range rows {
		key, err := r.evalString(row["key"])
		if err != nil {
			return nil, err
		}
		value, err := r.evalString(row["value"])
		if err != nil {
			return nil, err
		}
		out = append(out, nodes.KeyValue{Key: key, Value: value})
	}
	return out, nil
}

// conditions resolves the rows the frontend stores as
// [{left, operator, right}, …]. The operands keep their type, so a numeric
// comparison is not turned into a string comparison behind the user's back.
func (r *paramResolver) conditions(name string, raw any) ([]nodes.Condition, error) {
	rows, ok := rowsOf(raw)
	if !ok {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"parameter %q is not a condition list", name)
	}
	out := make([]nodes.Condition, 0, len(rows))
	for _, row := range rows {
		left, err := r.evalAny(row["left"])
		if err != nil {
			return nil, err
		}
		right, err := r.evalAny(row["right"])
		if err != nil {
			return nil, err
		}
		operator, err := r.evalString(row["operator"])
		if err != nil {
			return nil, err
		}
		out = append(out, nodes.Condition{Left: left, Operator: operator, Right: right})
	}
	return out, nil
}

func (r *paramResolver) evalAny(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return v, nil
	}
	return expr.Evaluate(s, r.env)
}

func (r *paramResolver) evalString(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return expr.Stringify(v), nil
	}
	return expr.EvaluateString(s, r.env)
}

// rowsOf normalises the several shapes a repeating parameter arrives in: the
// []any of maps the frontend stores, an already-typed slice from a Go caller,
// or a plain []map[string]any from a test.
func rowsOf(raw any) ([]map[string]any, bool) {
	switch t := raw.(type) {
	case nil:
		return nil, true
	case []map[string]any:
		return t, true
	case []nodes.KeyValue:
		out := make([]map[string]any, 0, len(t))
		for _, kv := range t {
			out = append(out, map[string]any{"key": kv.Key, "value": kv.Value})
		}
		return out, true
	case []nodes.Condition:
		out := make([]map[string]any, 0, len(t))
		for _, c := range t {
			out = append(out, map[string]any{"left": c.Left, "operator": c.Operator, "right": c.Right})
		}
		return out, true
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, el := range t {
			switch row := el.(type) {
			case map[string]any:
				out = append(out, row)
			case nodes.KeyValue:
				out = append(out, map[string]any{"key": row.Key, "value": row.Value})
			case nodes.Condition:
				out = append(out, map[string]any{"left": row.Left, "operator": row.Operator, "right": row.Right})
			default:
				return nil, false
			}
		}
		return out, true
	}
	return nil, false
}

// isBlank is the "user left it empty" test: nil, or an empty string. A false or
// a 0 is a real value and must not fall back to the default.
func isBlank(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	}
	return 0, false
}

func toBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		b, err := strconv.ParseBool(t)
		return b, err == nil
	}
	if f, ok := toFloat(v); ok {
		return f != 0, true
	}
	return false, false
}
