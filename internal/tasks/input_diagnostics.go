package tasks

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

// Input paths describe the logical JSON body, including when supplied by flags.
// Values and execution credentials never become diagnostic facts.
func inputFailure(message string, path ...string) *protocol.Error {
	return inputAt(failure("invalid_argument", message), path...)
}

func inputTypeError(message, expected string, value any, path ...string) *protocol.Error {
	e := inputFailure(message, path...)
	e.Details["reason"] = "invalid_type"
	e.Details["facts"] = map[string]any{"expected_type": expected, "observed_type": inputKind(value)}
	return e
}

func inputKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case []any, []string:
		return "array"
	case Object, map[string]any:
		return "object"
	case json.Number, float64, int, int64, uint64:
		return "number"
	default:
		return "unknown"
	}
}

func inputAt(e *protocol.Error, prefix ...string) *protocol.Error {
	copy := *e
	copy.Details = map[string]any{}
	for key, value := range e.Details {
		copy.Details[key] = value
	}
	sources := []any{}
	if previous, ok := e.Details["sources"].([]any); ok {
		for _, value := range previous {
			source, ok := value.(map[string]any)
			if !ok {
				continue
			}
			item := map[string]any{}
			for key, value := range source {
				item[key] = value
			}
			path := append([]string{}, prefix...)
			if existing, ok := source["path"].([]string); ok {
				path = append(path, existing...)
			}
			item["path"] = path
			sources = append(sources, item)
		}
	}
	if len(sources) == 0 {
		sources = append(sources, map[string]any{"kind": "json", "path": append([]string{}, prefix...)})
	}
	copy.Details["sources"] = sources
	return &copy
}

func editContext(op editOperation, e *protocol.Error, references ...map[string]string) *protocol.Error {
	prefix := []string{"operations", strconv.Itoa(op.Index)}
	already := false
	if sources, ok := e.Details["sources"].([]any); ok && len(sources) > 0 {
		if source, ok := sources[0].(map[string]any); ok {
			if path, ok := source["path"].([]string); ok && len(path) >= 2 && path[0] == "operations" {
				already = true
				if len(path) == 2 {
					field := ""
					if str(op.Body, "id") != "" {
						field = "id"
					} else if str(op.Body, "key") != "" {
						field = "key"
					} else if op.Body["value"] != nil {
						field = "value"
					}
					if field != "" {
						source["path"] = append(append([]string{}, path...), field)
					}
				}
			}
		}
	}
	if !already {
		if _, exists := e.Details["sources"]; !exists && str(op.Body, "id") != "" {
			prefix = append(prefix, "id")
		}
		e = inputAt(e, prefix...)
	}
	e.Details["operation_index"] = op.Index
	e.Details["edit_operation"] = op.Name
	delete(e.Details, "reference_id")
	if sources, ok := e.Details["sources"].([]any); ok && len(sources) > 0 {
		if source, ok := sources[0].(map[string]any); ok {
			if path, ok := source["path"].([]string); ok && len(path) > 2 {
				var value any = op.Body
				reference := false
				for _, field := range path[2:] {
					reference = reference || field == "id" || strings.HasSuffix(field, "_id") || field == "depends_on"
					switch v := value.(type) {
					case Object:
						value = v[field]
					case map[string]any:
						value = v[field]
					case []any:
						n, err := strconv.Atoi(field)
						value = nil
						if err == nil && n >= 0 && n < len(v) {
							value = v[n]
						}
					default:
						value = nil
					}
				}
				id, _ := value.(string)
				if len(references) > 0 && strings.HasPrefix(id, "@") {
					id = references[0][id]
				}
				if reference && validID(id) {
					e.Details["reference_id"] = id
				}
			}
		}
	}
	return e
}
