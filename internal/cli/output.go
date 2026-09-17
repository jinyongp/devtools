package cli

// OutputMode describes who owns stdout. Only JSON data is envelope-encoded.
type OutputMode string

const (
	OutputJSON        OutputMode = "json"
	OutputText        OutputMode = "text"
	OutputArtifact    OutputMode = "artifact"
	OutputPassthrough OutputMode = "passthrough"
)

// itemOutput describes a single resource, distinct from query metadata.
func itemOutput(item map[string]any) map[string]any {
	return object(map[string]any{"item": item}, "item")
}

func changedItemOutput(item map[string]any) map[string]any {
	return object(map[string]any{"item": item, "changed": map[string]any{"type": "boolean"}}, "item", "changed")
}
