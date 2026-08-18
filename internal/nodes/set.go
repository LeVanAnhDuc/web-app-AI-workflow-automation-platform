package nodes

import "github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"

// Set builds the item that flows on. It runs perItem so every row's expressions
// are resolved against the item they are shaping.
type Set struct{}

// Descriptor implements Node.
func (Set) Descriptor() Descriptor {
	return Descriptor{
		Type:        "set",
		Name:        "Set",
		Category:    CategoryCore,
		Description: "Adds, overwrites or narrows the fields on each item.",
		Icon:        "table",
		Mode:        ModePerItem,
		Inputs:      MainIn,
		Outputs:     MainOut,
		Params: []ParamSpec{
			{
				Name:    "mode",
				Label:   "Mode",
				Type:    ParamSelect,
				Default: "merge",
				Options: []ParamOption{
					{Label: "Keep input and add fields", Value: "merge"},
					{Label: "Keep only the fields below", Value: "keepOnly"},
				},
			},
			{
				Name:               "fields",
				Label:              "Fields",
				Type:               ParamKeyValue,
				Required:           true,
				Placeholder:        "fullName",
				Description:        "One field name and value per row. A name that already exists is overwritten.",
				SupportsExpression: true,
			},
		},
	}
}

// Execute produces the single output item for the current input item.
func (Set) Execute(ec ExecContext) (Result, error) {
	fields, err := ec.Params.KeyValues("fields")
	if err != nil {
		return Result{}, err
	}
	mode := ec.Params.StringOr("mode", "merge")

	var out map[string]any
	switch mode {
	case "", "merge":
		// A shallow copy: the input item must not be mutated, because the engine
		// persists it as this node's input and an earlier node may still hold it.
		out = make(map[string]any, len(ec.Item.JSON)+len(fields))
		for key, value := range ec.Item.JSON {
			out[key] = value
		}
	case "keepOnly":
		out = make(map[string]any, len(fields))
	default:
		return Result{}, domain.Errorf(domain.ErrCodeValidation, "unknown set mode %q", mode)
	}

	for _, field := range fields {
		if field.Key == "" {
			continue
		}
		// A dotted name stays one literal key in Phase 1. Writing into nested
		// paths is a later concern, and guessing at it now would make
		// "user.name" ambiguous with a field genuinely called that.
		out[field.Key] = field.Value
	}
	return Main(domain.NewItem(out)), nil
}
