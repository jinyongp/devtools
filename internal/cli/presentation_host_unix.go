//go:build !windows

package cli

func outputANSI(_ uintptr, terminal string) bool { return terminal != "" && terminal != "dumb" }
