package expr

import (
	"testing"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// testEnv is the environment every case below evaluates against: one current
// item, three input items, and one finished upstream node.
func testEnv() Env {
	return Env{
		JSON: map[string]any{
			"email": "ada@example.com",
			"name":  "Ada",
			"age":   36,
			"obj":   map[string]any{"b": "deep"},
		},
		Items: []domain.Item{
			domain.NewItem(map[string]any{"n": 1}),
			domain.NewItem(map[string]any{"n": 2}),
			domain.NewItem(map[string]any{"n": 3}),
		},
		Nodes: map[string]map[string][]domain.Item{
			"Fetch profile": {
				domain.MainHandle: {
					domain.NewItem(map[string]any{"id": 7}),
					domain.NewItem(map[string]any{"id": 8}),
				},
			},
		},
		ItemIndex:   2,
		Now:         time.Date(2026, 8, 18, 9, 30, 0, 0, time.UTC),
		ExecutionID: "exec-1",
	}
}

func TestEvaluateReturnsTypedValueForSingleExpression(t *testing.T) {
	got, err := Evaluate("{{ 1 + 2 }}", testEnv())
	mustNoError(t, err, "evaluate")
	mustEqual(t, got, 3, "a lone expression keeps its type")
	if _, isString := got.(string); isString {
		t.Fatalf("a lone expression must not be stringified")
	}
}

func TestEvaluateInterpolatesTwoExpressions(t *testing.T) {
	got, err := Evaluate("Hi {{ $json.name }}, you are {{ $json.age }}", testEnv())
	mustNoError(t, err, "evaluate")
	mustEqual(t, got, "Hi Ada, you are 36", "interpolation")
}

func TestEvaluateNames(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want any
	}{
		{"json field", "{{ $json.email }}", "ada@example.com"},
		{"nested json field", "{{ $json.obj.b }}", "deep"},
		{"node json", `{{ $node["Fetch profile"].json.id }}`, 7},
		{"node items", `{{ len($node["Fetch profile"].items) }}`, 2},
		{"items length", "{{ len($items) }}", 3},
		{"item by index", "{{ $items[1].n }}", 2},
		{"item index", "{{ $itemIndex }}", 2},
		{"now", "{{ $now }}", "2026-08-18T09:30:00Z"},
		{"execution id", "{{ $execution.id }}", "exec-1"},
		{"missing key is nil", "{{ $json.nope }}", nil},
		{"brace inside a string literal", `{{ "}" }}`, "}"},
		{"no expression at all", "plain text", "plain text"},
		{"empty source", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Evaluate(tc.src, testEnv())
			mustNoError(t, err, tc.src)
			mustEqual(t, got, tc.want, tc.src)
		})
	}
}

func TestEvaluateMissingKeyInterpolatesToEmptyString(t *testing.T) {
	got, err := Evaluate("name={{ $json.nope }}!", testEnv())
	mustNoError(t, err, "evaluate")
	mustEqual(t, got, "name=!", "nil interpolates to nothing")
}

func TestEvaluateNilJSONIsAnEmptyMap(t *testing.T) {
	// A node running with no input still evaluates its parameters, so $json has
	// to behave like an empty object rather than blowing up.
	got, err := Evaluate("{{ $json.anything }}", Env{})
	mustNoError(t, err, "evaluate with a zero env")
	mustEqual(t, got, nil, "missing key on an empty $json")
}

func TestEvaluateUnclosedTemplateIsAnExpressionError(t *testing.T) {
	_, err := Evaluate("Hello {{ $json.name", testEnv())
	mustError(t, err, "unclosed template")
	ne := domain.AsNodeError(err)
	mustEqual(t, ne.Code, domain.ErrCodeExpression, "error code")
	mustContain(t, ne.Message, "unclosed", "message")
}

func TestEvaluateCompileErrorNamesTheExpression(t *testing.T) {
	_, err := Evaluate("{{ 1 + + }}", testEnv())
	mustError(t, err, "syntax error")
	ne := domain.AsNodeError(err)
	mustEqual(t, ne.Code, domain.ErrCodeExpression, "error code")
	mustContain(t, ne.Message, "1 + +", "the user must see which expression broke")
}

func TestEvaluateRuntimeErrorNamesTheExpression(t *testing.T) {
	// Reaching through a missing key is deliberately tolerated now, so the
	// runtime error used here has to be a real mistake: arithmetic on a value
	// that is not there.
	_, err := Evaluate("{{ $json.nope.deeper + 1 }}", testEnv())
	mustError(t, err, "runtime error")
	ne := domain.AsNodeError(err)
	mustEqual(t, ne.Code, domain.ErrCodeExpression, "error code")
	mustContain(t, ne.Message, "$json.nope.deeper + 1", "message names the expression")
}

func TestEvaluateStringStringifies(t *testing.T) {
	env := testEnv()
	cases := []struct{ src, want string }{
		{"{{ 1 + 2 }}", "3"},
		{"{{ 3.0 }}", "3"},
		{"{{ 2.5 }}", "2.5"},
		{"{{ true }}", "true"},
		{"{{ $json.nope }}", ""},
		{"{{ $json.obj }}", `{"b":"deep"}`},
		{"{{ $items[0] }}", `{"n":1}`},
		{"literal", "literal"},
	}
	for _, tc := range cases {
		got, err := EvaluateString(tc.src, env)
		mustNoError(t, err, tc.src)
		mustEqual(t, got, tc.want, tc.src)
	}
}

func TestHasExpression(t *testing.T) {
	mustEqual(t, HasExpression("a {{ $json.b }}"), true, "template")
	mustEqual(t, HasExpression("a literal"), false, "literal")
	mustEqual(t, HasExpression(""), false, "empty")
}

func TestStringify(t *testing.T) {
	mustEqual(t, Stringify(nil), "", "nil")
	mustEqual(t, Stringify(float64(42)), "42", "integral float loses its .0")
	mustEqual(t, Stringify(42), "42", "int")
	mustEqual(t, Stringify(-1.25), "-1.25", "fractional float")
	mustEqual(t, Stringify(false), "false", "bool")
	mustEqual(t, Stringify("s"), "s", "string")
	mustEqual(t, Stringify([]any{1, 2}), "[1,2]", "slice as compact JSON")
	mustEqual(t, Stringify(time.Date(2026, 8, 18, 9, 30, 0, 0, time.UTC)),
		"2026-08-18T09:30:00Z", "time")
}
