package ports

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

type listenerOutput struct{ bytes.Buffer }

func (b *listenerOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 64<<10 {
		return 0, errors.New("listener output exceeded its limit")
	}
	return b.Buffer.Write(data)
}

func listenerProcesses(ctx context.Context, port int) ([]ListenerProcess, error) {
	// macOS ships lsof. Field output avoids parsing localized tables or reading
	// process arguments, which can contain credentials.
	command := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-a", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc")
	var output listenerOutput
	command.Stdout = &output
	err := command.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil && output.Len() == 0 {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var result []ListenerProcess
	pid := 0
	for _, field := range strings.Split(output.String(), "\n") {
		if len(field) < 2 {
			continue
		}
		switch field[0] {
		case 'p':
			pid, _ = strconv.Atoi(field[1:])
		case 'c':
			if pid > 0 {
				if _, identityErr := runProcessIdentity(pid); identityErr == nil {
					result = append(result, ListenerProcess{PID: pid, Program: field[1:]})
				}
			}
		}
	}
	return result, err
}
