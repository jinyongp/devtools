package configcontract

// JSON Schema describes each record; references and cycles require the complete
// error graph. Consumers of the Go contract validate both boundaries.
func diagnosticRelations(name string, value any) []Issue {
	object, _ := value.(map[string]any)
	prefix := []string{}
	if name == "Failure" {
		failure, _ := object["error"].(map[string]any)
		object, _ = failure["details"].(map[string]any)
		prefix = []string{"error", "details"}
	} else if name != "ErrorDetails" {
		return nil
	}
	causes, _ := object["causes"].([]any)
	ids := map[string]map[string]any{}
	bad := func() []Issue {
		return []Issue{{Path: append(prefix, "causes"), Reason: "invalid_value", Rule: "cause_graph"}}
	}
	for _, value := range causes {
		cause, _ := value.(map[string]any)
		id, _ := cause["id"].(string)
		if ids[id] != nil {
			return bad()
		}
		ids[id] = cause
	}
	state := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		if ids[id] == nil || state[id] == 1 {
			return false
		}
		if state[id] == 2 {
			return true
		}
		state[id] = 1
		parents, _ := ids[id]["caused_by"].([]any)
		for _, value := range parents {
			parent, _ := value.(string)
			if !visit(parent) {
				return false
			}
		}
		state[id] = 2
		return true
	}
	for id := range ids {
		if !visit(id) {
			return bad()
		}
	}
	if primary, ok := object["primary_cause_id"].(string); ok {
		if ids[primary] == nil {
			return bad()
		}
		parents, _ := ids[primary]["caused_by"].([]any)
		if len(parents) != 0 {
			return bad()
		}
	}
	checks, _ := object["checks"].([]any)
	for _, value := range checks {
		check, _ := value.(map[string]any)
		parents, _ := check["blocked_by"].([]any)
		if len(parents) > 0 && check["status"] != "unknown" {
			return bad()
		}
		for _, value := range parents {
			id, _ := value.(string)
			if ids[id] == nil {
				return bad()
			}
		}
	}
	return nil
}
