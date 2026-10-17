//go:build !windows

package expose

import (
	"strconv"
	"strings"
)

func platformTailscaleVersion(value string) bool {
	version := strings.Split(value, ".")
	if len(version) < 2 {
		return false
	}
	major, _ := strconv.Atoi(version[0])
	minor, _ := strconv.Atoi(version[1])
	return major > 1 || major == 1 && minor >= 52
}
