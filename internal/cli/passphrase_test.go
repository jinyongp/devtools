package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestAcquireTransferPassphraseInteractiveConfirmation(t *testing.T) {
	var prompts []string
	values := []string{"correct horse battery staple", "correct horse battery staple"}
	prompt := func(_ context.Context, _ IO, label string) (string, *protocol.Error) {
		prompts = append(prompts, label)
		value := values[0]
		values = values[1:]
		return value, nil
	}
	got, err := acquireTransferPassphrase(context.Background(), IO{}, map[string]string{}, "export", true, prompt)
	if err != nil || got != "correct horse battery staple" {
		t.Fatalf("interactive passphrase: %q %+v", got, err)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[1], "Confirm") {
		t.Fatalf("unexpected prompts: %#v", prompts)
	}
}

func TestAcquireTransferPassphraseRejectsMismatchWithoutDisclosure(t *testing.T) {
	values := []string{"FIRST-SECRET-CANARY", "SECOND-SECRET-CANARY"}
	prompt := func(_ context.Context, _ IO, _ string) (string, *protocol.Error) {
		value := values[0]
		values = values[1:]
		return value, nil
	}
	_, err := acquireTransferPassphrase(context.Background(), IO{}, map[string]string{}, "export", true, prompt)
	if err == nil || err.Code != "passphrase_mismatch" {
		t.Fatalf("mismatch result: %+v", err)
	}
	rendered := err.Message
	if strings.Contains(rendered, "FIRST-SECRET-CANARY") || strings.Contains(rendered, "SECOND-SECRET-CANARY") {
		t.Fatalf("passphrase leaked in mismatch error: %+v", err)
	}
}

func TestAcquireTransferPassphraseFileAndStdin(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "passphrase.txt")
	if err := os.WriteFile(path, []byte("FILE-SECRET-CANARY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fromFile, err := acquireTransferPassphrase(context.Background(), IO{}, map[string]string{"passphrase-file": path}, "export", true, nil)
	if err != nil || fromFile != "FILE-SECRET-CANARY" {
		t.Fatalf("file passphrase: %q %+v", fromFile, err)
	}

	fromStdin, stdinErr := acquireTransferPassphrase(context.Background(), IO{In: strings.NewReader("STDIN-SECRET-CANARY\n")}, map[string]string{"passphrase-stdin": "true"}, "import", false, nil)
	if stdinErr != nil || fromStdin != "STDIN-SECRET-CANARY" {
		t.Fatalf("stdin passphrase: %q %+v", fromStdin, stdinErr)
	}
}

func TestAcquireTransferPassphraseRejectsAmbiguousAndMultilineInput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "passphrase.txt")
	if err := os.WriteFile(path, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireTransferPassphrase(context.Background(), IO{In: strings.NewReader("secret")}, map[string]string{"passphrase-file": path, "passphrase-stdin": "true"}, "export", true, nil); err == nil || err.Code != "invalid_argument" {
		t.Fatalf("ambiguous passphrase input accepted: %+v", err)
	}
	if _, err := acquireTransferPassphrase(context.Background(), IO{In: strings.NewReader("one\ntwo")}, map[string]string{"passphrase-stdin": "true"}, "export", true, nil); err == nil || err.Code != "invalid_argument" {
		t.Fatalf("multiline passphrase input accepted: %+v", err)
	}
}

func TestAcquireTransferPassphraseRequiresExplicitInputWithoutTTY(t *testing.T) {
	var diagnostic bytes.Buffer
	_, err := acquireTransferPassphrase(context.Background(), IO{In: strings.NewReader(""), Err: &diagnostic}, map[string]string{}, "export", true, readTerminalPassphrase)
	if err == nil || err.Code != "passphrase_required" {
		t.Fatalf("non-tty default did not require passphrase input: %+v", err)
	}
	if diagnostic.Len() != 0 {
		t.Fatalf("non-tty prompt wrote diagnostics: %q", diagnostic.String())
	}
}

func TestPassphraseStdinRejectsCharacterDevice(t *testing.T) {
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	_, inputErr := acquireTransferPassphrase(context.Background(), IO{In: file}, map[string]string{"passphrase-stdin": "true"}, "import", false, nil)
	if inputErr == nil || inputErr.Code != "invalid_argument" {
		t.Fatalf("character-device stdin accepted: %+v", inputErr)
	}
}

func TestPassphraseOptionConflict(t *testing.T) {
	err := passphraseOptionConflict(map[string]string{"passphrase-file": "secret.txt", "recipient": "age1example"}, "recipient", "recipient-file")
	if err == nil || err.Code != "invalid_argument" || err.Details["field"] != "recipient" {
		t.Fatalf("recipient/passphrase conflict accepted: %+v", err)
	}
}
