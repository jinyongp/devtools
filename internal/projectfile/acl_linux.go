package projectfile

import "os"

// Linux POSIX ACLs are included in the descriptor-based xattr copy/verification.
func extendedACL(*os.File) (string, error)     { return "", nil }
func copyExtendedACL(*os.File, *os.File) error { return nil }
