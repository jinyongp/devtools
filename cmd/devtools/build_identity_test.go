package main

import (
	"runtime"
	"testing"
)

func TestPackageIdentityIsTheRuntimeVersionSource(t *testing.T) {
	old := packageIdentity
	t.Cleanup(func() { packageIdentity = old })
	packageIdentity = "devtools-build-v1:0.0.0-package:test-commit:" + runtime.GOOS + ":" + runtime.GOARCH + ":end"
	v, c := buildVersion()
	if v != "0.0.0-package" || c != "test-commit" {
		t.Fatal("package identity not bound to version")
	}
	for _, invalid := range []string{"", "devtools-build-v1:wrong:commit:wrong-os:wrong-arch:end", "devtools-build-v1:missing"} {
		packageIdentity = invalid
		v, c = buildVersion()
		if v != version || c != commit {
			t.Fatal("ordinary linker version contract changed")
		}
	}
}
