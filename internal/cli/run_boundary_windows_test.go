package cli

import (
	"fmt"
	"os"
	"testing"
)

func runBoundaryCommand(t *testing.T) []string {
	t.Helper()
	t.Setenv("DEVTOOLS_TEST_RUN_BOUNDARY", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []string{executable, "-test.run=^TestWindowsRunBoundaryChild$", "--"}
}
func TestWindowsRunBoundaryChild(t *testing.T) {
	if os.Getenv("DEVTOOLS_TEST_RUN_BOUNDARY") != "1" {
		return
	}
	fmt.Printf("%s|", os.Getenv("LEVEL"))
	past := false
	for _, arg := range os.Args[1:] {
		if !past {
			if arg == "--" {
				past = true
			}
			continue
		}
		fmt.Printf("<%s>", arg)
	}
	os.Exit(0)
}
