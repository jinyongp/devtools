//go:build linux || darwin

package process

import "github.com/jinyongp/devtools/internal/protocol"

func validatePlatformExecInput(args, environment []string) *protocol.Error {
	totalLimit, singleLimit, pointerBytes, err := nativeExecLimits()
	if err != nil {
		return err
	}
	// Reserve space for the resolved executable, interpreter and kernel-added
	// strings. This is conservative preflight, not an exec-success guarantee.
	total := uint64(8192) + uint64(len(args)+len(environment)+2)*pointerBytes
	for index, group := range [][]string{args, environment} {
		for _, item := range group {
			n := uint64(len(item)) + 1
			if singleLimit > 0 && n > singleLimit {
				if index == 0 {
					return inputTooLarge("argv_too_large")
				}
				return inputTooLarge("environment_too_large")
			}
			total += n
			if total > totalLimit {
				return inputTooLarge("execution_input_too_large")
			}
		}
	}
	return nil
}
