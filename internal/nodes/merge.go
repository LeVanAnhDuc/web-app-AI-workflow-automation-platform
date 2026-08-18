package nodes

import "github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"

// Merge joins two branches back together. It is the one Phase 1 node with more
// than one input handle, so it reads ec.Inputs rather than ec.Items.
type Merge struct{}

// Descriptor implements Node.
func (Merge) Descriptor() Descriptor {
	return Descriptor{
		Type:        "merge",
		Name:        "Merge",
		Category:    CategoryFlow,
		Description: "Combines the items arriving on two inputs into one stream.",
		Icon:        "merge",
		Mode:        ModeOnce,
		Inputs: []Handle{
			{Name: HandleInput1, Label: "Input 1"},
			{Name: HandleInput2, Label: "Input 2"},
		},
		Outputs: MainOut,
		Params: []ParamSpec{
			{
				Name:    "mode",
				Label:   "Mode",
				Type:    ParamSelect,
				Default: "append",
				Options: []ParamOption{
					{Label: "Append: input 1 then input 2", Value: "append"},
					{Label: "Combine by position", Value: "combineByPosition"},
				},
				Description: "Append keeps every item. Combine by position merges item 1 with " +
					"item 1, item 2 with item 2, and so on.",
			},
		},
	}
}

// Execute merges the two inputs according to the mode.
func (Merge) Execute(ec ExecContext) (Result, error) {
	first, second := ec.Inputs[HandleInput1], ec.Inputs[HandleInput2]

	switch mode := ec.Params.StringOr("mode", "append"); mode {
	case "", "append":
		out := make([]domain.Item, 0, len(first)+len(second))
		out = append(out, first...)
		out = append(out, second...)
		return MainSlice(out), nil
	case "combineByPosition":
		// The longer side decides the length, so a branch that produced fewer
		// items leaves the other branch's fields intact instead of truncating it.
		length := max(len(first), len(second))
		out := make([]domain.Item, 0, length)
		for i := range length {
			merged := map[string]any{}
			if i < len(first) {
				for key, value := range first[i].JSON {
					merged[key] = value
				}
			}
			if i < len(second) {
				// Input 2 wins a collision: it is the later branch in the
				// palette, and "enrich input 1 with input 2" is the common case.
				for key, value := range second[i].JSON {
					merged[key] = value
				}
			}
			out = append(out, domain.NewItem(merged))
		}
		return MainSlice(out), nil
	default:
		return Result{}, domain.Errorf(domain.ErrCodeValidation, "unknown merge mode %q", mode)
	}
}
