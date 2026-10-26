package presentation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

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
	e, err := decode(public)
	if err != nil {
		return cliui.Document{}, err
	}
	if !e.ok {
		return errorDocument(e.value.(map[string]any)), nil
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
	heading := cliui.Text{Spans: []cliui.Span{{Text: "ERROR", Role: cliui.Failure}, {Text: "  " + e["code"].(string)}}}
	blocks := []cliui.Block{cliui.Heading(heading, 1), cliui.Paragraph(cliui.Words(e["message"].(string)))}
	for _, key := range keys(e) {
		if key != "message" && key != "code" {
			blocks = append(blocks, named(key, e[key])...)
		}
	}
	return cliui.NewDocument(blocks...)
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
			return []cliui.Block{cliui.Section(cliui.Styled("Next", cliui.Info), actions...)}
		}
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
		result = append(result, cliui.Paragraph(cliui.Words(message)))
		for _, key := range keys(action) {
			if key == "message" || key == "argv" {
				continue
			}
			if key == "required_inputs" && len(inputs) == 0 {
				continue
			}
			result = append(result, named(key, action[key])...)
		}
		if len(inputs) == 0 && len(argv) > 0 && argv[0] != "" {
			command, _ := cliui.CommandLine(argv)
			result = append(result, command)
		} else {
			label := "Arguments"
			if len(inputs) > 0 {
				label = "Arguments (inputs required)"
			}
			result = append(result, cliui.Fields(cliui.Field{Label: cliui.Literal(label), Value: cliui.Literal(literal(action["argv"]))}))
		}
	}
	return result, true
}

func title(command string) string {
	return strings.ToUpper(command[:1]) + command[1:]
}
