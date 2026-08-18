package nodes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

func TestMerge(t *testing.T) {
	cases := []struct {
		name   string
		mode   string
		first  []domain.Item
		second []domain.Item
		want   []map[string]any
	}{
		{
			name:   "append puts input 1 before input 2",
			mode:   "append",
			first:  items(map[string]any{"a": 1}),
			second: items(map[string]any{"b": 2}, map[string]any{"b": 3}),
			want:   []map[string]any{{"a": 1}, {"b": 2}, {"b": 3}},
		},
		{
			name:   "append with one side empty",
			mode:   "append",
			first:  nil,
			second: items(map[string]any{"b": 2}),
			want:   []map[string]any{{"b": 2}},
		},
		{
			name:   "combineByPosition merges matching positions",
			mode:   "combineByPosition",
			first:  items(map[string]any{"id": 1, "name": "Ada"}, map[string]any{"id": 2}),
			second: items(map[string]any{"email": "ada@x.io"}, map[string]any{"email": "b@x.io"}),
			want: []map[string]any{
				{"id": 1, "name": "Ada", "email": "ada@x.io"},
				{"id": 2, "email": "b@x.io"},
			},
		},
		{
			name:   "combineByPosition lets input 2 win a collision",
			mode:   "combineByPosition",
			first:  items(map[string]any{"name": "Ada", "keep": true}),
			second: items(map[string]any{"name": "Grace"}),
			want:   []map[string]any{{"name": "Grace", "keep": true}},
		},
		{
			name:   "combineByPosition keeps the longer side intact",
			mode:   "combineByPosition",
			first:  items(map[string]any{"a": 1}, map[string]any{"a": 2}, map[string]any{"a": 3}),
			second: items(map[string]any{"b": 1}),
			want:   []map[string]any{{"a": 1, "b": 1}, {"a": 2}, {"a": 3}},
		},
		{
			name:   "combineByPosition when input 1 is the shorter side",
			mode:   "combineByPosition",
			first:  items(map[string]any{"a": 1}),
			second: items(map[string]any{"b": 1}, map[string]any{"b": 2}),
			want:   []map[string]any{{"a": 1, "b": 1}, {"b": 2}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Merge{}.Execute(ExecContext{
				Params: params(map[string]any{"mode": tc.mode}),
				Inputs: map[string][]domain.Item{
					HandleInput1: tc.first,
					HandleInput2: tc.second,
				},
			})
			require.NoError(t, err)
			out := res.Get(HandleMain)
			require.Len(t, out, len(tc.want))
			for i, want := range tc.want {
				assert.Equal(t, want, out[i].JSON)
			}
		})
	}
}

func TestMergeRejectsAnUnknownMode(t *testing.T) {
	_, err := Merge{}.Execute(ExecContext{
		Params: params(map[string]any{"mode": "chooseOne"}),
		Inputs: map[string][]domain.Item{},
	})
	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
}
