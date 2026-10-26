package cliui

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestDocumentOwnershipAndValidation(t *testing.T) {
	fields := []Field{{Literal("Name"), Literal("original")}}
	block := Fields(fields...)
	document := NewDocument(block)
	fields[0].Value.Spans[0].Text = "changed"
	copy := document.Blocks()
	copy[0].fields[0].Value.Spans[0].Text = "changed again"
	encoded, err := Render(document, Options{})
	if err != nil || !strings.Contains(string(encoded), "original") || strings.Contains(string(encoded), "changed") {
		t.Fatalf("mutation crossed document boundary: %s %v", encoded, err)
	}
	if _, err := Table([]Column{{Literal("one")}}, []Row{{Cells: []Text{Literal("one"), Literal("two")}}}); err != ErrInvalidDocument {
		t.Fatal("mismatched table was accepted", err)
	}
	for _, d := range []Document{{}, NewDocument(Block{}), NewDocument(Heading(Literal("title"), 3)), NewDocument(Paragraph(Text{Flow: 9})), NewDocument(Paragraph(Styled("bad", 99)))} {
		if out, err := Render(d, Options{}); err != ErrInvalidDocument || len(out) != 0 {
			t.Fatalf("invalid model produced output: %q %v", out, err)
		}
	}
}

func TestTableAndStyleFallbacks(t *testing.T) {
	makeDoc := func(name string) Document {
		b, err := Table([]Column{{Literal("Name")}, {Literal("State")}}, []Row{{Cells: []Text{Literal(name), Styled("READY", Success)}}})
		if err != nil {
			t.Fatal(err)
		}
		return NewDocument(Heading(Literal("Items"), 1), b)
	}
	for _, tc := range []struct {
		name    string
		width   int
		stacked bool
	}{{"한글", 80, false}, {"short", 79, true}, {"short", 40, true}, {"emoji🙂", 120, true}, {"e\u0301", 120, true}, {strings.Repeat("x", 120), 120, true}} {
		out, err := Render(makeDoc(tc.name), Options{Width: tc.width})
		if err != nil {
			t.Fatal(err)
		}
		if stacked := !strings.Contains(string(out), "Name  State"); stacked != tc.stacked {
			t.Errorf("name=%q width=%d: %s", tc.name, tc.width, out)
		}
	}
	doc := makeDoc("short")
	plain, _ := Render(doc, Options{Width: 80})
	styled, _ := Render(doc, Options{Width: 80, SGR: true, Color: true})
	strip := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	if string(plain) != strip.ReplaceAllString(string(styled), "") || !strings.Contains(string(styled), "\x1b[32mREADY\x1b[0m") {
		t.Fatalf("styles changed content: %q %q", plain, styled)
	}
	if escaped := Escape("safe\x1b[31m\r\x00\u202eevil"); strings.ContainsAny(escaped, "\x1b\r\x00\u202e") || !strings.Contains(escaped, `\u001b`) {
		t.Fatal(escaped)
	}
}

func TestCommandLineArgvHelper(t *testing.T) {
	if os.Getenv("DEVTOOLS_UI_ARGV_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			_ = json.NewEncoder(os.Stdout).Encode(os.Args[i+1:])
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func TestCommandLineRoundTrip(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"two words", "한글", "", "don't", `a\b\\`, "$(printf injected)", "`printf injected`", "*.go", "a;b", "a&b", `a"b`, "!history", "#comment"}
	argv := append([]string{executable, "-test.run=^TestCommandLineArgvHelper$", "--"}, want...)
	for _, tc := range []struct {
		name  string
		shell Shell
	}{{"sh", POSIX}, {"bash", POSIX}, {"zsh", POSIX}, {"fish", Fish}} {
		t.Run(tc.name, func(t *testing.T) {
			binary, err := exec.LookPath(tc.name)
			if err != nil {
				t.Skip("shell is not installed")
			}
			line, ok := commandLine(argv, tc.shell)
			if !ok || strings.Contains(line, "\n") {
				t.Fatal(line, ok)
			}
			cmd := exec.Command(binary, "-c", line)
			cmd.Env = append(os.Environ(), "DEVTOOLS_UI_ARGV_HELPER=1")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v %s", line, err, out)
			}
			var got []string
			if json.Unmarshal(out, &got) != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("argv changed: want=%q got=%q output=%s", want, got, out)
			}
		})
	}
}

func TestCopyLineHasNoPresentationBytes(t *testing.T) {
	argv := []string{"devtools", "command", "list", "--dir", "/workspace/my project"}
	block, _ := CommandLine(argv)
	argv[0] = "changed"
	doc := NewDocument(Section(Styled("Next", Info), Paragraph(Words("List configured commands.")), block))
	want := "devtools command list --dir '/workspace/my project'"
	for _, width := range []int{40, 80, 120} {
		out, err := Render(doc, Options{Width: width, SGR: true, Color: true, CommandShells: []Shell{POSIX, Fish}})
		if err != nil || strings.Count(string(out), want+"\n") != 1 {
			t.Fatalf("%d: %q %v", width, out, err)
		}
		found := false
		for _, line := range strings.Split(string(out), "\n") {
			if line == want {
				found = true
			}
		}
		if !found {
			t.Fatal("copy line has indentation, styling, or wrapping", string(out))
		}
	}
	for _, shell := range []Shell{POSIX, Fish, PowerShell} {
		if _, ok := commandLine([]string{"devtools", "newline\nargument"}, shell); ok {
			t.Fatal("control token advertised as copyable")
		}
	}
}
