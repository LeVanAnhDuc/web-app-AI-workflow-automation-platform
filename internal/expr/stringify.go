package expr

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

// Stringify renders a value the way interpolation does: strings as themselves,
// integral numbers without a trailing ".0", booleans as true/false, nil as the
// empty string, and anything structured as compact JSON. Nodes reuse it so a
// parameter reads the same whether it came from an expression or a literal.
func Stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int8:
		return strconv.FormatInt(int64(t), 10)
	case int16:
		return strconv.FormatInt(int64(t), 10)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint:
		return strconv.FormatUint(uint64(t), 10)
	case uint8:
		return strconv.FormatUint(uint64(t), 10)
	case uint16:
		return strconv.FormatUint(uint64(t), 10)
	case uint32:
		return strconv.FormatUint(uint64(t), 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float32:
		return formatFloat(float64(t))
	case float64:
		return formatFloat(t)
	case time.Time:
		return t.Format(time.RFC3339)
	case error:
		return t.Error()
	case fmt.Stringer:
		return t.String()
	}
	// Maps, slices and structs: compact JSON, so an object interpolated into a
	// URL or a body is at least valid data rather than Go's %v syntax.
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}

// formatFloat drops the fractional part of an integral value: JSON decodes
// every number to float64, and "id: 42" reads better than "id: 42.0".
func formatFloat(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	if math.Abs(f) >= 1e21 {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
