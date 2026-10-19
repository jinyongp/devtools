package cli

func outputOptions() []Option {
	return []Option{
		{Name: "output", Pattern: "^(auto|redacted|raw)$", Description: "Output protection: auto masks non-TTY streams; raw is an explicit exception."},
		{Name: "stdout", Pattern: "^(auto|redacted|raw)$", Description: "Override stdout protection, for example raw binary stdout."},
		{Name: "stderr", Pattern: "^(auto|redacted|raw)$", Description: "Override stderr protection independently of stdout."},
	}
}
func outputPolicySchema() map[string]any {
	mode := map[string]any{"enum": []string{"auto", "redacted", "raw"}}
	return object(map[string]any{"mode": mode, "stdout": mode, "stderr": mode}, "mode")
}
func outputPlanSchema() map[string]any {
	mode := map[string]any{"enum": []string{"redacted", "raw"}}
	return object(map[string]any{
		"requested": outputPolicySchema(), "stdout": mode, "stderr": mode,
		"stdout_reason": stringSchema(), "stderr_reason": stringSchema(),
		"short_secret_patterns": map[string]any{"type": "boolean"},
	}, "requested", "stdout", "stderr", "stdout_reason", "stderr_reason", "short_secret_patterns")
}
