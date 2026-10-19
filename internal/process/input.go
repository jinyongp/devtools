package process

import (
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/protocol"
)

// This memory/transport ceiling is independent of the smaller OS exec limits.
const MaxExecutionInputBytes = 16 << 20

func inputTooLarge(code string) *protocol.Error {
	return protocol.NewError(code, "Execution input exceeds the supported process limit.", 2, nil)
}
func ValidateExecInput(args, environment []string) *protocol.Error {
	if len(args) == 0 || args[0] == "" {
		return protocol.NewError("invalid_argument", "Supply an executable.", 2, nil)
	}
	var size uint64
	for _, group := range [][]string{args, environment} {
		for _, item := range group {
			if strings.ContainsRune(item, 0) || !utf8.ValidString(item) {
				return protocol.NewError("invalid_argument", "Execution input cannot be represented safely.", 2, nil)
			}
			size += uint64(len(item)) + 1
			if size > MaxExecutionInputBytes {
				return inputTooLarge("execution_input_too_large")
			}
		}
	}
	for _, item := range environment {
		name, _, ok := splitEnvironment(item)
		if !ok || name == "" {
			return protocol.NewError("invalid_argument", "Execution environment contains an invalid entry.", 2, nil)
		}
	}
	return validatePlatformExecInput(args, environment)
}
