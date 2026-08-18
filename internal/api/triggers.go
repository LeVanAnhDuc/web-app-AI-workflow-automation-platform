package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/schedule"
)

// The trigger node types the API has to understand, because activating a
// workflow turns them into rows other processes watch.
const (
	nodeTypeManualTrigger   = "trigger.manual"
	nodeTypeWebhookTrigger  = "trigger.webhook"
	nodeTypeScheduleTrigger = "trigger.schedule"
)

// reconcileTriggers makes the webhooks and schedules tables match reality: the
// active version's trigger nodes when the workflow is active, and nothing at
// all when it is not. Deriving them rather than mutating them on every edit
// means a half-finished edit can never leave a stale public URL live.
func (s *server) reconcileTriggers(ctx context.Context, workspaceID string, wf domain.Workflow) error {
	if !wf.Active {
		if err := s.store.ReplaceWebhooks(ctx, workspaceID, wf.ID, nil); err != nil {
			return err
		}
		return s.store.ReplaceSchedules(ctx, workspaceID, wf.ID, nil)
	}

	version, err := s.store.LatestVersion(ctx, workspaceID, wf.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}

	hooks, schedules, err := triggersFromGraph(workspaceID, wf.ID, version.Graph, time.Now())
	if err != nil {
		return err
	}
	if err := s.store.ReplaceWebhooks(ctx, workspaceID, wf.ID, hooks); err != nil {
		return err
	}
	return s.store.ReplaceSchedules(ctx, workspaceID, wf.ID, schedules)
}

// triggersFromGraph derives the trigger rows a graph implies. It is a pure
// function so the mapping is unit-testable without a database.
func triggersFromGraph(workspaceID, workflowID string, g domain.Graph, now time.Time) ([]domain.Webhook, []domain.Schedule, error) {
	var hooks []domain.Webhook
	var schedules []domain.Schedule

	for _, n := range g.Nodes {
		if n.Disabled {
			continue
		}
		switch n.Type {
		case nodeTypeWebhookTrigger:
			path := strings.Trim(strings.TrimSpace(stringParam(n.Params, "path")), "/")
			if path == "" {
				return nil, nil, fmt.Errorf("webhook trigger %q has no path", n.Name)
			}
			method := strings.ToUpper(stringParam(n.Params, "method"))
			if method == "" {
				method = "POST"
			}
			hooks = append(hooks, domain.Webhook{
				WorkspaceID: workspaceID,
				WorkflowID:  workflowID,
				NodeID:      n.ID,
				Path:        path,
				Method:      method,
			})

		case nodeTypeScheduleTrigger:
			spec := strings.TrimSpace(stringParam(n.Params, "cron"))
			if spec == "" {
				return nil, nil, fmt.Errorf("schedule trigger %q has no cron expression", n.Name)
			}
			tz := strings.TrimSpace(stringParam(n.Params, "timezone"))
			if tz == "" {
				tz = "UTC"
			}
			next, err := schedule.NextRun(spec, tz, now)
			if err != nil {
				return nil, nil, fmt.Errorf("schedule trigger %q: %w", n.Name, err)
			}
			schedules = append(schedules, domain.Schedule{
				WorkspaceID: workspaceID,
				WorkflowID:  workflowID,
				NodeID:      n.ID,
				Cron:        spec,
				Timezone:    tz,
				NextRunAt:   next,
			})
		}
	}
	return hooks, schedules, nil
}

func stringParam(params map[string]any, name string) string {
	if params == nil {
		return ""
	}
	v, _ := params[name].(string)
	return v
}
