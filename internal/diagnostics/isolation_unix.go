//go:build !windows

package diagnostics

// No portable query establishes the absence of all host sandbox restrictions.
func platformIsolation() string { return "unknown" }
