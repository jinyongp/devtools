package protocol

// Resource facts are assembled by domain observers. Copy only the declared
// public fields; never publish a private process record or lock document.
func projectResourceFacts(out, input map[string]any) {
	for _, key := range []string{"service", "instance_id", "owner_profile", "owner_directory"} {
		if value, ok := input[key].(string); ok {
			out[key] = value
		}
	}
	for name, fields := range map[string][]string{
		"port_observation": {"status", "tcp_reachable", "reason"},
		"owner":            {"status", "kind", "pid", "execution_id", "command", "reason"},
		"observation":      {"status", "reason", "missing"},
	} {
		if source := diagnosticObject(input[name]); source != nil {
			value := map[string]any{}
			for _, field := range fields {
				value[field] = source[field]
			}
			out[name] = value
		}
	}
	if value, exists := input["port"]; exists && out["port_observation"] != nil {
		out["port"] = value
	}
	if source := diagnosticObject(input["listener"]); source != nil {
		processes := []any{}
		for _, item := range diagnosticArray(source["processes"]) {
			process := diagnosticObject(item)
			processes = append(processes, map[string]any{"pid": process["pid"], "program": process["program"]})
			if len(processes) == 16 {
				break
			}
		}
		out["listener"] = map[string]any{"status": source["status"], "reason": source["reason"], "processes": processes}
	}
	if items := diagnosticArray(input["related_items"]); items != nil {
		result := []any{}
		for _, item := range items {
			source := diagnosticObject(item)
			value := map[string]any{}
			for _, key := range []string{"id", "kind", "state", "completion_status", "execution_status"} {
				value[key] = source[key]
			}
			blockers := []any{}
			for _, item := range diagnosticArray(source["blockers"]) {
				blocker := diagnosticObject(item)
				blockers = append(blockers, map[string]any{"code": blocker["code"], "message": blocker["message"], "target_id": blocker["target_id"]})
			}
			value["blockers"] = blockers
			result = append(result, value)
		}
		out["related_items"] = result
	}
}
