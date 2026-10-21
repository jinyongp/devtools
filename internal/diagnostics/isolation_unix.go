//go:build !windows

package diagnostics

// No portable query establishes the absence of all host sandbox restrictions.
func platformIsolation() string { return "unknown" }

func platformPackageIdentity() string { return "not_applicable" }

func storageMappingLimits(string) []string { return nil }
