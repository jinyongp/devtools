package cli

import (
	"github.com/jinyongp/devtools/internal/tasks"
	"strings"
)

func taskBody(def tasks.Definition) map[string]any {
	if def.Action == "workstream.edited" {
		return editBodySchema()
	}
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
	body := object(properties, def.Required...)
	if strings.HasSuffix(def.Action, ".update") {
		body["minProperties"] = 1
	}
	return body
}
func taskOutput(mutation bool) map[string]any {
	str := stringSchema()
	integer := map[string]any{"type": "integer", "minimum": 0}
	boolean := map[string]any{"type": "boolean"}
	nullable := map[string]any{"type": []string{"string", "null"}}
	item := map[string]any{"type": []string{"object", "null"}, "properties": map[string]any{"id": str, "kind": str, "title": str, "description": str, "state": str, "running": boolean, "ready": boolean, "workstream_id": nullable, "definition_revision": integer, "created_at": str, "updated_at": str}}
	props := map[string]any{"profile": str, "revision": integer, "item": item, "items": map[string]any{"type": "array"}, "next_cursor": nullable, "cursor": nullable, "valid": boolean, "issues": map[string]any{"type": "array"}, "target_ids": map[string]any{"type": "array"}, "affected_ids": map[string]any{"type": "array"}, "affected_task_ids": map[string]any{"type": "array"}, "affected_workstream_ids": map[string]any{"type": "array"}, "running_ids": map[string]any{"type": "array"}, "completed_ids": map[string]any{"type": "array"}, "allowed": boolean, "blockers": map[string]any{"type": "array"}, "nodes": map[string]any{"type": "array", "items": item}, "edges": map[string]any{"type": "array", "items": object(map[string]any{"from": str, "to": str}, "from", "to")}, "roots": map[string]any{"type": "array"}, "truncated": boolean, "continuations": map[string]any{"type": "array"}, "documents": map[string]any{"type": "object"}, "tasks": map[string]any{"type": "array"}, "validations": map[string]any{"type": "array"}, "history": map[string]any{"type": "array"}, "omitted_ids": map[string]any{"type": "array"}, "bases": map[string]any{"type": []string{"object", "null"}}, "records": map[string]any{"type": []string{"array", "null"}}, "reason": str}
	props["proposed_changes"] = map[string]any{"type": "object"}
	for _, key := range []string{"dry_run", "would_change", "applicable", "closable", "execution_ready"} {
		props[key] = boolean
	}
	for _, key := range []string{"changes", "effects", "next_actions", "created_items", "task_order"} {
		props[key] = map[string]any{"type": "array"}
	}
	props["created_refs"] = map[string]any{"type": "object"}
	props["impact"] = map[string]any{"type": "object"}
	required := []string{"profile", "revision"}
	if mutation {
		for k, v := range map[string]any{"previous_revision": integer, "current_revision": integer, "affected_count": integer, "affected_ids": map[string]any{"type": "array", "items": str}, "request_id": str, "replayed": boolean, "changed": boolean, "action_ids": map[string]any{"type": "array", "items": str}, "claimed": boolean, "run": map[string]any{"type": []string{"object", "null"}}, "context": nullable, "context_valid": boolean, "basis_id": str, "record_id": str} {
			props[k] = v
		}
		required = append(required, "previous_revision", "current_revision", "affected_count", "affected_ids", "request_id", "replayed", "changed", "action_ids")
	}
	return object(props, required...)
}

func editBodySchema() map[string]any {
	text := map[string]any{"type": "string", "minLength": 1, "maxLength": 16384}
	key := map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_-]{0,63}$"}
	ref := map[string]any{"oneOf": []any{map[string]any{"type": "string", "format": "uuid"}, map[string]any{"type": "string", "pattern": "^@[A-Za-z][A-Za-z0-9_-]{0,63}$"}}}
	array := func(v any) map[string]any { return map[string]any{"type": "array", "items": v} }
	variants := []any{}
	add := func(name string, props map[string]any, required ...string) {
		props["op"] = map[string]any{"const": name}
		variants = append(variants, object(props, append([]string{"op"}, required...)...))
	}
	metadata := taskBody(*tasks.Find("workstream.update"))
	add("workstream.update", map[string]any{"value": metadata}, "value")
	for _, name := range []string{"spec.update", "plan.update"} {
		add(name, map[string]any{"value": object(map[string]any{"body": map[string]any{"type": "string", "maxLength": 1 << 20}}, "body")}, "value")
	}
	for _, kind := range []string{"requirement", "acceptance", "task", "validation"} {
		identifier := "id"
		idSchema := ref
		if kind == "requirement" || kind == "acceptance" {
			identifier = "key"
			idSchema = key
		}
		for _, action := range []string{"remove", "restore"} {
			add(kind+"."+action, map[string]any{identifier: idSchema}, identifier)
		}
		for _, action := range []string{"add", "update"} {
			name := kind + "." + action
			var value map[string]any
			if kind == "task" || kind == "validation" {
				value = taskBody(*tasks.Find(name))
				p := value["properties"].(map[string]any)
				if kind == "task" {
					delete(p, "workstream_id")
				}
				if kind == "validation" && action == "add" {
					p["task_id"] = ref
					p["workstream_id"] = map[string]any{"type": "string", "format": "uuid"}
					value["oneOf"] = []any{
						map[string]any{"required": []string{"task_id"}, "not": map[string]any{"required": []string{"workstream_id"}}}, map[string]any{"required": []string{"workstream_id"}, "not": map[string]any{"required": []string{"task_id"}}},
					}
				}
			} else {
				p := map[string]any{"text": text}
				required := []string{}
				if kind == "acceptance" {
					p["requirement_keys"] = array(key)
				}
				if action == "add" {
					p["key"] = key
					required = []string{"key", "text"}
					if kind == "acceptance" {
						required = append(required, "requirement_keys")
					}
				}
				value = object(p, required...)
			}
			if action == "update" {
				value["minProperties"] = 1
			}
			props := map[string]any{"value": value}
			required := []string{"value"}
			if action == "update" {
				props[identifier] = idSchema
				required = append(required, identifier)
			} else if kind == "task" || kind == "validation" {
				props["ref"] = key
			}
			add(name, props, required...)
		}
	}
	add("task.depends.set", map[string]any{"id": ref, "depends_on": array(ref)}, "id", "depends_on")
	add("workstream.depends.set", map[string]any{"depends_on": array(map[string]any{"type": "string", "format": "uuid"})}, "depends_on")
	for _, anchor := range []string{"before_id", "after_id"} {
		add("task.move", map[string]any{"id": ref, anchor: ref}, "id", anchor)
	}
	return object(map[string]any{"reason": text, "operations": map[string]any{"type": "array", "minItems": 1, "maxItems": 200, "items": map[string]any{"oneOf": variants}}}, "reason", "operations")
}
