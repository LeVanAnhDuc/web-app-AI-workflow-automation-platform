package expr

import (
	"testing"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

func TestPreprocessMakesDataChainsOptional(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"plain chain", "$json.body.company", "_dollar_json?.body?.company"},
		{"node lookup", `$node["Fetch profile"].json.id`, `_dollar_node["Fetch profile"]?.json?.id`},
		{"index then field", "$items[0].json.email", "_dollar_items[0]?.json?.email"},
		{"two chains", "$json.a + $json.b", "_dollar_json?.a + _dollar_json?.b"},
		{"bare name", "$itemIndex", "_dollar_itemIndex"},
		{"inside a call", "len($json.list)", "len(_dollar_json?.list)"},
		{"piped", "$json.items | len", "_dollar_json?.items | len"},
		{"trailing subscript", "$json.tags[0]", "_dollar_json?.tags[0]"},

		// The three cases that must survive untouched.
		{"dollar in a double-quoted string", `"total: $5"`, `"total: $5"`},
		{"dollar in a single-quoted string", `'$json'`, `'$json'`},
		{"dollar in a backtick string", "`$json.body`", "`$json.body`"},
		{"a plain number keeps its dot", "1.5 + $json.n", "1.5 + _dollar_json?.n"},

		// An author who already wrote optional chaining keeps exactly that.
		{"explicit optional chaining", "$json?.body?.company", "_dollar_json?.body?.company"},

		// A dot on something that is not a $-chain is left alone, so a function
		// result's own members are not silently made optional.
		{"non-dollar chain", "upper(name).length", "upper(name).length"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Preprocess(tc.src); got != tc.want {
				t.Fatalf("Preprocess(%q)\n got: %s\nwant: %s", tc.src, got, tc.want)
			}
		})
	}
}

// The reason optional chaining is here at all: workflow data is shaped by
// whoever sends it, and an absent optional field must not fail the run.
func TestEvaluateReturnsNilForAMissingIntermediateKey(t *testing.T) {
	env := Env{
		JSON: map[string]any{"email": "ha@acme.vn"},
		Now:  time.Now(),
	}

	got, err := Evaluate("{{ $json.body.company }}", env)
	if err != nil {
		t.Fatalf("a missing intermediate key must not be an error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestEvaluateStringRendersAMissingKeyAsEmpty(t *testing.T) {
	env := Env{JSON: map[string]any{"email": "ha@acme.vn"}}

	got, err := EvaluateString("company: {{ $json.body.company }}", env)
	if err != nil {
		t.Fatalf("EvaluateString: %v", err)
	}
	if got != "company: " {
		t.Fatalf("got %q, want the absent value rendered as empty", got)
	}
}

func TestEvaluateStillResolvesDeepPresentPaths(t *testing.T) {
	env := Env{
		JSON: map[string]any{"body": map[string]any{"company": "acme.vn"}},
	}

	got, err := Evaluate("{{ $json.body.company }}", env)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got != "acme.vn" {
		t.Fatalf("got %v, want acme.vn", got)
	}
}

func TestEvaluateReturnsNilForAMissingNode(t *testing.T) {
	env := Env{Nodes: map[string]map[string][]domain.Item{}}

	got, err := Evaluate(`{{ $node["Never ran"].json.id }}`, env)
	if err != nil {
		t.Fatalf("referencing a node that has not run must not be an error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

// A real mistake still has to be reported, or the leniency would hide typos in
// the expression language itself rather than in the data.
func TestEvaluateStillReportsASyntaxError(t *testing.T) {
	_, err := Evaluate("{{ $json.a + }}", Env{JSON: map[string]any{}})
	if err == nil {
		t.Fatal("expected a syntax error")
	}
	if ne := domain.AsNodeError(err); ne.Code != domain.ErrCodeExpression {
		t.Fatalf("code %q, want %q", ne.Code, domain.ErrCodeExpression)
	}
}

func TestEvaluateStillReportsAnUnknownFunction(t *testing.T) {
	_, err := Evaluate("{{ notAFunction($json.a) }}", Env{JSON: map[string]any{}})
	if err == nil {
		t.Fatal("expected an error for an unknown function")
	}
}

func TestArithmeticOnAMissingValueIsStillAnError(t *testing.T) {
	// nil + 1 is a genuine mistake in the expression, not absent data, so it
	// must not be silently swallowed.
	if _, err := Evaluate("{{ $json.missing + 1 }}", Env{JSON: map[string]any{}}); err == nil {
		t.Fatal("expected an error for arithmetic on a missing value")
	}
}
