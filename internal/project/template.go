package project

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"strings"
)

// Expand performs one pass; inserted values are always literal.
func Expand(input string, resolve func(string) (string, bool)) (string, *protocol.Error) {
	var out strings.Builder
	for len(input) > 0 {
		if strings.HasPrefix(input, "$${") {
			out.WriteString("${")
			input = input[3:]
			continue
		}
		if strings.HasPrefix(input, "${") {
			end := strings.IndexByte(input, '}')
			if end < 0 {
				return "", templateError()
			}
			value, ok := resolve(input[2:end])
			if !ok || strings.ContainsRune(value, 0) {
				return "", templateError()
			}
			out.WriteString(value)
			input = input[end+1:]
			continue
		}
		out.WriteByte(input[0])
		input = input[1:]
	}
	return out.String(), nil
}
func templateError() *protocol.Error {
	return protocol.NewError("binding_reference_error", "A binding reference is invalid or unavailable.", 3, nil)
}
