package nodes

import "github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"

// ManualTrigger starts a workflow from the editor's Run button. It has no
// parameters: everything it emits comes from the run request's payload.
type ManualTrigger struct{}

// Descriptor implements Node.
func (ManualTrigger) Descriptor() Descriptor {
	return Descriptor{
		Type:        "trigger.manual",
		Name:        "Manual Trigger",
		Category:    CategoryTrigger,
		Description: "Starts the workflow when you press Run in the editor.",
		Icon:        "bolt",
		Mode:        ModeOnce,
		Inputs:      nil,
		Outputs:     MainOut,
		IsTrigger:   true,
	}
}

// Execute emits the run payload. A run started with no data still has to
// produce one item, otherwise the engine would treat every downstream node as
// having empty input and skip the whole workflow.
func (ManualTrigger) Execute(ec ExecContext) (Result, error) {
	return MainSlice(triggerItemsOrEmpty(ec.Trigger.Items)), nil
}

// triggerItemsOrEmpty falls back to a single empty item so a trigger never
// returns nothing. Shared by all three Phase 1 triggers.
func triggerItemsOrEmpty(items []domain.Item) []domain.Item {
	if len(items) > 0 {
		return items
	}
	return []domain.Item{domain.NewItem(nil)}
}
