package protocol

import (
	"encoding/json"
	"strings"
)

// Public projects domain details onto the protocol-13 allowlist. Private paths,
// request bodies and execution/session credentials never cross this boundary.
func Public(e *Error, operation string) *Error {
	copy := *e
	old := e.Details
	if old == nil {
		old = map[string]any{}
	}
	if operation == "" {
		operation = "devtools"
	}
	phase := "validate"
	if value, ok := old["phase"].(string); ok {
		phase = value
	}
	reason := e.Code
	if value, ok := old["reason"].(string); ok && value != "" {
		reason = value
	}
	if e.Code == "command_not_found" && e.ExitCode == 127 {
		copy.Code = "executable_not_found"
		reason = "not_found"
	}
	target := map[string]any{"kind": "command", "name": operation}
	if value := diagnosticObject(old["target"]); value != nil {
		for _, key := range []string{"kind", "name", "command", "profile", "env", "directory", "config_path", "id", "request_id", "revision", "bundle"} {
			if v, ok := value[key].(string); ok {
				target[key] = v
			}
		}
		if _, named := value["name"]; !named {
			if id, ok := value["id"].(string); ok {
				target["name"] = id
			}
		}
	}
	if _, ok := target["name"]; !ok {
		target["name"] = operation
	}
	for _, key := range []string{"profile", "env", "command", "bundle", "request_id", "id"} {
		if v, ok := old[key].(string); ok {
			target[key] = v
		}
	}
	if e.Code == "invalid_config" {
		if path, ok := old["path"].(string); ok {
			target["config_path"] = path
		}
	}
	facts := map[string]any{}
	allowed := []string{"expected_kind", "actual_kind", "expected_type", "observed_type", "limit_kind", "observed", "limit", "import_items", "expected_version", "observed_version", "state", "expected_revision", "observed_revision", "candidate_digest", "backup_name", "os_code", "input_role", "resource", "field", "rule", "allowed", "minimum", "maximum", "actual_length", "suggestions", "reason", "executable", "search_origin", "domain", "stage", "context_reason", "expected_target_kind", "actual_target_kind", "affected_count", "outcomes"}
	if value, ok := old["facts"].(map[string]any); ok {
		for _, key := range allowed {
			if v, ok := value[key]; ok {
				facts[key] = v
			}
		}
	}
	if values, ok := facts["outcomes"]; ok {
		out := []any{}
		for _, value := range diagnosticArray(values) {
			item := diagnosticObject(value)
			safe := map[string]any{}
			for _, key := range []string{"kind", "name", "id", "state", "code"} {
				if v, ok := item[key].(string); ok {
					safe[key] = v
				}
			}
			if v, ok := item["changed"].(bool); ok {
				safe["changed"] = v
			}
			out = append(out, safe)
		}
		facts["outcomes"] = out
	}
	importItems := facts["import_items"]
	if e.Code == "import_conflict" {
		importItems = old["items"]
	}
	if importItems != nil {
		out := []any{}
		for _, item := range diagnosticArray(importItems) {
			v := diagnosticObject(item)
			key, kok := v["key"].(string)
			kind, _ := v["kind"].(string)
			action, _ := v["action"].(string)
			if kok && (kind == "variable" || kind == "secret") && (action == "conflict" || action == "kind_conflict") {
				out = append(out, map[string]any{"key": key, "kind": kind, "action": action})
			}
		}
		facts["import_items"] = out
	}
	for _, key := range []string{"domain", "resource", "field", "stage", "state", "input_role", "expected_revision", "observed_revision"} {
		if v, ok := old[key].(string); ok {
			facts[key] = v
		}
	}
	if v, ok := old["os_error"]; ok {
		facts["os_code"] = v
	}
	if stage, ok := old["operation"].(string); ok && stage != operation {
		if _, exists := facts["stage"]; !exists {
			facts["stage"] = stage
		}
	}
	for _, key := range []string{"context_reason", "expected_target_kind", "actual_target_kind", "affected_count"} {
		if value, ok := old[key]; ok {
			facts[key] = value
		}
	}
	if value, ok := old["current_revision"]; ok {
		facts["observed_revision"] = value
	}
	outcomes := []any{}
	for _, item := range diagnosticArray(old["items"]) {
		value := diagnosticObject(item)
		name, ok := value["command"].(string)
		if !ok {
			continue
		}
		state, _ := value["status"].(string)
		changed, _ := value["changed"].(bool)
		outcome := map[string]any{"kind": "command", "name": name, "state": state, "changed": changed}
		if record := diagnosticObject(value["item"]); record != nil {
			if id, ok := record["id"].(string); ok {
				outcome["id"] = id
			}
		}
		if condition := diagnosticObject(value["condition"]); condition != nil {
			if code, ok := condition["code"].(string); ok {
				outcome["code"] = code
			}
		}
		outcomes = append(outcomes, outcome)
	}
	if len(outcomes) > 0 {
		facts["outcomes"] = outcomes
	}
	sources := diagnosticArray(old["sources"])
	if sources == nil {
		sources = []any{}
	}
	checks := old["checks"]
	if checks == nil {
		checks = []any{}
	}
	allChecks := diagnosticArray(checks)
	causes := old["causes"]
	if causes == nil {
		causes = []any{}
	}
	projectedCauses := []any{}
	childRemedies := []any{}
	if list := diagnosticArray(causes); list != nil {
		for _, item := range list {
			value := diagnosticObject(item)
			if value["code"] == nil {
				continue
			}
			projectedCauses = append(projectedCauses, publicCause(value, operation)...)
		}
	}
	for _, item := range diagnosticArray(old["items"]) {
		value := diagnosticObject(item)
		if condition := diagnosticObject(value["condition"]); condition != nil {
			condition["target"] = map[string]any{"kind": "command", "name": value["command"]}
			projectedCauses = append(projectedCauses, publicCause(condition, operation)...)
			if details := diagnosticObject(condition["details"]); details != nil {
				allChecks = append(allChecks, diagnosticArray(details["checks"])...)
				childRemedies = append(childRemedies, diagnosticArray(details["remedies"])...)
			}
		}
	}
	causes = projectedCauses
	if allChecks == nil {
		allChecks = []any{}
	}
	checks = allChecks
	remedies := old["remedies"]
	if remedies == nil && len(childRemedies) > 0 {
		remedies = childRemedies
	}
	if remedies == nil {
		argv := []string{"devtools", "help"}
		if operation != "devtools" {
			argv = append(argv, strings.Fields(operation)...)
		}
		remedies = []Remedy{{Argv: argv, RequiredInputs: []string{}, Message: "Inspect the accepted inputs and next actions for this operation.", Effect: "read"}}
	}
	retry := "after_input_change"
	if copy.ExitCode == 1 {
		retry = "after_dependency_change"
	}
	if value, ok := old["retry"].(string); ok {
		retry = value
	}
	effects := "none"
	if changed, ok := old["changed"].(bool); ok && changed {
		effects = "committed"
	}
	for _, item := range diagnosticArray(old["items"]) {
		if changed, ok := diagnosticObject(item)["changed"].(bool); ok && changed {
			effects = "partial"
		}
	}
	if value, ok := old["effects"].(string); ok {
		effects = value
	}
	copy.Details = map[string]any{"operation": operation, "phase": phase, "target": target, "sources": sources, "reason": reason, "facts": facts, "checks": checks, "causes": causes, "remedies": remedies, "retry": retry, "effects": effects}
	if report, ok := old["report"]; ok {
		copy.Details["report"] = report
	}
	return &copy
}

