package process

import (
	"os"
	"strings"
	"testing"
)

func TestLinuxExecSingleStringLimit(t *testing.T) {
	value := strings.Repeat("x", 32*os.Getpagesize())
	if e := ValidateExecInput([]string{"program"}, []string{"TOKEN=" + value}); e == nil || e.Code != "environment_too_large" {
		t.Fatal("Linux per-string limit not enforced")
	}
	if e := ValidateExecInput([]string{"program", value}, []string{}); e == nil || e.Code != "argv_too_large" {
		t.Fatal("Linux argument limit not enforced")
	}
}
