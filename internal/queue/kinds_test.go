package queue

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A worker must claim only the kinds it can run. Claiming a kind it does not
// understand takes the job away from a worker that does, which is how a job
// kind added by a newer deployment would silently vanish against an older one.
func TestClaimIgnoresOtherKinds(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	mine, err := q.Enqueue(ctx, testKindPrefix+"kinds-mine", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)
	theirs, err := q.Enqueue(ctx, testKindPrefix+"kinds-theirs", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)

	jobs, err := q.Claim(ctx, 20, testKindPrefix+"kinds-mine")
	require.NoError(t, err)

	var ids []int64
	for _, j := range jobs {
		ids = append(ids, j.ID)
	}
	assert.Contains(t, ids, mine)
	assert.NotContains(t, ids, theirs, "a filtered claim must leave another kind alone")

	// The untouched job is still pending, so its own worker can take it.
	status, _, _, _ := jobRow(t, q, theirs)
	assert.Equal(t, StatusPending, status)
}

func TestClaimAcceptsSeveralKinds(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	a, err := q.Enqueue(ctx, testKindPrefix+"kinds-a", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)
	b, err := q.Enqueue(ctx, testKindPrefix+"kinds-b", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)
	c, err := q.Enqueue(ctx, testKindPrefix+"kinds-c", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)

	jobs, err := q.Claim(ctx, 20, testKindPrefix+"kinds-a", testKindPrefix+"kinds-b")
	require.NoError(t, err)

	claimed := map[int64]bool{}
	for _, j := range jobs {
		claimed[j.ID] = true
	}
	assert.True(t, claimed[a])
	assert.True(t, claimed[b])
	assert.False(t, claimed[c])
}

// Passing no kinds keeps the original behaviour, so an operator running a single
// worker for everything does not have to enumerate them.
func TestClaimWithNoKindsTakesAnything(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	id, err := q.Enqueue(ctx, testKindPrefix+"kinds-unfiltered", ExecutionPayload{ExecutionID: uuid.NewString()})
	require.NoError(t, err)

	jobs, err := q.Claim(ctx, 50)
	require.NoError(t, err)

	var found bool
	for _, j := range jobs {
		if j.ID == id {
			found = true
		}
	}
	assert.True(t, found, "an unfiltered claim still takes every kind")

	// Hand back anything this test swept up that was not its own, so a
	// concurrently running worker is not robbed of its work.
	for _, j := range jobs {
		if j.ID != id {
			require.NoError(t, q.Fail(ctx, j.ID, errRequeued))
		}
	}
}

var errRequeued = requeueError{}

type requeueError struct{}

func (requeueError) Error() string { return "returned by TestClaimWithNoKindsTakesAnything" }
