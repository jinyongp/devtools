//go:build !linux && !darwin && !windows

package diagnostics

func storageFilesystem(string) (string, bool, bool) { return "unknown", false, false }
