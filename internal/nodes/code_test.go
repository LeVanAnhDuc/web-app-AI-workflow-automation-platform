package nodes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// runCode executes the code node with the given snippet, mode and input.
func runCode(code, mode string, timeoutMs int, in []domain.Item) (Result, error) {
	return Code{}.Execute(ExecContext{
		Node:   domain.GraphNode{Name: "Code", Settings: domain.NodeSettings{TimeoutMs: timeoutMs}},
		Params: params(map[string]any{"jsCode": code, "mode": mode}),
		Items:  in,
	})
}

func TestCodeReturnShapes(t *testing.T) {
	in := items(map[string]any{"n": 1}, map[string]any{"n": 2})
	cases := []struct {
		name string
		code string
		mode string
		want []map[string]any
	}{
		{
			name: "the default snippet passes items through",
			code: defaultJSCode,
			mode: "allItems",
			want: []map[string]any{{"n": float64(1)}, {"n": float64(2)}},
		},
		{
			name: "an array of json wrappers",
			code: `return items.map(function (i) { return { json: { n: i.json.n * 2 } }; });`,
			mode: "allItems",
			want: []map[string]any{{"n": float64(2)}, {"n": float64(4)}},
		},
		{
			name: "an array of plain objects",
			code: `return items.map(function (i) { return { doubled: i.json.n * 2 }; });`,
			mode: "allItems",
			want: []map[string]any{{"doubled": float64(2)}, {"doubled": float64(4)}},
		},
		{
			name: "a single object becomes one item",
			code: `return { total: items.length };`,
			mode: "allItems",
			want: []map[string]any{{"total": float64(2)}},
		},
		{
			name: "eachItem runs once per item with item and index in scope",
			code: `return { n: item.json.n, index: index };`,
			mode: "eachItem",
			want: []map[string]any{
				{"n": float64(1), "index": float64(0)},
				{"n": float64(2), "index": float64(1)},
			},
		},
		{
			name: "eachItem accepts the wrapper shape too",
			code: `return { json: { n: item.json.n } };`,
			mode: "eachItem",
			want: []map[string]any{{"n": float64(1)}, {"n": float64(2)}},
		},
		{
			name: "console.log does not affect the output",
			code: `console.log("seen", items.length, { a: 1 }); return items;`,
			mode: "allItems",
			want: []map[string]any{{"n": float64(1)}, {"n": float64(2)}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := runCode(tc.code, tc.mode, 5000, in)
			require.NoError(t, err)
			out := res.Get(HandleMain)
			require.Len(t, out, len(tc.want))
			for i, want := range tc.want {
				assert.Equal(t, want, out[i].JSON)
			}
		})
	}
}

func TestCodeScriptErrors(t *testing.T) {
	in := items(map[string]any{"n": 1})
	cases := []struct {
		name        string
		code        string
		wantMessage string
	}{
		{
			name:        "a thrown error",
			code:        `throw new Error("boom");`,
			wantMessage: "boom",
		},
		{
			name:        "a syntax error",
			code:        `return (;`,
			wantMessage: "SyntaxError",
		},
		{
			name:        "returning a number",
			code:        `return 42;`,
			wantMessage: "return an array of objects",
		},
		{
			name:        "returning nothing",
			code:        `var unused = 1;`,
			wantMessage: "returned nothing",
		},
		{
			name:        "an array of non-objects",
			code:        `return [1, 2];`,
			wantMessage: "element 0 of the returned array is a number",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runCode(tc.code, "allItems", 5000, in)
			var nodeErr *domain.NodeError
			require.ErrorAs(t, err, &nodeErr)
			assert.Equal(t, domain.ErrCodeScript, nodeErr.Code)
			assert.Contains(t, nodeErr.Message, tc.wantMessage)
		})
	}
}

func TestCodeRequiresASnippet(t *testing.T) {
	_, err := runCode("   \n", "allItems", 5000, nil)
	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
}

func TestCodeRejectsAnUnknownMode(t *testing.T) {
	_, err := runCode(`return items;`, "everyOtherItem", 5000, nil)
	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
}

// TestCodeTimesOutOnAnInfiniteLoop is the reason the node arms a watchdog: goja
// has no instruction budget, so without the interrupt this test would hang.
func TestCodeTimesOutOnAnInfiniteLoop(t *testing.T) {
	_, err := runCode(`while (true) {}`, "allItems", 200, items(map[string]any{}))

	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeScript, nodeErr.Code)
	assert.Contains(t, nodeErr.Message, "timeout")
}
