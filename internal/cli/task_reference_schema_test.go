package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestTaskReferenceSchemasDoNotDependOnFormatAssertions(t *testing.T) {
	app := testApp(t)
	const id = "01234567-89ab-4cde-8fab-0123456789AB"
	for _, formats := range []bool{false, true} {
		for _, terminalNewline := range []bool{false, true} {
			for _, command := range [][]string{{"task", "workstream", "edit"}, {"task", "add"}, {"task", "validation", "add"}} {
				data := outputData(t, app, append([]string{"schema"}, command...))
				compiler := jsonschema.NewCompiler()
				if terminalNewline {
					compiler.UseRegexpEngine(func(pattern string) (jsonschema.Regexp, error) {
						re, err := regexp.Compile(pattern)
						return terminalNewlineRegexp{re}, err
					})
				}
				if formats {
					compiler.AssertFormat()
				}
				if err := compiler.AddResource("urn:task-reference", data["body_schema"]); err != nil {
					t.Fatal(err)
				}
				schema, err := compiler.Compile("urn:task-reference")
				if err != nil {
					t.Fatal(err)
				}
				if command[len(command)-1] == "edit" {
					for _, key := range []string{"new_task", "new_task\n", "new_task\r\n"} {
						var input any
						body := fmt.Sprintf(`{"reason":"Fixture","operations":[{"op":"task.add","ref":%q,"value":{"title":"Task"}}]}`, key)
						if err := json.Unmarshal([]byte(body), &input); err != nil {
							t.Fatal(err)
						}
						if got := schema.Validate(input) == nil; got != (key == "new_task") {
							t.Fatalf("formats=%v terminalNewline=%v key=%q accepted=%v", formats, terminalNewline, key, got)
						}
					}
				}
				for _, value := range []string{id, "@new_task", "", "not-an-id", "urn:uuid:" + id, "@1bad", id + "\n", "@new_task\n", "@new_task\r\n"} {
					cases := []string{}
					valid := value == id
					switch command[len(command)-1] {
					case "edit":
						valid = valid || value == "@new_task"
						for _, operation := range []string{
							`{"op":"task.update","id":%q,"value":{"title":"Changed"}}`,
							`{"op":"task.remove","id":%q}`,
							`{"op":"task.restore","id":%q}`,
							`{"op":"validation.remove","id":%q}`,
							`{"op":"task.depends.set","id":"` + id + `","depends_on":[%q]}`,
							`{"op":"task.move","id":"` + id + `","before_id":%q}`,
							`{"op":"task.move","id":"` + id + `","after_id":%q}`,
							`{"op":"validation.add","value":{"title":"Check","method":"Test","task_id":%q}}`,
						} {
							cases = append(cases, `{"reason":"Fixture","operations":[`+fmt.Sprintf(operation, value)+`]}`)
						}
					default:
						if len(command) == 2 {
							cases = append(cases, fmt.Sprintf(`{"title":"Task","workstream_id":%q}`, value))
						} else {
							cases = append(cases, fmt.Sprintf(`{"title":"Check","method":"Test","workstream_id":%q}`, value))
						}
					}
					for _, body := range cases {
						var input any
						if err := json.Unmarshal([]byte(body), &input); err != nil {
							t.Fatal(err)
						}
						if got := schema.Validate(input) == nil; got != valid {
							t.Fatalf("formats=%v command=%v body=%s accepted=%v want=%v", formats, command, body, got, valid)
						}
					}
					if command[len(command)-1] == "edit" {
						for _, operation := range []string{
							`{"op":"workstream.depends.set","depends_on":[%q]}`,
							`{"op":"validation.add","value":{"title":"Check","method":"Test","workstream_id":%q}}`,
						} {
							var input any
							body := `{"reason":"Fixture","operations":[` + fmt.Sprintf(operation, value) + `]}`
							if err := json.Unmarshal([]byte(body), &input); err != nil {
								t.Fatal(err)
							}
							if got := schema.Validate(input) == nil; got != (value == id) {
								t.Fatalf("formats=%v body=%s accepted=%v", formats, body, got)
							}
						}
					}
				}
			}
		}
	}
}

// Model the final-LF $ behavior used by Python/ECMAScript consumers without a
// second regexp dependency. A native Python consumer is also checked in review.
type terminalNewlineRegexp struct{ *regexp.Regexp }

func (r terminalNewlineRegexp) MatchString(value string) bool {
	return r.Regexp.MatchString(value) || strings.HasSuffix(r.String(), "$") && strings.HasSuffix(value, "\n") && r.Regexp.MatchString(strings.TrimSuffix(value, "\n"))
}
