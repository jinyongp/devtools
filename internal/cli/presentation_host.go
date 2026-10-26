package cli

import (
	"io"
	"os"
	"runtime"

	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/cliui"
	"golang.org/x/term"
)

func observeOutputEnvironment(streams IO) presentation.Environment {
	terminalName, _ := os.LookupEnv("TERM")
	observe := func(w io.Writer) presentation.Terminal {
		fdWriter, ok := w.(interface{ Fd() uintptr })
		if !ok {
			return presentation.Terminal{}
		}
		fd := fdWriter.Fd()
		if !term.IsTerminal(int(fd)) {
			return presentation.Terminal{}
		}
		width, _, _ := term.GetSize(int(fd))
		return presentation.Terminal{IsTerminal: true, Width: width, ANSI: outputANSI(fd, terminalName)}
	}
	allowed := make(map[string]string, len(presentation.AgentVariables))
	for _, name := range presentation.AgentVariables {
		allowed[name], _ = os.LookupEnv(name)
	}
	format, formatSet := os.LookupEnv("DEVTOOLS_FORMAT")
	color, _ := os.LookupEnv("NO_COLOR")
	return presentation.Environment{
		Stdout: observe(streams.Out), Stderr: observe(streams.Err),
		Signals:   presentation.DetectAgent(allowed).Signals,
		FormatEnv: presentation.FormatInput{Present: formatSet, Raw: format},
		NoColor:   color != "", DumbTerminal: terminalName == "dumb",
	}
}

func textOptions(environment presentation.Environment, failure bool) cliui.Options {
	terminal := environment.Stdout
	if failure {
		terminal = environment.Stderr
	}
	options := presentation.ResolveTextOptions(terminal, environment.NoColor, environment.DumbTerminal)
	options.CommandShells = []cliui.Shell{cliui.POSIX, cliui.Fish}
	if runtime.GOOS == "windows" {
		options.CommandShells = []cliui.Shell{cliui.PowerShell}
	}
	return options
}
