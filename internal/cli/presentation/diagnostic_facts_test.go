package presentation

import (
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
)

func TestCorrectionFactsSurviveConcisePresentation(t *testing.T) {
	for _, tc := range []struct{ details, want string }{
		{`"sources":[{"kind":"dotenv","line":7,"path":[]}]`, "At line 7."},
		{`"causes":[{"reason":"invalid_type","facts":{"expected_type":"string","observed_type":"number"}}]`, "Expected string; received number."},
		{`"facts":{"limit_kind":"pattern_bytes","observed":66000,"limit":65536}`, "Observed 66000 bytes per pattern; limit 65536."},
		{`"facts":{"limit_kind":"patterns","observed":0,"limit":0}`, "Observed 0 patterns; limit 0."},
		{`"facts":{"input_role":"dotenv","observed":501,"limit":500}`, "Observed 501 dotenv input bytes; limit 500."},
		{`"facts":{"input_role":"variable","observed":65,"limit":64,"observed_exact":false}`, "Read at least 65 variable input bytes; limit 64."},
		{`"facts":{"input_role":"variable","observed":65,"limit":64,"observed_exact":true}`, "Observed 65 variable input bytes; limit 64."},
		{`"sources":[{"kind":"json","path":["operations","0","value","title"]}]`, "At operations[0].value.title."},
	} {
		doc, err := Build("import", []byte(`{"schema_version":1,"ok":false,"error":{"code":"test_error","message":"Correct the input.","details":{`+tc.details+`}}}`))
		if err != nil {
			t.Fatal(err)
		}
		for _, width := range []int{40, 80, 120} {
			out, err := cliui.Render(doc, cliui.Options{Width: width})
			if err != nil || !strings.Contains(strings.Join(strings.Fields(string(out)), " "), tc.want) {
				t.Fatal(string(out), err)
			}
			if strings.Contains(string(out), "column") || strings.Contains(string(out), "Details") {
				t.Fatal(string(out))
			}
		}
	}
}

func TestManualActionsNeverAdvertiseEmptyCommands(t *testing.T) {
	for _, inputs := range []string{`[]`, `["os-authentication"]`} {
		doc, err := Build("vault unlock", []byte(`{"schema_version":1,"ok":false,"error":{"code":"locked","message":"Locked.","details":{"remedies":[{"message":"Authenticate privately.","argv":[],"required_inputs":`+inputs+`,"effect":"value_write"}]}}}`))
		if err != nil {
			t.Fatal(err)
		}
		out, err := cliui.Render(doc, cliui.Options{})
		if err != nil || !strings.Contains(string(out), "Authenticate privately.") || strings.Contains(string(out), "Command") || strings.Contains(string(out), "[]") {
			t.Fatal(string(out), err)
		}
		if inputs != `[]` && !strings.Contains(string(out), "Requires: os-authentication.") {
			t.Fatal(string(out))
		}
	}
}
