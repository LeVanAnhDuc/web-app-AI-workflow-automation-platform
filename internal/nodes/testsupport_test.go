package nodes

import (
	"sort"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// fakeParams is a ParamResolver over a plain map. Expression evaluation belongs
// to internal/expr and is tested there, so these tests hand the nodes the values
// a resolver would already have produced.
type fakeParams struct {
	values map[string]any
}

// params builds a resolver over the given parameter values.
func params(values map[string]any) *fakeParams {
	if values == nil {
		values = map[string]any{}
	}
	return &fakeParams{values: values}
}

func (p *fakeParams) String(name string) (string, error) {
	v, ok := p.values[name]
	if !ok {
		return "", nil
	}
	return stringify(v), nil
}

func (p *fakeParams) Int(name string) (int, error) {
	f, err := p.Float(name)
	return int(f), err
}

func (p *fakeParams) Float(name string) (float64, error) {
	v, ok := p.values[name]
	if !ok {
		return 0, nil
	}
	f, numeric := toFloat(v)
	if !numeric {
		return 0, domain.Errorf(domain.ErrCodeValidation, "parameter %q is not a number", name)
	}
	return f, nil
}

func (p *fakeParams) Bool(name string) (bool, error) {
	v, ok := p.values[name]
	if !ok {
		return false, nil
	}
	return truthy(v), nil
}

func (p *fakeParams) Raw(name string) (any, error) {
	return p.values[name], nil
}

func (p *fakeParams) KeyValues(name string) ([]KeyValue, error) {
	switch v := p.values[name].(type) {
	case nil:
		return nil, nil
	case []KeyValue:
		return v, nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		// Sorted so a test asserting on an encoded query string is stable.
		sort.Strings(keys)
		rows := make([]KeyValue, 0, len(keys))
		for _, key := range keys {
			rows = append(rows, KeyValue{Key: key, Value: stringify(v[key])})
		}
		return rows, nil
	default:
		return nil, domain.Errorf(domain.ErrCodeValidation, "parameter %q is not a key/value list", name)
	}
}

func (p *fakeParams) Conditions(name string) ([]Condition, error) {
	switch v := p.values[name].(type) {
	case nil:
		return nil, nil
	case []Condition:
		return v, nil
	default:
		return nil, domain.Errorf(domain.ErrCodeValidation, "parameter %q is not a condition list", name)
	}
}

func (p *fakeParams) StringOr(name, def string) string {
	if _, ok := p.values[name]; !ok {
		return def
	}
	s, err := p.String(name)
	if err != nil || s == "" {
		return def
	}
	return s
}

func (p *fakeParams) IntOr(name string, def int) int {
	if _, ok := p.values[name]; !ok {
		return def
	}
	n, err := p.Int(name)
	if err != nil {
		return def
	}
	return n
}

func (p *fakeParams) BoolOr(name string, def bool) bool {
	if _, ok := p.values[name]; !ok {
		return def
	}
	b, err := p.Bool(name)
	if err != nil {
		return def
	}
	return b
}

func (p *fakeParams) Literal(name string) any {
	return p.values[name]
}

// items builds input items from plain maps.
func items(maps ...map[string]any) []domain.Item {
	out := make([]domain.Item, 0, len(maps))
	for _, m := range maps {
		out = append(out, domain.NewItem(m))
	}
	return out
}
