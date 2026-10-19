package process

import (
	"fmt"
	"strings"
	"testing"
)

func TestWindowsUnicodeEnvironmentIsNotLimitedTo32KTotal(t *testing.T) {
	var environment []string
	for i := range 1000 {
		environment = append(environment, fmt.Sprintf("VALUE_%04d=%s", i, strings.Repeat("x", 64)))
	}
	if e := ValidateExecInput([]string{"program.exe"}, environment); e != nil {
		t.Fatal("valid Unicode environment rejected", e)
	}
	if e := ValidateExecInput([]string{"program.exe"}, []string{"TOKEN=" + strings.Repeat("🔒", 16384)}); e == nil || e.Code != "environment_too_large" {
		t.Fatal("UTF-16 variable limit not enforced")
	}
	if e := ValidateExecInput([]string{"program.exe", strings.Repeat(`"`, 20000)}, []string{}); e == nil || e.Code != "argv_too_large" {
		t.Fatal("escaped command line limit not enforced")
	}
}
