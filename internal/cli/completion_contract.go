package cli

// Completion sources describe selectable entities, not validation syntax. In
// particular, a newly created name or retry UUID has no existing-entity source.
// This metadata stays private; shell adapters use the shared __complete protocol.
type completionSource string

const (
	completeCommand         completionSource = "command"
	completeTool            completionSource = "tool"
	completePreset          completionSource = "preset"
	completePort            completionSource = "port"
	completePortDeclaration completionSource = "port-declaration"
	completeInstance        completionSource = "instance"
	completeProcess         completionSource = "process"
	completeProfile         completionSource = "profile"
	completeEnv             completionSource = "env"
	completeVariable        completionSource = "variable"
	completeSecret          completionSource = "secret"
	completeBundle          completionSource = "bundle"
	completeSelection       completionSource = "selection"
	completeValueKey        completionSource = "value-key"
	completeRoute           completionSource = "route"
	completeListener        completionSource = "listener"
	completeExposure        completionSource = "exposure"
	completeTask            completionSource = "task"
	completeWorkstream      completionSource = "workstream"
	completeValidation      completionSource = "validation"
	completeRun             completionSource = "run"
)

func taskFieldCompletion(field, targetKind string) completionSource {
	switch field {
	case "workstream_id":
		return completeWorkstream
	case "task_id", "task_ids":
		return completeTask
	case "validation_ids":
		return completeValidation
	case "depends_on":
		return completionSource(targetKind)
	}
	return ""
}
