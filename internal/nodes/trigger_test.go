package nodes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// TestTriggersFallBackToOneEmptyItem pins the rule the whole engine relies on: a
// trigger never returns nothing, because empty input makes the engine skip every
// downstream node and a run with no payload would do nothing at all.
func TestTriggersFallBackToOneEmptyItem(t *testing.T) {
	triggers := []struct {
		name string
		node Node
	}{
		{"manual", ManualTrigger{}},
		{"webhook", WebhookTrigger{}},
	}
	for _, tc := range triggers {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.node.Execute(ExecContext{Params: params(nil)})
			require.NoError(t, err)
			out := res.Get(HandleMain)
			require.Len(t, out, 1)
			assert.Equal(t, map[string]any{}, out[0].JSON)
		})
	}
}

func TestTriggersPassThroughTheirPayload(t *testing.T) {
	payload := items(
		map[string]any{"headers": map[string]any{"x": "1"}, "body": map[string]any{"id": 1}},
		map[string]any{"body": map[string]any{"id": 2}},
	)
	triggers := []struct {
		name        string
		node        Node
		triggerType domain.TriggerType
	}{
		{"manual", ManualTrigger{}, domain.TriggerManual},
		{"webhook", WebhookTrigger{}, domain.TriggerWebhook},
	}
	for _, tc := range triggers {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.node.Execute(ExecContext{
				Params:  params(nil),
				Trigger: TriggerPayload{Type: tc.triggerType, Items: payload},
			})
			require.NoError(t, err)
			assert.Equal(t, payload, res.Get(HandleMain))
		})
	}
}

func TestScheduleTriggerTimestamp(t *testing.T) {
	t.Run("uses the due time the worker supplied", func(t *testing.T) {
		due := "2026-08-18T09:00:00Z"
		res, err := ScheduleTrigger{}.Execute(ExecContext{
			Params:  params(nil),
			Trigger: TriggerPayload{Type: domain.TriggerSchedule, Items: items(map[string]any{"timestamp": due})},
		})
		require.NoError(t, err)
		out := res.Get(HandleMain)
		require.Len(t, out, 1)
		assert.Equal(t, map[string]any{"timestamp": due}, out[0].JSON)
	})

	t.Run("falls back to now when there is none", func(t *testing.T) {
		res, err := ScheduleTrigger{}.Execute(ExecContext{Params: params(nil)})
		require.NoError(t, err)
		out := res.Get(HandleMain)
		require.Len(t, out, 1)
		stamp, ok := out[0].JSON["timestamp"].(string)
		require.True(t, ok)
		parsed, err := time.Parse(time.RFC3339, stamp)
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().UTC(), parsed, time.Minute)
	})
}
