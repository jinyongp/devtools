package presentation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/cliui"
)

var errEnvelope = errors.New("invalid public response envelope")

type envelope struct {
	ok    bool
	value any
}

func decode(public []byte) (envelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(public))
	decoder.UseNumber()
	var raw map[string]any
	if decoder.Decode(&raw) != nil {
		return envelope{}, errEnvelope
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return envelope{}, errEnvelope
	}
	ok, valid := raw["ok"].(bool)
	if !valid || raw["schema_version"] != json.Number("1") {
		return envelope{}, errEnvelope
	}
	key, other := "data", "error"
	if !ok {
		key, other = other, key
	}
	value, exists := raw[key]
	if !exists {
		return envelope{}, errEnvelope
	}
	if _, exists := raw[other]; exists {
		return envelope{}, errEnvelope
	}
	if !ok {
		e, valid := value.(map[string]any)
		if !valid {
			return envelope{}, errEnvelope
		}
		if _, valid := e["code"].(string); !valid {
			return envelope{}, errEnvelope
		}
		if _, valid := e["message"].(string); !valid {
			return envelope{}, errEnvelope
		}
	}
	return envelope{ok, value}, nil
}

// Build consumes one already projected envelope. A missing or incompatible
// dedicated view falls back as a whole, without discarding unfamiliar fields.
func Build(command string, public []byte) (cliui.Document, error) {
	return BuildWithInputs(command, public, nil)
}

// InputHint is static command metadata, never request values or stored data.
// It supplies readable constraints without changing the public JSON diagnostic.
type InputHint struct {
	Name    string
	Option  bool
	Choices []string
	Rule    string
}

func BuildWithInputs(command string, public []byte, inputs []InputHint) (cliui.Document, error) {
	e, err := decode(public)
	if err != nil {
		return cliui.Document{}, err
	}
	if !e.ok {
		return conciseError(e.value.(map[string]any), inputs), nil
	}
	definition := DefinitionFor(command)
	if document, valid := buildView(definition, e.value); valid {
		return document, nil
	}
	return generic(e), nil
}

func BuildGeneric(public []byte) (cliui.Document, error) {
	e, err := decode(public)
	if err != nil {
		return cliui.Document{}, err
	}
	return generic(e), nil
}

func generic(e envelope) cliui.Document {
	if !e.ok {
		return errorDocument(e.value.(map[string]any))
	}
	blocks := []cliui.Block{cliui.Heading(cliui.Literal("Result"), 1)}
	blocks = append(blocks, tree(e.value)...)
	return cliui.NewDocument(blocks...)
}

func errorDocument(e map[string]any) cliui.Document {
	return conciseError(e, nil)
}

func keys(object map[string]any) []string {
	result := make([]string, 0, len(object))
	for key := range object {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func scalar(value any) bool {
	switch value.(type) {
	case map[string]any, []any:
		return false
	default:
		return true
	}
}

func literal(value any) string {
	if number, ok := value.(json.Number); ok {
		return string(number)
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func tree(value any) []cliui.Block {
	switch node := value.(type) {
	case map[string]any:
		if len(node) == 0 {
			return []cliui.Block{cliui.Paragraph(cliui.Literal("{}"))}
		}
		var blocks []cliui.Block
		var fields []cliui.Field
		flush := func() {
			if len(fields) > 0 {
				blocks = append(blocks, cliui.Fields(fields...))
				fields = nil
			}
		}
		for _, key := range keys(node) {
			v := node[key]
			if scalar(v) {
				fields = append(fields, cliui.Field{Label: cliui.Literal(key), Value: cliui.Literal(literal(v))})
			} else {
				flush()
				blocks = append(blocks, named(key, v)...)
			}
		}
		flush()
		return blocks
	case []any:
		if len(node) == 0 {
			return []cliui.Block{cliui.Paragraph(cliui.Literal("[]"))}
		}
		var blocks []cliui.Block
		for i, value := range node {
			blocks = append(blocks, cliui.Section(cliui.Literal(fmt.Sprintf("[%d]", i)), tree(value)...))
		}
		return blocks
	default:
		return []cliui.Block{cliui.Paragraph(cliui.Literal(literal(node)))}
	}
}

func named(key string, value any) []cliui.Block {
	if key == "next_actions" || key == "remedies" {
		if actions, valid := actionBlocks(value); valid {
			if len(actions) == 0 {
				return []cliui.Block{cliui.Section(cliui.Literal(key), cliui.Paragraph(cliui.Literal("[]")))}
			}
			return []cliui.Block{cliui.Section(cliui.Literal("Next"), actions...)}
		}
	}
	if blocks, valid := diagnosticSection(key, value); valid {
		return blocks
	}
	return []cliui.Block{cliui.Section(cliui.Literal(key), tree(value)...)}
}

func stringArray(value any) ([]string, bool) {
	array, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, len(array))
	for i, v := range array {
		var valid bool
		if result[i], valid = v.(string); !valid {
			return nil, false
		}
	}
	return result, true
}

func actionBlocks(value any) ([]cliui.Block, bool) {
	array, ok := value.([]any)
	if !ok {
		return nil, false
	}
	var result []cliui.Block
	for _, value := range array {
		action, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		message, ok := action["message"].(string)
		argv, argvOK := stringArray(action["argv"])
		inputs, inputsOK := stringArray(action["required_inputs"])
		if !ok || !argvOK || !inputsOK {
			return nil, false
		}
		// A complete help invocation already says what to inspect. Keep
		// substantive recovery instructions and unfinished templates verbatim.
		if len(inputs) > 0 || !routineHelp(message, argv) {
			result = append(result, cliui.Paragraph(cliui.Words(message)))
		}
		if len(inputs) > 0 {
			result = append(result, cliui.Paragraph(cliui.Words("Requires: "+strings.Join(inputs, ", ")+".")))
		}
		if len(inputs) == 0 && len(argv) > 0 && argv[0] != "" {
			command, _ := cliui.CommandLine(argv)
			result = append(result, command)
		} else {
			label := "Command"
			if len(inputs) > 0 {
				label = "Command template"
			}
			result = append(result, cliui.Fields(cliui.Field{Label: cliui.Literal(label), Value: cliui.Literal(argvDisplay(argv))}))
		}
	}
	return result, true
}

func routineHelp(message string, argv []string) bool {
	if len(argv) < 2 || argv[0] != "devtools" || argv[1] != "help" {
		return false
	}
	switch message {
	case "Inspect this command's accepted inputs and examples.",
		"Inspect the accepted inputs and next actions for this operation.",
		"List commands in this group.", "List commands in the nearest valid group.":
		return true
	}
	return false
}

func title(command string) string {
	if command == "" {
		return `""`
	}
	first, size := utf8.DecodeRuneInString(command)
	return string(unicode.ToUpper(first)) + command[size:]
}
