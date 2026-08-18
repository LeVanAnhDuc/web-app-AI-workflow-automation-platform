package nodes

// Default returns every Phase 1 node, in palette order. The order is the order
// the palette and the node picker show, so triggers come first and the flow
// nodes last; GET /api/v1/node-types serves this list verbatim.
func Default() *Registry {
	return NewRegistry(
		ManualTrigger{},
		WebhookTrigger{},
		ScheduleTrigger{},
		HTTPRequest{},
		Code{},
		If{},
		Set{},
		Merge{},
	)
}
