package store

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

func TestCursorRoundTrip(t *testing.T) {
	id := uuid.NewString()
	at := time.Date(2026, 8, 18, 9, 30, 15, 123456789, time.UTC)

	gotAt, gotID, err := decodeCursor(encodeCursor(at, id))
	require.NoError(t, err)
	assert.Equal(t, id, gotID)
	assert.True(t, at.Equal(gotAt), "want %s, got %s", at, gotAt)
}

func TestCursorNormalisesToUTC(t *testing.T) {
	id := uuid.NewString()
	at := time.Date(2026, 8, 18, 16, 30, 0, 0, time.FixedZone("ICT", 7*3600))

	gotAt, _, err := decodeCursor(encodeCursor(at, id))
	require.NoError(t, err)
	assert.Equal(t, time.UTC, gotAt.Location())
	assert.True(t, at.Equal(gotAt))
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	encoded := func(raw string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(raw))
	}
	for name, cursor := range map[string]string{
		"not base64":     "!!!!",
		"no separator":   encoded("2026-08-18T00:00:00Z"),
		"bad timestamp":  encoded("not-a-time|" + uuid.NewString()),
		"id not a uuid":  encoded("2026-08-18T00:00:00Z|nope"),
		"empty sections": encoded("|"),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := decodeCursor(cursor)
			assert.Error(t, err)
		})
	}
}

func TestJSONBOrNullMapsNilToSQLNull(t *testing.T) {
	var (
		nilItems   []domain.Item
		nilOutputs map[string][]domain.Item
		nilErr     *domain.NodeError
	)
	for name, value := range map[string]any{
		"nil slice":   nilItems,
		"nil map":     nilOutputs,
		"nil pointer": nilErr,
		"untyped nil": nil,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := jsonbOrNull(value)
			require.NoError(t, err)
			assert.Nil(t, got, "a nil Go value must become SQL NULL, not the literal null")
		})
	}
}

func TestJSONBOrNullEncodesValues(t *testing.T) {
	got, err := jsonbOrNull([]domain.Item{domain.NewItem(map[string]any{"a": 1})})
	require.NoError(t, err)
	assert.Equal(t, `[{"json":{"a":1}}]`, string(got.([]byte)))

	// An empty but non-nil collection is data, not absence.
	got, err = jsonbOrNull([]domain.Item{})
	require.NoError(t, err)
	assert.Equal(t, `[]`, string(got.([]byte)))
}

func TestDecodeJSONBColumnsTolerateNull(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("null")} {
		ne, err := nodeErrorFromJSON(raw)
		require.NoError(t, err)
		assert.Nil(t, ne)

		out, err := outputsFromJSON(raw)
		require.NoError(t, err)
		assert.Nil(t, out)
	}

	ne, err := nodeErrorFromJSON([]byte(`{"code":"http_error","message":"boom"}`))
	require.NoError(t, err)
	require.NotNil(t, ne)
	assert.Equal(t, domain.ErrCodeHTTP, ne.Code)

	out, err := outputsFromJSON([]byte(`{"main":[{"json":{"ok":true}}]}`))
	require.NoError(t, err)
	assert.Len(t, out[domain.MainHandle], 1)
}

func TestTriggerFromNodeType(t *testing.T) {
	cases := map[string]domain.TriggerType{
		"trigger.webhook":  domain.TriggerWebhook,
		"trigger.schedule": domain.TriggerSchedule,
		"trigger.manual":   domain.TriggerManual,
		"":                 domain.TriggerManual,
		"http.request":     domain.TriggerManual,
	}
	for nodeType, want := range cases {
		assert.Equal(t, want, triggerFromNodeType(nodeType), nodeType)
	}
}

func TestWorkflowOrderDefaultsToUpdated(t *testing.T) {
	assert.Equal(t, workflowOrder(domain.SortUpdated), workflowOrder(""))
	assert.Contains(t, workflowOrder(domain.SortName), "lower(w.name)")
	assert.Contains(t, workflowOrder(domain.SortCreated), "w.created_at")
}

func TestValidIDs(t *testing.T) {
	assert.True(t, validIDs(uuid.NewString(), uuid.NewString()))
	assert.False(t, validIDs(uuid.NewString(), "../../etc/passwd"))
	assert.False(t, validIDs(""))
}

func TestPrefixedQualifiesEveryColumn(t *testing.T) {
	assert.Equal(t, "v.id, v.version", prefixed("v", "id, version"))
}
