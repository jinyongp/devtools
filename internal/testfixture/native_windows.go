// Package testfixture supplies native executable fixtures to Windows tests.
// Production commands never import this package.
package testfixture

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// CopyIn copies the already-built test image. The distinctive basename makes
// the child's fixture entry explicit before the Go test flag parser runs.
func CopyIn(t testing.TB, directory, mode string) string {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "devtools-fixture-"+mode+".exe")
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func Copy(t testing.TB, mode string) string { t.Helper(); return CopyIn(t, t.TempDir(), mode) }
func init() {
	name := strings.ToLower(filepath.Base(os.Args[0]))
	if !strings.HasPrefix(name, "devtools-fixture-") || !strings.HasSuffix(name, ".exe") {
		return
	}
	mode := strings.TrimSuffix(strings.TrimPrefix(name, "devtools-fixture-"), ".exe")
	args := os.Args[1:]
	switch mode {
	case "true":
	case "print":
		fmt.Print(strings.Join(args, ""))
	case "check-marker":
		if os.Getenv("TOKEN") == "" {
			os.Exit(1)
		}
		if err := os.WriteFile("marker", []byte("ran"), 0600); err != nil {
			os.Exit(1)
		}
	case "bind":
		if len(args) != 1 {
			os.Exit(2)
		}
		fmt.Printf("%s|%s", os.Getenv("URL"), args[0])
	case "output-fail":
		fmt.Print("output")
		fmt.Fprint(os.Stderr, "diagnostic")
		os.Exit(17)
	case "version-bad":
		fmt.Print("fixture 1.2.30 CANARY-SECRET\n")
	case "version":
		fmt.Print("tool 1.2.3 fixture-secret\n")
	case "version-fail":
		fmt.Fprint(os.Stderr, "fixture-secret\n")
		os.Exit(1)
	case "env":
		if len(args) != 1 {
			os.Exit(2)
		}
		fmt.Print(os.Getenv(args[0]))
	case "cwd-level-first":
		cwd, _ := os.Getwd()
		first := ""
		if len(args) > 0 {
			first = args[0]
		}
		fmt.Printf("%s|%s|%s", cwd, os.Getenv("LEVEL"), first)
	case "cwd-level":
		cwd, _ := os.Getwd()
		fmt.Printf("%s|%s", cwd, os.Getenv("LEVEL"))
	case "raw-fail":
		fmt.Print("raw")
		fmt.Fprint(os.Stderr, "diagnostic")
		os.Exit(23)
	case "secret-and-true":
		fmt.Print(os.Getenv("TOKEN"))
		fmt.Print("true")
	case "plain":
		fmt.Print("plain")
	case "cat":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "exit":
		if len(args) != 1 {
			os.Exit(2)
		}
		code, err := strconv.Atoi(args[0])
		if err != nil {
			os.Exit(2)
		}
		os.Exit(code)
	case "hold":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		<-signals
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
