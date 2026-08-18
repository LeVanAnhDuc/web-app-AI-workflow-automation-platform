package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// A brand-new workflow has an empty graph, and rows written before the API
// guaranteed JSON arrays hold {"nodes": null}. Both must summarise as zero
// nodes: jsonb_array_length and jsonb_array_elements raise 22023 on a
// non-array, which used to make the whole workflow list 500.
func TestWorkflowSummaryHandlesAnEmptyGraph(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Freshly created")
	require.NoError(t, err)
	_, err = s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, domain.Graph{}, &user.ID)
	require.NoError(t, err)

	summaries, err := s.ListWorkflowSummaries(ctx, ws.ID, domain.WorkflowFilter{})
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	got := summaries[0]
	assert.Equal(t, 0, got.NodeCount)
	// A graph with no trigger node still reports "manual": the list column
	// always needs a value, and running it by hand is the only thing that
	// workflow can do.
	assert.Equal(t, domain.TriggerManual, got.TriggerType)
	assert.Empty(t, got.TriggerDetail)
	assert.Nil(t, got.LastRun)
	assert.Nil(t, got.SuccessRate7d, "no runs means no rate, which is not the same as zero")
}

func TestWorkflowSummaryHandlesALegacyNullNodesGraph(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Written before the array guarantee")
	require.NoError(t, err)
	version, err := s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, domain.Graph{}, &user.ID)
	require.NoError(t, err)

	// Reproduce exactly what the old encoder stored, which no Go value can now
	// produce — hence writing the column directly.
	_, err = s.Pool().Exec(ctx,
		`UPDATE workflow_versions SET graph = '{"nodes": null, "edges": null}'::jsonb WHERE id = $1`,
		version.ID)
	require.NoError(t, err)

	summaries, err := s.ListWorkflowSummaries(ctx, ws.ID, domain.WorkflowFilter{})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, 0, summaries[0].NodeCount)
}

func TestWorkflowSummarySurvivesAMissingNodesKey(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Hand-edited graph")
	require.NoError(t, err)
	version, err := s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, domain.Graph{}, &user.ID)
	require.NoError(t, err)

	_, err = s.Pool().Exec(ctx,
		`UPDATE workflow_versions SET graph = '{}'::jsonb WHERE id = $1`, version.ID)
	require.NoError(t, err)

	summaries, err := s.ListWorkflowSummaries(ctx, ws.ID, domain.WorkflowFilter{})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, 0, summaries[0].NodeCount)
}

// A workflow with no versions at all still belongs on the list: the screen must
// show it so the user can open it and start building.
func TestWorkflowSummaryIncludesAWorkflowWithNoVersion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, _ := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Never saved")
	require.NoError(t, err)

	summaries, err := s.ListWorkflowSummaries(ctx, ws.ID, domain.WorkflowFilter{})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, wf.ID, summaries[0].ID)
	assert.Equal(t, 0, summaries[0].NodeCount)
}

// An empty graph must also round-trip as arrays, so the editor never has to
// guard before iterating.
func TestEmptyGraphRoundTripsAsArrays(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ws, user := newWorkspace(t, s)

	wf, err := s.CreateWorkflow(ctx, ws.ID, "Empty")
	require.NoError(t, err)
	_, err = s.CreateWorkflowVersion(ctx, ws.ID, wf.ID, domain.Graph{}, &user.ID)
	require.NoError(t, err)

	got, err := s.LatestVersion(ctx, ws.ID, wf.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Graph.Nodes)
	assert.Empty(t, got.Graph.Edges)
}
