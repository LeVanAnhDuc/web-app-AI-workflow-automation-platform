package nodes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// routeItem runs the IF node over one item and reports which branch it took.
func routeItem(t *testing.T, combinator string, conditions []Condition, item map[string]any) bool {
	t.Helper()
	res, err := If{}.Execute(ExecContext{
		Params: params(map[string]any{"conditions": conditions, "combinator": combinator}),
		Item:   domain.NewItem(item),
	})
	require.NoError(t, err)
	// Both handles are always present; exactly one carries the item.
	onTrue, onFalse := res.Get(HandleTrue), res.Get(HandleFalse)
	require.NotNil(t, onTrue)
	require.NotNil(t, onFalse)
	require.Equal(t, 1, len(onTrue)+len(onFalse), "the item must take exactly one branch")
	return len(onTrue) == 1
}

func TestIfOperators(t *testing.T) {
	cases := []struct {
		name      string
		condition Condition
		want      bool
	}{
		{"equals", Condition{Left: "a", Operator: OpEquals, Right: "a"}, true},
		{"equals fails", Condition{Left: "a", Operator: OpEquals, Right: "b"}, false},
		{"notEquals", Condition{Left: "a", Operator: OpNotEquals, Right: "b"}, true},
		{"contains", Condition{Left: "hello world", Operator: OpContains, Right: "lo wo"}, true},
		{"notContains", Condition{Left: "hello", Operator: OpNotContains, Right: "z"}, true},
		{"startsWith", Condition{Left: "hello", Operator: OpStartsWith, Right: "hel"}, true},
		{"startsWith fails", Condition{Left: "hello", Operator: OpStartsWith, Right: "ello"}, false},
		{"endsWith", Condition{Left: "hello", Operator: OpEndsWith, Right: "llo"}, true},
		{"gt", Condition{Left: 5, Operator: OpGreater, Right: 3}, true},
		{"gt fails on equal", Condition{Left: 3, Operator: OpGreater, Right: 3}, false},
		{"gte", Condition{Left: 3, Operator: OpGreaterEq, Right: 3}, true},
		{"lt", Condition{Left: 2.5, Operator: OpLess, Right: 10}, true},
		{"lte", Condition{Left: 10, Operator: OpLessEq, Right: 10}, true},
		{"ordering falls back to text", Condition{Left: "abc", Operator: OpLess, Right: "abd"}, true},
		{"isEmpty on an empty string", Condition{Left: "", Operator: OpIsEmpty}, true},
		{"isEmpty on nil", Condition{Left: nil, Operator: OpIsEmpty}, true},
		{"isEmpty is false for zero", Condition{Left: 0, Operator: OpIsEmpty}, false},
		{"isNotEmpty", Condition{Left: "x", Operator: OpIsNotEmpty}, true},
		{"isTrue on a boolean", Condition{Left: true, Operator: OpIsTrue}, true},
		{"isTrue on the string true", Condition{Left: "true", Operator: OpIsTrue}, true},
		{"isFalse on a boolean", Condition{Left: false, Operator: OpIsFalse}, true},
		{"isFalse on zero", Condition{Left: 0, Operator: OpIsFalse}, true},
		{"regex", Condition{Left: "order-42", Operator: OpRegex, Right: `^order-\d+$`}, true},
		{"regex fails", Condition{Left: "order-x", Operator: OpRegex, Right: `^order-\d+$`}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, routeItem(t, "all", []Condition{tc.condition}, map[string]any{"id": 1}))
		})
	}
}

// TestIfNumericLeniency pins the promise that JSON's loose typing does not make
// a condition behave differently depending on which API produced the value.
func TestIfNumericLeniency(t *testing.T) {
	cases := []struct {
		name  string
		left  any
		right any
		want  bool
	}{
		{"int against string", 1, "1", true},
		{"float against string", 1.0, "1", true},
		{"float against int", 1.0, 1, true},
		{"string against float", "1.0", 1, true},
		{"different numbers", 1, "2", false},
		{"padded string", " 1 ", 1, true},
		{"non-numeric text still compares as text", "one", "one", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := routeItem(t, "all", []Condition{
				{Left: tc.left, Operator: OpEquals, Right: tc.right},
			}, map[string]any{})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIfCombinators(t *testing.T) {
	pass := Condition{Left: 1, Operator: OpEquals, Right: 1}
	fail := Condition{Left: 1, Operator: OpEquals, Right: 2}
	cases := []struct {
		name       string
		combinator string
		conditions []Condition
		want       bool
	}{
		{"all with every condition passing", "all", []Condition{pass, pass}, true},
		{"all with one failing", "all", []Condition{pass, fail}, false},
		{"any with one passing", "any", []Condition{fail, pass}, true},
		{"any with none passing", "any", []Condition{fail, fail}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, routeItem(t, tc.combinator, tc.conditions, map[string]any{}))
		})
	}
}

func TestIfRoutesTheItemItWasGiven(t *testing.T) {
	item := map[string]any{"id": 7, "status": "open"}
	res, err := If{}.Execute(ExecContext{
		Params: params(map[string]any{"conditions": []Condition{
			{Left: "open", Operator: OpEquals, Right: "open"},
		}}),
		Item:      domain.NewItem(item),
		ItemIndex: 3,
	})
	require.NoError(t, err)
	require.Len(t, res.Get(HandleTrue), 1)
	assert.Equal(t, item, res.Get(HandleTrue)[0].JSON)
	assert.Empty(t, res.Get(HandleFalse))
}

func TestIfValidationErrors(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]any
	}{
		{
			name:   "no conditions",
			values: map[string]any{"conditions": []Condition{}},
		},
		{
			name: "unknown operator",
			values: map[string]any{"conditions": []Condition{
				{Left: 1, Operator: "isRoughly", Right: 1},
			}},
		},
		{
			name: "unknown combinator",
			values: map[string]any{
				"conditions": []Condition{{Left: 1, Operator: OpEquals, Right: 1}},
				"combinator": "most",
			},
		},
		{
			name: "invalid regular expression",
			values: map[string]any{"conditions": []Condition{
				{Left: "x", Operator: OpRegex, Right: "([a-z"},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := If{}.Execute(ExecContext{Params: params(tc.values), Item: domain.NewItem(nil)})
			var nodeErr *domain.NodeError
			require.ErrorAs(t, err, &nodeErr)
			assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
		})
	}
}
