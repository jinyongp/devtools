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
	HexPattern  = `^[0-9a-fA-F]{4,32}$`
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
		map[string]any{"facts": map[string]any{"candidate_count": len(matches), "suggested_ids": suggestions}})
}

func commonPrefix(a, b string) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
