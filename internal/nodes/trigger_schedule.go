package nodes

import (
	"time"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// ScheduleTrigger starts a workflow on a cron schedule. The worker's ticker
// owns the clock and the next-run bookkeeping; this node only reports when the
// run it belongs to was due.
type ScheduleTrigger struct{}

// Descriptor implements Node.
func (ScheduleTrigger) Descriptor() Descriptor {
	return Descriptor{
		Type:        "trigger.schedule",
		Name:        "Schedule",
		Category:    CategoryTrigger,
		Description: "Starts the workflow on a repeating cron schedule.",
		Icon:        "clock",
		Mode:        ModeOnce,
		Inputs:      nil,
		Outputs:     MainOut,
		IsTrigger:   true,
		Params: []ParamSpec{
			{
				Name:        "cron",
				Label:       "Cron Expression",
				Type:        ParamString,
				Default:     "0 9 * * *",
				Required:    true,
				Placeholder: "0 9 * * *",
				Description: "Five fields, minute hour day-of-month month day-of-week. " +
					"\"0 9 * * *\" is every day at 09:00, \"*/15 * * * *\" every fifteen " +
					"minutes, \"0 9 * * 1-5\" weekdays at 09:00.",
			},
			{
				Name:        "timezone",
				Label:       "Timezone",
				Type:        ParamString,
				Default:     "UTC",
				Placeholder: "UTC",
				Description: "IANA timezone the cron expression is interpreted in, " +
					"for example Asia/Ho_Chi_Minh.",
			},
		},
	}
}

// Execute emits the single timestamp item downstream nodes date their work
// from. The worker supplies the due time as trigger data so a job that waited
// in the queue still reports the moment it was scheduled for, not the moment it
// happened to run; a test run from the editor has no such time and uses now.
func (ScheduleTrigger) Execute(ec ExecContext) (Result, error) {
	ts := time.Now().UTC().Format(time.RFC3339)
	if len(ec.Trigger.Items) > 0 {
		if supplied, ok := ec.Trigger.Items[0].JSON["timestamp"].(string); ok && supplied != "" {
			ts = supplied
		}
	}
	return Main(domain.NewItem(map[string]any{"timestamp": ts})), nil
}
