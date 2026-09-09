package tasks

import (
	"fmt"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

type editOperation struct {
	Name  string
	Index int
	Body  Object
}

func editError(index int, message string) *protocol.Error {
	e := failure("invalid_argument", message)
	e.Details = map[string]any{"operation_index": index}
	return e
}

func parseEdit(body Object) ([]editOperation, *protocol.Error) {
	if len(body) != 2 || strings.TrimSpace(str(body, "reason")) == "" || len([]rune(str(body, "reason"))) > 16384 {
		return nil, failure("invalid_argument", "Provide reason and 1–200 operations.")
	}
	if _, ok := body["operations"]; !ok {
		return nil, failure("invalid_argument", "Provide operations.")
	}
	entries, ok := body["operations"].([]any)
	if !ok || len(entries) < 1 || len(entries) > 200 {
		return nil, failure("invalid_argument", "Provide 1–200 operations.")
	}
	out := []editOperation{}
	for n, value := range entries {
		m := objectValue(value)
		name := str(m, "op")
		parts := strings.Split(name, ".")
		if len(parts) != 2 {
			return nil, editError(n, "Unknown edit operation.")
		}
		kind, action := parts[0], parts[1]
		fields := []string{"op"}
		required := []string{"op"}
		switch {
		case name == "spec.update" || name == "plan.update":
			fields = append(fields, "value")
			required = append(required, "value")
		case name == "task.move":
			fields = append(fields, "id", "before_id", "after_id")
			required = append(required, "id")
		case contains([]string{"requirement", "acceptance", "task", "validation"}, kind) && contains([]string{"add", "update", "remove", "restore"}, action):
			if action == "add" {
				fields = append(fields, "value")
				required = append(required, "value")
				if kind == "task" || kind == "validation" {
					fields = append(fields, "ref")
				}
			} else {
				id := "id"
				if kind == "requirement" || kind == "acceptance" {
					id = "key"
				}
				fields = append(fields, id)
				required = append(required, id)
				if action == "update" {
					fields = append(fields, "value")
					required = append(required, "value")
				}
			}
		default:
			return nil, editError(n, "Unknown edit operation.")
		}
		for k, v := range m {
			if !contains(fields, k) || v == nil {
				return nil, editError(n, "Unknown or null operation field.")
			}
		}
		for _, k := range required {
			if _, ok := m[k]; !ok {
				return nil, editError(n, "Missing operation field: "+k)
			}
		}
		if _, ok := m["value"]; ok && len(objectValue(m["value"])) == 0 {
			return nil, editError(n, "Provide nonempty value object.")
		}
		for _, k := range []string{"id", "ref", "key", "before_id", "after_id"} {
			if v, ok := m[k]; ok {
				if _, ok := v.(string); !ok || str(m, k) == "" {
					return nil, editError(n, "Expected nonempty "+k)
				}
			}
		}
		out = append(out, editOperation{Name: name, Index: n, Body: m})
	}
	return out, nil
}

// Relation operations have a third name segment but share the same strict
// top-level contract as other edit operations.
func editOperations(body Object) ([]editOperation, *protocol.Error) {
	raw, ok := body["operations"].([]any)
	if !ok {
		return parseEdit(body)
	}
	copyBody := Object{"reason": body["reason"], "operations": raw}
	if len(body) != 2 {
		return parseEdit(body)
	}
	normal := make([]any, len(raw))
	relations := map[int]Object{}
	for n, v := range raw {
		m := objectValue(v)
		name := str(m, "op")
		if name == "task.depends.set" || name == "workstream.depends.set" {
			fields := []string{"op", "depends_on"}
			if name == "task.depends.set" {
				fields = append(fields, "id")
			}
			if len(m) != len(fields) {
				return nil, editError(n, "Provide dependency operation fields.")
			}
			for k := range m {
				if !contains(fields, k) {
					return nil, editError(n, "Unknown dependency field.")
				}
			}
			if e := stringArray(m["depends_on"], false); e != nil {
				return nil, editError(n, e.Message)
			}
			if name == "task.depends.set" && str(m, "id") == "" {
				return nil, editError(n, "Provide task id.")
			}
			relations[n] = m
			normal[n] = Object{"op": "plan.update", "value": Object{"body": "placeholder"}}
		} else {
			normal[n] = v
		}
	}
	copyBody["operations"] = normal
	ops, e := parseEdit(copyBody)
	if e != nil {
		return nil, e
	}
	for n, m := range relations {
		ops[n] = editOperation{Name: str(m, "op"), Index: n, Body: m}
	}
	return ops, nil
}

func operationValue(op editOperation, refs map[string]string) (Object, *protocol.Error) {
	v := objectValue(op.Body["value"])
	if v == nil {
		return nil, editError(op.Index, "Expected value.")
	}
	for _, field := range []string{"task_id", "workstream_id"} {
		if ref := str(v, field); strings.HasPrefix(ref, "@") {
			id, ok := refs[ref]
			if !ok {
				return nil, editError(op.Index, "Unknown reference: "+ref)
			}
			v[field] = id
		}
	}
	name := op.Name
	var d Definition
	switch name {
	case "spec.update", "plan.update":
		d = Definition{Fields: []string{"body"}, Required: []string{"body"}}
	case "task.add", "task.update", "validation.add", "validation.update":
		d = *Find(name)
	case "requirement.add", "requirement.update":
		fields := []string{"text"}
		if name == "requirement.add" {
			fields = append(fields, "key")
		}
		d = Definition{Fields: fields, Required: fields}
	case "acceptance.add", "acceptance.update":
		fields := []string{"text", "requirement_keys"}
		required := []string{}
		if name == "acceptance.add" {
			fields = append(fields, "key")
			required = fields
		}
		d = Definition{Fields: fields, Required: required}
	default:
		return nil, editError(op.Index, "Operation has no value.")
	}
	// Placeholders are syntactically UUIDs only during value validation. Their
	// actual type/ownership is checked against the candidate state afterwards.
	check := Object{}
	for k, x := range v {
		check[k] = x
	}
	for _, k := range []string{"task_id", "workstream_id"} {
		if strings.HasPrefix(str(check, k), "@") {
			check[k] = "00000000-0000-4000-8000-000000000000"
		}
	}
	if e := validateBody(&d, check); e != nil {
		return nil, editError(op.Index, e.Message)
	}
	for _, field := range []string{"acceptance_keys", "requirement_keys"} {
		if _, ok := v[field]; ok {
			v[field] = unique(arr(v, field))
		}
	}
	if k := str(v, "key"); k != "" && !key.MatchString(k) {
		return nil, editError(op.Index, "Invalid document key.")
	}
	if name == "task.add" {
		if _, exists := v["workstream_id"]; exists {
			return nil, editError(op.Index, "Task ownership is selected by the edit target.")
		}
	}
	if strings.HasPrefix(name, "validation.") && strings.HasSuffix(name, "add") {
		if (str(v, "task_id") == "") == (str(v, "workstream_id") == "") {
			return nil, editError(op.Index, "Choose exactly one validation owner.")
		}
		if _, ok := v["required"]; !ok {
			v["required"] = true
		}
	}
	return v, nil
}

func referenceID(ref string, refs map[string]string) (string, bool) {
	if strings.HasPrefix(ref, "@") {
		id, ok := refs[ref]
		return id, ok
	}
	return ref, validID(ref)
}

func previewID(op editOperation) string {
	if ref := str(op.Body, "ref"); ref != "" {
		return "@" + ref
	}
	return fmt.Sprintf("@operation-%d", op.Index)
}
