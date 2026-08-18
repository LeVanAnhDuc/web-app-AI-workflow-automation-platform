package nodes

// WebhookTrigger starts a workflow from an inbound HTTP request. The API owns
// the ingress: it matches the path, builds the {headers, query, body} item and
// stores it as the execution's trigger data, so this node only has to hand that
// data on. Keeping the parsing in the API is what lets the webhook answer 202
// before the worker has picked the job up.
type WebhookTrigger struct{}

// Descriptor implements Node.
func (WebhookTrigger) Descriptor() Descriptor {
	return Descriptor{
		Type:        "trigger.webhook",
		Name:        "Webhook",
		Category:    CategoryTrigger,
		Description: "Starts the workflow when an HTTP request arrives at a public URL.",
		Icon:        "webhook",
		Mode:        ModeOnce,
		Inputs:      nil,
		Outputs:     MainOut,
		IsTrigger:   true,
		Params: []ParamSpec{
			{
				Name:        "path",
				Label:       "Path",
				Type:        ParamString,
				Required:    true,
				Placeholder: "lead-in",
				Description: "The last segment of the public URL this workflow listens on: " +
					"a path of \"lead-in\" is reachable at /webhook/lead-in. Must be unique " +
					"across the workspace, and only takes effect once the workflow is active.",
			},
			{
				Name:    "method",
				Label:   "HTTP Method",
				Type:    ParamSelect,
				Default: "POST",
				Options: []ParamOption{
					{Label: "GET", Value: "GET"},
					{Label: "POST", Value: "POST"},
					{Label: "PUT", Value: "PUT"},
					{Label: "PATCH", Value: "PATCH"},
					{Label: "DELETE", Value: "DELETE"},
				},
				Description: "Requests using any other method are rejected with 405.",
			},
			{
				Name:    "respondMode",
				Label:   "Respond",
				Type:    ParamSelect,
				Default: "immediately",
				Options: []ParamOption{
					{Label: "Immediately with 202", Value: "immediately"},
				},
				Description: "When the caller gets its response.",
			},
			{
				Name:  "notice",
				Label: "",
				Type:  ParamNotice,
				Description: "The webhook always answers 202 Accepted as soon as the execution " +
					"is queued. Responding synchronously with data produced by the workflow " +
					"arrives in a later phase.",
			},
		},
	}
}

// Execute passes the trigger payload straight through, falling back to one
// empty item so a manual test run of an inactive webhook still flows.
func (WebhookTrigger) Execute(ec ExecContext) (Result, error) {
	return MainSlice(triggerItemsOrEmpty(ec.Trigger.Items)), nil
}
