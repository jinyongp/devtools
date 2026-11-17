// Package idref resolves user-facing references to exact storage keys.
package idref

import (
	"context"
	"iter"
	"regexp"
	"slices"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

const (
	MinDigits   = 4
	UUIDPattern = `^([0-9a-fA-F]{4,31}|[0-9a-fA-F]{8}-([0-9a-fA-F]{1,4}|[0-9a-fA-F]{4}-([0-9a-fA-F]{1,4}|[0-9a-fA-F]{4}-([0-9a-fA-F]{1,4}|[0-9a-fA-F]{4}-[0-9a-fA-F]{1,12}))))$`
	HexDigits   = `[0-9a-fA-F]{4,32}`
	HexPattern  = `^` + HexDigits + `$`
)

type Format uint8

const (
	UUID Format = iota
	Hex
)

var uuidReference = regexp.MustCompile(UUIDPattern)
var hexReference = regexp.MustCompile(HexPattern)

func (format Format) Exact(value string) bool {
	if format == UUID {
		return protocol.ValidUUID(value)
	}
	return len(value) == 32 && hexReference.MatchString(value)
}

func (format Format) Valid(value string) bool {
	if format == UUID {
		return uuidReference.MatchString(value)
	}
	return hexReference.MatchString(value)
}

func digits(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, "-", ""))
}

// Matches also accepts the incomplete words produced by shell completion.
func Matches(id, prefix string) bool {
	return matchesDigits(id, digits(prefix))
}

func matchesDigits(id, prefix string) bool {
	if prefix == "" {
		return true
	}
	n := min(MinDigits, len(prefix))
	return len(id) >= n && strings.EqualFold(id[:n], prefix[:n]) && strings.HasPrefix(digits(id), prefix)
}

// Resolve uses IDs supplied by the owning store at its selection boundary.
// Full IDs preserve their exact spelling and take the store's direct path.
func Resolve(ctx context.Context, format Format, input string, keys iter.Seq[string]) (string, *protocol.Error) {
	if format.Exact(input) {
		return input, nil
	}
	if !format.Valid(input) {
		return "", protocol.NewError("invalid_argument", "Enter at least four hexadecimal digits or the full ID.", 2, nil)
	}
	var matches []string
	prefix := digits(input)
	for id := range keys {
		if ctx.Err() != nil {
			break
		}
		if matchesDigits(id, prefix) && format.Exact(id) {
			matches = append(matches, id)
		}
	}
	if condition := protocol.ContextError(ctx.Err()); condition != nil {
		return "", condition
	}
	switch len(matches) {
	case 0:
		return "", protocol.NewError("id_not_found", "No ID matches this prefix in the selected scope.", 3, nil)
	case 1:
		return matches[0], nil
	}
	slices.SortFunc(matches, func(a, b string) int {
		if comparison := strings.Compare(digits(a), digits(b)); comparison != 0 {
			return comparison
		}
		return strings.Compare(a, b)
	})
	matches = slices.Compact(matches)
	if len(matches) == 1 {
		return matches[0], nil
	}
	suggestions := make([]string, 0, min(5, len(matches)))
	for i, id := range matches[:cap(suggestions)] {
		key := digits(id)
		length := len(prefix) + 1
		if i > 0 {
			length = max(length, commonPrefix(key, digits(matches[i-1]))+1)
		}
		if i+1 < len(matches) {
			length = max(length, commonPrefix(key, digits(matches[i+1]))+1)
		}
		if length < len(key) {
			id = key[:length]
		}
		suggestions = append(suggestions, id)
	}
	return "", protocol.NewError("ambiguous_id", "ID prefix is ambiguous. Use a longer ID such as "+strings.Join(suggestions, ", ")+".", 3,
		map[string]any{"ids": matches[:len(suggestions)], "facts": map[string]any{"suggestions": suggestions}})
}

// DescribeAmbiguity labels only the small set of public IDs in the diagnostic.
func DescribeAmbiguity(condition *protocol.Error, name func(string) string) *protocol.Error {
	if condition == nil || condition.Code != "ambiguous_id" {
		return condition
	}
	ids, _ := condition.Details["ids"].([]string)
	facts, _ := condition.Details["facts"].(map[string]any)
	suggestions, _ := facts["suggestions"].([]string)
	if len(ids) == 0 || len(ids) != len(suggestions) {
		return condition
	}
	choices := make([]string, len(suggestions))
	for i, prefix := range suggestions {
		choices[i] = prefix
		if label := strings.Join(strings.Fields(name(ids[i])), " "); label != "" {
			choices[i] += " (" + label + ")"
		}
	}
	condition.Message = "ID prefix is ambiguous. Use a longer ID such as " + strings.Join(choices, ", ") + "."
	return condition
}

// ResolveMany reads a shared key space once for repeated selector options.
func ResolveMany(ctx context.Context, format Format, inputs []string, keys iter.Seq[string]) ([]string, *protocol.Error) {
	buckets := map[string][]string{}
	for _, input := range inputs {
		if !format.Exact(input) {
			if !format.Valid(input) {
				return nil, protocol.NewError("invalid_argument", "Enter at least four hexadecimal digits or the full ID.", 2, nil)
			}
			buckets[strings.ToLower(input[:MinDigits])] = nil
		}
	}
	if len(buckets) != 0 {
		for id := range keys {
			if ctx.Err() != nil {
				return nil, protocol.ContextError(ctx.Err())
			}
			if len(id) >= MinDigits {
				key := strings.ToLower(id[:MinDigits])
				if bucket, ok := buckets[key]; ok {
					buckets[key] = append(bucket, id)
				}
			}
		}
	}
	resolved := make([]string, 0, len(inputs))
	for _, input := range inputs {
		id, e := Resolve(ctx, format, input, slices.Values(buckets[strings.ToLower(input[:MinDigits])]))
		if e != nil {
			return nil, e
		}
		resolved = append(resolved, id)
	}
	return resolved, nil
}

func commonPrefix(a, b string) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
