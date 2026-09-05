package cli

import (
	"github.com/jinyongp/devtools/internal/tasks"
	"strings"
)

func taskBody(def tasks.Definition) map[string]any {
	text := func() map[string]any { return map[string]any{"type": "string", "minLength": 1, "maxLength": 16384} }
	id := map[string]any{"type": "string", "format": "uuid"}
	array := func(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
	properties := map[string]any{}
	for _, field := range def.Fields {
		var s map[string]any
		textSchema := text()
		s = textSchema
		switch field {
		case "title":
			s = map[string]any{"type": "string", "minLength": 1, "maxLength": 200}
		case "body":
			s = map[string]any{"type": "string", "maxLength": 1048576}
		case "description":
			s = map[string]any{"type": "string", "maxLength": 16384}
		case "required":
			s = map[string]any{"type": "boolean", "default": true}
		case "requirements":
			s = array(object(map[string]any{"key": text(), "text": text()}, "key", "text"))
		case "acceptance":
			s = array(text())
			if def.Action == "spec.set" {
				s = array(object(map[string]any{"key": text(), "text": text(), "requirement_keys": array(text())}, "key", "text", "requirement_keys"))
			}
		case "code":
			s = array(object(map[string]any{"repository": text(), "commit": map[string]any{"type": []string{"string", "null"}}, "dirty": map[string]any{"type": "boolean"}, "evidence": map[string]any{"oneOf": []any{text(), array(text())}}}, "repository", "commit", "dirty", "evidence"))
		case "commits":
			s = array(object(map[string]any{"repository": text(), "commit": text()}, "repository", "commit"))
		case "evidence":
			s = array(object(map[string]any{"kind": map[string]any{"enum": []string{"command", "file", "commit", "url", "note"}}, "reference": text(), "description": text()}, "kind", "reference", "description"))
		case "result":
			s = map[string]any{"enum": []string{"pass", "fail", "blocked", "skipped"}}
		default:
			if strings.HasSuffix(field, "_id") {
				s = id
			} else if strings.HasSuffix(field, "_ids") || field == "depends_on" {
				s = array(id)
			} else if taskArray(field) {
				s = array(text())
			}
		}
		properties[field] = s
	}
	return object(properties, def.Required...)
}
func taskOutput(mutation bool) map[string]any {
	str := stringSchema()
	integer := map[string]any{"type": "integer", "minimum": 0}
	boolean := map[string]any{"type": "boolean"}
	nullable := map[string]any{"type": []string{"string", "null"}}
	item := map[string]any{"type": []string{"object", "null"}, "properties": map[string]any{"id": str, "kind": str, "title": str, "description": str, "state": str, "running": boolean, "ready": boolean, "workstream_id": nullable, "definition_revision": integer, "created_at": str, "updated_at": str}}
	props := map[string]any{"profile": str, "revision": integer, "item": item, "items": map[string]any{"type": "array"}, "next_cursor": nullable, "cursor": nullable, "valid": boolean, "issues": map[string]any{"type": "array"}, "target_ids": map[string]any{"type": "array"}, "affected_ids": map[string]any{"type": "array"}, "affected_task_ids": map[string]any{"type": "array"}, "affected_workstream_ids": map[string]any{"type": "array"}, "running_ids": map[string]any{"type": "array"}, "completed_ids": map[string]any{"type": "array"}, "allowed": boolean, "blockers": map[string]any{"type": "array"}, "nodes": map[string]any{"type": "array", "items": item}, "edges": map[string]any{"type": "array", "items": object(map[string]any{"from": str, "to": str}, "from", "to")}, "roots": map[string]any{"type": "array"}, "truncated": boolean, "continuations": map[string]any{"type": "array"}, "documents": map[string]any{"type": "object"}, "tasks": map[string]any{"type": "array"}, "validations": map[string]any{"type": "array"}, "history": map[string]any{"type": "array"}, "omitted_ids": map[string]any{"type": "array"}, "bases": map[string]any{"type": []string{"object", "null"}}, "records": map[string]any{"type": []string{"array", "null"}}, "reason": str}
	required := []string{"profile", "revision"}
	if mutation {
		for k, v := range map[string]any{"current_revision": integer, "request_id": str, "replayed": boolean, "changed": boolean, "action_ids": map[string]any{"type": "array", "items": str}, "claimed": boolean, "run": map[string]any{"type": []string{"object", "null"}}, "context": nullable, "context_valid": boolean, "basis_id": str, "record_id": str} {
			props[k] = v
		}
		required = append(required, "current_revision", "request_id", "replayed", "changed", "action_ids")
	}
	return object(props, required...)
}
