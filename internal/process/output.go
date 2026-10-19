package process

import (
	"golang.org/x/term"
	"io"
	"os"
)

// OutputTTY describes the actual stream, not the identity of its reader. Agents
// can also allocate a PTY; auto's terminal exception is therefore public policy.
func OutputTTY(output io.Writer) bool {
	file, ok := output.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
