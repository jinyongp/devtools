package tasks

import (
	"encoding/json"
	"reflect"
)

func sameJSON(left, right any) bool {
	// Go considers +0 and -0 equal; their JSON encodings and signatures differ.
	// Keep the fast comparison for ordinary projections and use the canonical
	// comparison when a floating-point zero could hide a sign change.
	return reflect.DeepEqual(left, right) && !hasFloatingZero(reflect.ValueOf(left)) || hash(left) == hash(right)
}

func hasFloatingZero(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return !value.IsNil() && hasFloatingZero(value.Elem())
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if hasFloatingZero(value.Field(i)) {
				return true
			}
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			if hasFloatingZero(value.Index(i)) {
				return true
			}
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if hasFloatingZero(iterator.Value()) {
				return true
			}
		}
	}
	return false
}

// copyJSONValue preserves JSON's object/array/number representation while
// copying mutable containers. Strings (including large document bodies) are
// immutable and can be shared without encoding and decoding them again.
func copyJSONValue(value any) any {
	switch v := value.(type) {
	case nil, string, bool, float64:
		return v
	case int:
		return float64(v)
	case Object:
		if v == nil {
			return nil
		}
		return map[string]any(copyObject(v))
	case map[string]any:
		if v == nil {
			return nil
		}
		return map[string]any(copyObject(Object(v)))
	case []any:
		if v == nil {
			return nil
		}
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = copyJSONValue(x)
		}
		return out
	case []string:
		if v == nil {
			return nil
		}
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = x
		}
		return out
	case []Object:
		if v == nil {
			return nil
		}
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = copyJSONValue(x)
		}
		return out
	default:
		// Typed values in legacy upgrade events and receipt payloads still use
		// their JSON contracts; ordinary stored projection data uses the cases above.
		raw, err := json.Marshal(value)
		if err != nil {
			panic(err)
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			panic(err)
		}
		return out
	}
}

func copyItem(item *Item) *Item {
	if item == nil {
		return nil
	}
	out := *item
	if item.Depends != nil {
		out.Depends = append([]string{}, item.Depends...)
	}
	out.Props = copyObject(item.Props)
	return &out
}

func copyDefinition(basis *DefinitionBasis) *DefinitionBasis {
	if basis == nil {
		return nil
	}
	out := *basis
	out.BodyEpochs = copyMap(basis.BodyEpochs)
	out.KeyEpochs = copyMap(basis.KeyEpochs)
	out.LegacyBases = copyMap(basis.LegacyBases)
	out.LegacyRuns = copyMap(basis.LegacyRuns)
	if basis.RemovedKeys != nil {
		out.RemovedKeys = make(map[string]Object, len(basis.RemovedKeys))
		for key, v := range basis.RemovedKeys {
			out.RemovedKeys[key] = copyObject(v)
		}
	}
	if basis.Order != nil {
		out.Order = append([]string{}, basis.Order...)
	}
	if basis.Completions != nil {
		out.Completions = make([]CompletionBasis, len(basis.Completions))
		for i, v := range basis.Completions {
			out.Completions[i] = v
			out.Completions[i].Result = copyObject(v.Result)
		}
	}
	return &out
}

func copyMap[K comparable, V any](source map[K]V) map[K]V {
	if source == nil {
		return nil
	}
	out := make(map[K]V, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func copyEvent(event Event) Event {
	event.Data = copyObject(event.Data)
	return event
}
