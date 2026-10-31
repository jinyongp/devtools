package protocol

import "fmt"

// Nested domain errors have independent local cause namespaces. Qualify them
// before flattening, then assign the stable public cN identifiers once.
func namespaceCauseGraph(causes, checks []any, prefix string) {
	renames := map[string]string{}
	for i, value := range causes {
		cause := diagnosticObject(value)
		id, _ := cause["id"].(string)
		qualified := prefix + id
		if id == "" {
			qualified = fmt.Sprintf("%sroot%d", prefix, i)
		} else {
			renames[id] = qualified
		}
		cause["id"] = qualified
		causes[i] = cause
	}
	remapCauseReferences(causes, "caused_by", renames)
	remapCauseReferences(checks, "blocked_by", renames)
}

func remapCauseReferences(values []any, key string, renames map[string]string) {
	for i, value := range values {
		record := diagnosticObject(value)
		if record == nil {
			continue
		}
		references := diagnosticArray(record[key])
		if references == nil {
			continue
		}
		for j, value := range references {
			if id, ok := value.(string); ok {
				if replacement := renames[id]; replacement != "" {
					references[j] = replacement
				}
			}
		}
		record[key] = references
		values[i] = record
	}
}
