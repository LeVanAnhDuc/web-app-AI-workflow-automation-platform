package nodes

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

func TestSet(t *testing.T) {
	cases := []struct {
		name   string
		mode   string
		fields []KeyValue
		input  map[string]any
		want   map[string]any
	}{
		{
			name:   "merge keeps the input and adds a field",
			mode:   "merge",
			fields: []KeyValue{{Key: "stage", Value: "new"}},
			input:  map[string]any{"id": 1, "name": "Ada"},
			want:   map[string]any{"id": 1, "name": "Ada", "stage": "new"},
		},
		{
			name:   "merge overwrites an existing key",
			mode:   "merge",
			fields: []KeyValue{{Key: "name", Value: "Grace"}},
			input:  map[string]any{"id": 1, "name": "Ada"},
			want:   map[string]any{"id": 1, "name": "Grace"},
		},
		{
			name:   "keepOnly drops everything else",
			mode:   "keepOnly",
			fields: []KeyValue{{Key: "name", Value: "Ada"}},
			input:  map[string]any{"id": 1, "name": "Ada", "secret": "x"},
			want:   map[string]any{"name": "Ada"},
		},
		{
			name:   "an empty field name is skipped",
			mode:   "merge",
			fields: []KeyValue{{Key: "", Value: "ignored"}, {Key: "ok", Value: "yes"}},
			input:  map[string]any{"id": 1},
			want:   map[string]any{"id": 1, "ok": "yes"},
		},
		{
			name:   "a dotted name stays one literal key",
			mode:   "keepOnly",
			fields: []KeyValue{{Key: "user.name", Value: "Ada"}},
			input:  map[string]any{},
			want:   map[string]any{"user.name": "Ada"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := domain.NewItem(tc.input)
			before := maps.Clone(tc.input)
			res, err := Set{}.Execute(ExecContext{
				Params: params(map[string]any{"mode": tc.mode, "fields": tc.fields}),
				Item:   input,
			})
			require.NoError(t, err)
			out := res.Get(HandleMain)
			require.Len(t, out, 1)
			assert.Equal(t, tc.want, out[0].JSON)
			// The engine persists the input item, so the node must not mutate it.
			assert.Equal(t, before, input.JSON)
		})
	}
}

func TestSetRejectsAnUnknownMode(t *testing.T) {
	_, err := Set{}.Execute(ExecContext{
		Params: params(map[string]any{"mode": "replaceAll", "fields": []KeyValue{{Key: "a", Value: "b"}}}),
		Item:   domain.NewItem(nil),
	})
	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
}