func diagnosticObject(value any) map[string]any {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	data, e := json.Marshal(value)
	if e != nil {
		return nil
	}
	var m map[string]any
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.UseNumber()
	if d.Decode(&m) != nil {
		return nil
	}
	return m
}
func diagnosticArray(value any) []any {
	if value == nil {
		return nil
	}
	if a, ok := value.([]any); ok {
		return a
	}
	data, e := json.Marshal(value)
	if e != nil {
		return nil
	}
	var a []any
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.UseNumber()
	if d.Decode(&a) != nil {
		return nil
	}
	return a
}
func publicCause(value map[string]any, operation string) []any {
	code, _ := value["code"].(string)
	details := diagnosticObject(value["details"])
	if details == nil {
		details = value
	} else if target, ok := value["target"]; ok {
		copy := map[string]any{}
		for key, v := range details {
			copy[key] = v
		}
		merged := map[string]any{}
		for key, v := range diagnosticObject(details["target"]) {
			merged[key] = v
		}
		for key, v := range diagnosticObject(target) {
			merged[key] = v
		}
		copy["target"] = merged
		details = copy
	}
	projected := Public(&Error{Code: code, Message: "Dependency condition.", Details: details, ExitCode: 3}, operation)
	out := []any{map[string]any{"code": code, "reason": projected.Details["reason"], "target": projected.Details["target"], "sources": projected.Details["sources"], "facts": projected.Details["facts"]}}
	return append(out, diagnosticArray(projected.Details["causes"])...)
}

func (r Remedy) MarshalJSON() ([]byte, error) {
	type wire Remedy
	if r.Effect == "" {
		r.Effect = "read"
	}
	if r.Argv == nil {
		r.Argv = []string{}
	}
	if r.RequiredInputs == nil {
		r.RequiredInputs = []string{}
	}
	return json.Marshal(wire(r))
}
