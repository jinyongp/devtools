package values

import (
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/protocol"
)

func dotenvError(line int, message string) *protocol.Error {
	return protocol.NewError("invalid_dotenv", message, 2, map[string]any{"line": line})
}

// ParseDotenv parses assignments as data, preserving dollar expressions literally.
// Diagnostics contain only line numbers and fixed descriptions.
func ParseDotenv(input string) (map[string]string, *protocol.Error) {
	input = strings.TrimPrefix(input, "\ufeff")
	lines := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if !utf8.ValidString(line) || strings.ContainsRune(line, 0) {
			return nil, dotenvError(i+1, "Use UTF-8 input without NUL bytes.")
		}
	}
	result := map[string]string{}
	for i := 0; i < len(lines); i++ {
		lineNumber := i + 1
		line := strings.TrimLeft(lines[i], " \t")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") || strings.HasPrefix(line, "export\t") {
			line = strings.TrimLeft(line[6:], " \t")
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !keyPattern.MatchString(key) {
			return nil, dotenvError(lineNumber, "Expected an environment key followed by = and a value.")
		}
		if _, exists := result[key]; exists {
			return nil, dotenvError(lineNumber, "Each key must appear once in the input.")
		}
		value = strings.TrimLeft(value, " \t")
		if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
			quote := value[0]
			var parsed strings.Builder
			for pos := 1; ; pos++ {
				if pos == len(value) {
					i++
					if i == len(lines) {
						return nil, dotenvError(lineNumber, "Quoted value requires a closing quote.")
					}
					parsed.WriteByte('\n')
					value = lines[i]
					pos = -1
					continue
				}
				c := value[pos]
				if c == quote {
					tail := strings.TrimSpace(value[pos+1:])
					if tail != "" && !strings.HasPrefix(tail, "#") {
						return nil, dotenvError(i+1, "Only a comment may follow a quoted value.")
					}
					break
				}
				if quote == '"' && c == '\\' && pos+1 < len(value) {
					next := value[pos+1]
					switch next {
					case 'n':
						c = '\n'
					case 'r':
						c = '\r'
					case 't':
						c = '\t'
					case '"', '\\', '$':
						c = next
					default:
						parsed.WriteByte(c)
						c = next
					}
					pos++
				}
				parsed.WriteByte(c)
			}
			result[key] = parsed.String()
		} else {
			for pos := 0; pos < len(value); pos++ {
				if value[pos] == '#' && (pos == 0 || value[pos-1] == ' ' || value[pos-1] == '\t') {
					value = value[:pos]
					break
				}
			}
			result[key] = strings.TrimSpace(value)
		}
	}
	return result, nil
}
