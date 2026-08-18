package nodes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// This file holds the value coercions the nodes share. They exist because JSON
// gives a workflow author no control over types: the same field arrives as 1,
// 1.0 or "1" depending on which API produced it, and a comparison that took
// those for three different values would be unusable in practice.

// stringify renders a value the way a workflow author expects to see it: a
// string stays itself, a number loses the trailing zeros Go would print, and a
// structure becomes its JSON.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return t.String()
	}
	if encoded, err := json.Marshal(v); err == nil {
		return string(encoded)
	}
	return fmt.Sprint(v)
}

// toFloat coerces every numeric JSON type, plus a string that holds a number,
// to a float. Booleans are deliberately not numbers: treating true as 1 would
// make `status > 0` silently true for a boolean field.
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
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(trimmed, 64)
		return f, err == nil
	}
	return 0, false
}

// looseEqual compares two values numerically when both sides can be read as
// numbers and textually otherwise, so 1, 1.0 and "1" are all equal.
func looseEqual(left, right any) bool {
	if lf, ok := toFloat(left); ok {
		if rf, ok := toFloat(right); ok {
			return lf == rf
		}
	}
	return stringify(left) == stringify(right)
}

// compareValues orders two values, returning -1, 0 or 1. Numbers compare as
// numbers; anything else falls back to comparing the rendered strings, which at
// least gives dates and version-free identifiers a sensible order.
func compareValues(left, right any) int {
	if lf, ok := toFloat(left); ok {
		if rf, ok := toFloat(right); ok {
			switch {
			case lf < rf:
				return -1
			case lf > rf:
				return 1
			default:
				return 0
			}
		}
	}
	return strings.Compare(stringify(left), stringify(right))
}

// isEmptyValue reports whether a value counts as "not filled in". A zero number
// and a false boolean are values the user set, so neither is empty.
func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

// truthy applies JavaScript-like truthiness, extended so that the strings a
// form or a query parameter produces ("true", "1", "no") behave as the booleans
// the user meant.
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		if b, err := strconv.ParseBool(strings.TrimSpace(t)); err == nil {
			return b
		}
		return strings.TrimSpace(t) != ""
	}
	if f, ok := toFloat(v); ok {
		return f != 0
	}
	return !isEmptyValue(v)
}
