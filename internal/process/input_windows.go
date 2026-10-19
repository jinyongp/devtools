package process

import (
	"os"
	"strings"
	"syscall"

	"github.com/jinyongp/devtools/internal/protocol"
)

func utf16Units(s string) uint64 {
	var n uint64
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func validatePlatformExecInput(args, environment []string) *protocol.Error {
	commandUnits := uint64(1)
	for i, arg := range args {
		if i > 0 {
			commandUnits++
		}
		commandUnits += utf16Units(syscall.EscapeArg(arg))
	}
	if commandUnits > 32767 {
		return inputTooLarge("argv_too_large")
	}
	envUnits := uint64(1)
	for _, item := range environment {
		name, value, _ := splitEnvironment(item)
		if utf16Units(name)+1 > 32767 || utf16Units(value)+1 > 32767 {
			return inputTooLarge("environment_too_large")
		}
		envUnits += utf16Units(item) + 1
		if envUnits*2 > MaxExecutionInputBytes {
			return inputTooLarge("environment_too_large")
		}
	}
	return nil
}
func sameEnvironmentName(a, b string) bool { return strings.EqualFold(a, b) }
func CompleteEnvironment(environment []string) []string {
	result := append([]string{}, environment...)
	for _, item := range result {
		name, _, ok := splitEnvironment(item)
		if ok && strings.EqualFold(name, "SYSTEMROOT") {
			return result
		}
	}
	// Match Go's required Windows environment addition now, before the input
	// snapshot is frozen, rather than reading the parent again during spawn.
	return append(result, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
}
