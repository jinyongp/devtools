package main

import (
	"runtime"
	"strings"
)

// packageIdentity is set only by the archive builder. Unlike Go's -ldflags build
// setting (omitted with -trimpath), this value remains in stripped binaries and
// is also the source of their runtime version response. Ordinary -X main.version
// and -X main.commit builds retain their existing behavior.
var packageIdentity string

func buildVersion() (string, string) {
	fields := strings.Split(packageIdentity, ":")
	if len(fields) == 6 && fields[0] == "devtools-build-v1" && fields[1] != "" && fields[2] != "" && fields[3] == runtime.GOOS && fields[4] == runtime.GOARCH && fields[5] == "end" {
		return fields[1], fields[2]
	}
	return version, commit
}
