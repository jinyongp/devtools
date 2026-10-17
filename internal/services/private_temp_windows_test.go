package services

import "testing"

func privateTempDir(t testing.TB) string { return privateTestRoot(t) }
