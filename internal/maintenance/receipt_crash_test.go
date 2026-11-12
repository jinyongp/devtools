package maintenance

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const crashProfile = "profiles/61.json"
const crashReceipt = "backup-receipts/values-61-00000000-0000-4000-8000-000000000001.json"

// Test-only subprocess driver: production code has no environment failpoint.
// Opaque profile/receipt bytes isolate the storage engine's atomicity contract;
// values package process tests separately verify semantic replay and encryption.
func TestValueReceiptTransactionCrashHelper(t *testing.T) {
	mode := os.Getenv("DEVTOOLS_TEST_RECEIPT_PHASE")
	if mode == "" {
		t.Skip("owned subprocess only")
	}
	root := os.Getenv("DEVTOOLS_TEST_RECEIPT_ROOT")
	ctx := context.Background()
	if strings.HasPrefix(mode, "verify-") {
		release, err := AcquireShared(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		committed := mode == "verify-committed"
		want := "profile-old"
		if committed {
			want = "profile-new"
		}
		body, err := Read(filepath.Join(root, crashProfile), 1024)
		if err != nil || string(body) != want {
			t.Fatal("profile recovery disagreed with durable commit", err)
		}
		body, err = Read(filepath.Join(root, crashReceipt), 1024)
		if committed {
			if err != nil || string(body) != "saved-result-new" {
				t.Fatal("committed profile lost matching receipt", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("uncommitted receipt survived", err)
		}
		fmt.Println("verified")
		return
	}
	release, err := AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	files := map[string][]byte{crashProfile: []byte("profile-new"), crashReceipt: []byte("saved-result-new")}
	if mode == "response" {
		if err := Replace(root, files); err != nil {
			t.Fatal(err)
		}
	} else if mode != "candidate" {
		pointer, err := prepareTransaction(root, files)
		if err != nil {
			t.Fatal(err)
		}
		switch mode {
		case "prepared":
		case "first-replacement":
			// Use the same replacement function on the first sorted target.
			if err := applyReplacement(root, map[string][]byte{crashReceipt: files[crashReceipt]}); err != nil {
				t.Fatal(err)
			}
		case "all-replacements", "durable-commit":
			if err := applyReplacement(root, files); err != nil {
				t.Fatal(err)
			}
			if mode == "durable-commit" {
				committed, err := markCommitted(root, pointer)
				if err != nil || !committed {
					t.Fatal("commit pointer not durable", err)
				}
			}
		default:
			t.Fatal("invalid test phase")
		}
	}
	fmt.Println("checkpoint")
	// Parent owns the stdin pipe and kills this process while the write gate is
	// held. No deferred releases or simulated graceful recovery can run.
	var signal [1]byte
	if _, err := io.ReadFull(os.Stdin, signal[:]); err != nil {
		t.Fatal("checkpoint released without kill")
	}
}

func TestValueReceiptTransactionSurvivesRealProcessKill(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"candidate", "prepared", "first-replacement", "all-replacements", "durable-commit", "response"} {
		t.Run(phase, func(t *testing.T) {
			root := privateTempDir(t)
			release, err := AcquireExclusive(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			err = Write(filepath.Join(root, crashProfile), []byte("profile-old"))
			release()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := func(mode string) *exec.Cmd {
				c := exec.CommandContext(ctx, executable, "-test.run=^TestValueReceiptTransactionCrashHelper$", "-test.timeout=15s")
				c.Env = append(os.Environ(), "DEVTOOLS_TEST_RECEIPT_PHASE="+mode, "DEVTOOLS_TEST_RECEIPT_ROOT="+root)
				return c
			}
			writer := command(phase)
			stdin, err := writer.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := writer.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var diagnostic bytes.Buffer
			writer.Stderr = &diagnostic
			if err := writer.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					_ = writer.Process.Kill()
					_ = writer.Wait()
				}
			}()
			scanner := bufio.NewScanner(stdout)
			if !scanner.Scan() || scanner.Text() != "checkpoint" {
				t.Fatal("writer did not reach owned checkpoint")
			}
			if err := writer.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Wait(); err == nil {
				t.Fatal("writer was not forcibly terminated")
			}
			waited = true
			mode := "verify-rolled-back"
			if phase == "durable-commit" || phase == "response" {
				mode = "verify-committed"
			}
			out, err := command(mode).CombinedOutput()
			if err != nil || !bytes.Contains(out, []byte("verified")) {
				t.Fatalf("fresh reader did not recover atomic profile/receipt pair: %v\n%s", err, out)
			}
		})
	}
}
