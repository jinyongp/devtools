package presentation

import "testing"

func TestSelectionAndDetection(t *testing.T) {
	for _, tc := range []struct {
		flag, env string
		set       bool
		want      Format
		source    string
		invalid   bool
	}{
		{"", "", false, DefaultFormat, "default", false},
		{"", "text", false, FormatText, "env", false},
		{"auto", "text", true, FormatAuto, "flag", false},
		{"json", "secret-looking-invalid-value", true, FormatJSON, "flag", false},
		{"", "JSON", false, "JSON", "env", true},
		{"", " text", false, " text", "env", true},
		{"", "json", true, "", "flag", true},
	} {
		s, err := Select(FormatInput{tc.set, tc.flag}, FormatInput{true, tc.env})
		if (err != nil) != tc.invalid || s.Format != tc.want || s.Source != tc.source {
			t.Errorf("selection flag=%q env=%q: %#v %v", tc.flag, tc.env, s, err)
		}
	}
	d := DetectAgent(map[string]string{"CI": "1", "CODEX_SANDBOX": "1", "AI_AGENT": "false", "CLAUDECODE": " TRUE ", "CODEX_THREAD_ID": "test-session", "GEMINI_CLI": "no", "OPENCODE_CLIENT": "0"})
	if len(d.Signals) != 2 || d.Signals[0] != "CODEX_THREAD_ID" || d.Signals[1] != "CLAUDECODE" {
		t.Fatalf("unexpected signals: %#v", d)
	}
}

func TestResolutionSeparatesFormatAndDecoration(t *testing.T) {
	for _, tc := range []struct {
		selection  Selection
		agent, tty bool
		cap        Capability
		want       Format
		conflict   bool
	}{
		{Selection{FormatAuto, "default"}, false, true, Adaptable, FormatText, false},
		{Selection{FormatAuto, "default"}, true, true, Adaptable, FormatJSON, false},
		{Selection{FormatAuto, "default"}, false, false, Adaptable, FormatJSON, false},
		{Selection{FormatText, "flag"}, true, false, Adaptable, FormatText, false},
		{Selection{FormatText, "env"}, false, true, MachineOnly, FormatJSON, false},
		{Selection{FormatText, "flag"}, false, true, MachineOnly, FormatJSON, true},
	} {
		detection := Detection{}
		if tc.agent {
			detection.Signals = []string{"AI_AGENT"}
		}
		d, err := Resolve(tc.selection, detection, tc.tty, tc.cap)
		if d.Format != tc.want || (err != nil) != tc.conflict {
			t.Errorf("%#v: %#v %v", tc, d, err)
		}
	}
	terminal := Terminal{IsTerminal: true, Width: 79, ANSI: true}
	if o := ResolveTextOptions(terminal, true, false); !o.SGR || o.Color || o.Width != 79 {
		t.Fatal(o)
	}
	if o := ResolveTextOptions(terminal, false, true); o.SGR || o.Color {
		t.Fatal(o)
	}
}
