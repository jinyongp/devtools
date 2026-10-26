// Package presentation selects an output format and maps public response values
// to terminal documents. It does not observe the host or execute commands.
package presentation

import (
	"errors"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

type Format string

const (
	FormatAuto Format = "auto"
	FormatText Format = "text"
	FormatJSON Format = "json"
	DefaultFormat = FormatAuto
)

type Capability uint8

const (
	Adaptable Capability = iota
	MachineOnly
	Native
)

type Terminal struct {
	IsTerminal bool
	Width      int
	ANSI       bool
}
type Environment struct {
	Stdout, Stderr Terminal
	Signals        []string
	FormatEnv      FormatInput
	NoColor        bool
	DumbTerminal   bool
}
type FormatInput struct {
	Present bool
	Raw     string
}
type Selection struct {
	Format Format
	Source string
}
type Detection struct{ Signals []string }
type Decision struct {
	Format Format
	Source string
	Reason string
}

var ErrInvalidFormat = errors.New("format must be auto, text, or json")
var ErrMachineText = errors.New("machine discovery requires JSON")

// AgentVariables is also the observer allowlist, in deterministic signal order.
var AgentVariables = [...]string{"AI_AGENT", "CODEX_THREAD_ID", "CODEX_CI", "CLAUDECODE", "CLAUDE_CODE", "CURSOR_AGENT", "GEMINI_CLI", "OPENCODE", "OPENCODE_CLIENT"}

func DetectAgent(environment map[string]string) Detection {
	result := Detection{Signals: []string{}}
	for _, name := range AgentVariables {
		value := strings.ToLower(strings.TrimSpace(environment[name]))
		positive := value == "1" || value == "true"
		if name == "AI_AGENT" || name == "CODEX_THREAD_ID" || name == "OPENCODE_CLIENT" {
			positive = value != "" && value != "0" && value != "false"
		}
		if positive {
			result.Signals = append(result.Signals, name)
		}
	}
	return result
}

func Select(flag, env FormatInput) (Selection, error) {
	s := Selection{Format: DefaultFormat, Source: "default"}
	if flag.Present {
		s = Selection{Format: Format(flag.Raw), Source: "flag"}
	} else if env.Present && env.Raw != "" {
		s = Selection{Format: Format(env.Raw), Source: "env"}
	}
	if s.Format != FormatAuto && s.Format != FormatText && s.Format != FormatJSON {
		return s, ErrInvalidFormat
	}
	return s, nil
}

func Resolve(s Selection, detected Detection, stdoutTerminal bool, capability Capability) (Decision, error) {
	d := Decision{Format: s.Format, Source: s.Source, Reason: "explicit"}
	if capability == MachineOnly {
		d = Decision{Format: FormatJSON, Source: "machine-contract", Reason: "machine-contract"}
		if s.Source == "flag" && s.Format == FormatText {
			return d, ErrMachineText
		}
		return d, nil
	}
	if s.Format != FormatAuto {
		return d, nil
	}
	d.Format = FormatJSON
	switch {
	case len(detected.Signals) > 0:
		d.Reason = "agent"
	case !stdoutTerminal:
		d.Reason = "non-terminal"
	default:
		d.Format = FormatText
		d.Reason = "terminal"
	}
	return d, nil
}

func ResolveTextOptions(terminal Terminal, noColor, dumb bool) cliui.Options {
	sgr := terminal.IsTerminal && terminal.ANSI && !dumb
	return cliui.Options{Width: terminal.Width, SGR: sgr, Color: sgr && !noColor}
}
