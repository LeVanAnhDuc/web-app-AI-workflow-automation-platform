package nodes

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/llm"
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

/* ------------------------------ language model ----------------------------- */

// fakeProvider is an llm.Provider that answers from a queued script and records
// every request. The AI nodes must never reach a real provider in a test, so
// this is the only implementation they are given.
type fakeProvider struct {
	// queue is consumed one response per call. When it runs out, last is
	// repeated, which is how an agent that never stops asking for tools is
	// scripted without listing a response per turn.
	queue []llm.Response
	last  *llm.Response

	// err, when set, is returned instead of a response.
	err error

	mu       sync.Mutex
	requests []llm.Request
}

// scripted builds a provider that returns the given responses in order.
func scripted(responses ...llm.Response) *fakeProvider {
	return &fakeProvider{queue: responses}
}

// repeating builds a provider that returns the same response for every call.
func repeating(resp llm.Response) *fakeProvider {
	return &fakeProvider{last: &resp}
}

// failing builds a provider whose calls all fail.
func failing(err error) *fakeProvider {
	return &fakeProvider{err: err}
}

func (f *fakeProvider) Name() string     { return "fake" }
func (f *fakeProvider) Models() []string { return []string{"fake-model"} }

func (f *fakeProvider) Chat(ctx context.Context, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	// The messages slice is copied because the agent keeps appending to its own
	// copy between turns, and a recorded request must stay a snapshot.
	snapshot := req
	snapshot.Messages = append([]llm.Message(nil), req.Messages...)
	f.requests = append(f.requests, snapshot)

	if f.err != nil {
		err := f.err
		f.mu.Unlock()
		return llm.Response{}, err
	}

	var resp llm.Response
	switch {
	case len(f.queue) > 0:
		resp, f.queue = f.queue[0], f.queue[1:]
	case f.last != nil:
		resp = *f.last
	default:
		f.mu.Unlock()
		return llm.Response{}, fmt.Errorf("fakeProvider: unexpected call with no scripted response")
	}
	f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return llm.Response{}, err
	}
	return resp, nil
}

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// request returns the i-th recorded request.
func (f *fakeProvider) request(i int) llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[i]
}

// registry wraps the provider as the registry a node reads.
func (f *fakeProvider) registry() *llm.Registry { return llm.NewRegistry(f) }
