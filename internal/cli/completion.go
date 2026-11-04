package cli

import (
	"context"
	_ "embed"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

//go:embed completion_bash.sh
var bashCompletionScript string

//go:embed completion_zsh.zsh
var zshCompletionScript string

//go:embed completion_fish.fish
var fishCompletionScript string

//go:embed completion_powershell.ps1
var powershellCompletionScript string

func (a *App) registerCompletion() {
	a.commands = append(a.commands, Command{Name: "completion", Description: "Generate shell completion for zsh, bash, fish, or PowerShell.", Arguments: []Argument{{Name: "shell", Required: true, Pattern: "^(zsh|bash|fish|powershell)$"}}, OutputMode: OutputArtifact, Run: func(_ context.Context, streams IO, r Request) (any, *protocol.Error) {
		if err := writeHelp(streams.Out, a.completionScript(r.Args[0])); err != nil {
			return nil, protocol.NewError("io_error", "Cannot write completion script.", 1, nil)
		}
		return nil, nil
	}})
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// Adapters own shell quoting and registration. The invoked CLI owns all
// candidate selection, including command/option names and their descriptions.
func (a *App) completionScript(shell string) string {
	switch shell {
	case "bash":
		return bashCompletionScript
	case "zsh":
		return zshCompletionScript
	case "fish":
		return fishCompletionScript
	case "powershell":
		return powershellCompletionScript
	}
	return ""
}
