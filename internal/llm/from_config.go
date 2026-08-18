package llm

import "log/slog"

// FromAPIKey builds the registry a deployment runs with.
//
// An absent key is not an error: every non-AI node still works, and the AI nodes
// report the missing key with a message that says what to set. Failing at boot
// would make the whole platform depend on a feature most workflows never touch.
func FromAPIKey(apiKey string, log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	provider, err := NewAnthropic(apiKey)
	if err != nil {
		log.Warn("llm: AI nodes are disabled", "reason", err.Error())
		return NewRegistry()
	}
	log.Info("llm: provider ready", "provider", provider.Name(), "defaultModel", DefaultModel)
	return NewRegistry(provider)
}
