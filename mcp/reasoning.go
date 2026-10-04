package mcp

// ConfigureReasoningEffort applies the saved setting to providers that support
// it. Other providers retain their existing request format and behavior.
func ConfigureReasoningEffort(client AIClient, effort string) {
	if configurable, ok := client.(interface{ SetReasoningEffort(string) }); ok {
		configurable.SetReasoningEffort(effort)
	}
}
