package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/backup"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProfileTransferPrepareCreatesAndReusesLocalIdentity(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	app := testApp(t)

	code, out, diagnostic := invoke(t, app, "", "profile", "transfer", "prepare")
	if code != 0 || diagnostic != "" {
		t.Fatalf("prepare failed: code=%d out=%s err=%s", code, out, diagnostic)
	}
	if strings.Contains(out, "AGE-SECRET-KEY-") || strings.Contains(out, "identity.txt") {
		t.Fatalf("prepare disclosed private identity metadata: %s", out)
	}
	first := outputData(t, app, []string{"profile", "transfer", "prepare"})
	if first["changed"] != false {
		t.Fatalf("second prepare should reuse identity: %#v", first)
	}
	item := first["item"].(map[string]any)
	recipient, _ := item["recipient"].(string)
	if !strings.HasPrefix(recipient, "age1") {
		t.Fatalf("invalid recipient: %#v", item)
	}

	profiles, err := app.dataDirectory()
	if err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(filepath.Dir(profiles), "profile-transfer", "identity.txt")
	info, statErr := os.Lstat(identity)
	if statErr != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("identity mode: %#v %v", info, statErr)
	}
}

func TestProfileTransferPrepareRejectsUnsafeIdentity(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	app := testApp(t)
	profiles, err := app.dataDirectory()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(filepath.Dir(profiles), "profile-transfer")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(directory, "identity.txt")
	if err := os.WriteFile(identity, []byte("not-an-identity\n"), 0600); err != nil {
		t.Fatal(err)
	}

	code, out, diagnostic := invoke(t, app, "", "profile", "transfer", "prepare")
	if code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"transfer_identity_invalid\"") {
		t.Fatalf("unsafe identity accepted: code=%d out=%s err=%s", code, out, diagnostic)
	}
	if strings.Contains(diagnostic, "AGE-SECRET-KEY-") || strings.Contains(diagnostic, identity) {
		t.Fatalf("unsafe identity path/material leaked: %s", diagnostic)
	}
}

func TestProfileExportUsesExplicitRecipientWithoutBackupConfiguration(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Chdir(root)
	if err := os.WriteFile("devtools.toml", []byte("profile='app'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	outputData(t, app, []string{"var", "set", "MODE", "--profile", "app", "--value", "portable"})
	prepared := outputData(t, app, []string{"profile", "transfer", "prepare"})
	recipient := prepared["item"].(map[string]any)["recipient"].(string)

	configDir := filepath.Join(root, "config", "devtools")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "backup.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}

	exported := outputData(t, app, []string{"profile", "export", "--recipient", recipient})
	item := exported["item"].(map[string]any)
	if item["path"] != "./app.age" || exported["changed"] != true {
		t.Fatalf("unexpected export: %#v", exported)
	}
	info, err := os.Lstat(filepath.Join(root, "app.age"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("archive mode: %#v %v", info, err)
	}

	before, err := os.ReadFile(filepath.Join(root, "app.age"))
	if err != nil {
		t.Fatal(err)
	}
	code, out, diagnostic := invoke(t, app, "", "profile", "export", "--recipient", recipient)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"output_exists\"") || !strings.Contains(diagnostic, "\"remedies\"") || !strings.Contains(diagnostic, "\"required_inputs\":[\"output\"]") {
		t.Fatalf("existing output not preserved or remedy missing: code=%d out=%s err=%s", code, out, diagnostic)
	}
	after, err := os.ReadFile(filepath.Join(root, "app.age"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("existing archive changed: %v", err)
	}
}

func TestProfileExportDirectoryOutputUsesProfileFilename(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Chdir(root)
	if err := os.WriteFile("devtools.toml", []byte("profile='app'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	outputData(t, app, []string{"var", "set", "MODE", "--profile", "app", "--value", "portable"})
	prepared := outputData(t, app, []string{"profile", "transfer", "prepare"})
	recipient := prepared["item"].(map[string]any)["recipient"].(string)
	directory := filepath.Join(root, "exports")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}

	exported := outputData(t, app, []string{"profile", "export", "--recipient", recipient, "--output", directory})
	want := filepath.Join(directory, "app.age")
	if exported["item"].(map[string]any)["path"] != want {
		t.Fatalf("directory output did not resolve profile filename: %#v", exported)
	}
	if info, err := os.Lstat(want); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("directory export missing/private mode: %#v %v", info, err)
	}

	code, out, diagnostic := invoke(t, app, "", "profile", "export", "--recipient", recipient, "--output", directory)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"output_exists\"") || !strings.Contains(diagnostic, want) {
		t.Fatalf("directory collision did not fail fast with resolved path: code=%d out=%s err=%s", code, out, diagnostic)
	}
}

func TestProfileExportExistingOutputFailsBeforeSnapshot(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Chdir(root)
	if err := os.WriteFile("devtools.toml", []byte("profile='missing-profile-data'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	prompted := false
	app.passphrasePrompt = func(_ context.Context, _ IO, _ string) (string, *protocol.Error) {
		prompted = true
		return "SHOULD-NOT-BE-READ", nil
	}
	output := filepath.Join(root, "missing-profile-data.age")
	if err := os.WriteFile(output, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	code, out, diagnostic := invoke(t, app, "", "profile", "export", "--output", output)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"output_exists\"") || !strings.Contains(diagnostic, output) || prompted {
		t.Fatalf("existing output did not fail before missing-profile snapshot: code=%d out=%s err=%s", code, out, diagnostic)
	}
	body, err := os.ReadFile(output)
	if err != nil || string(body) != "keep" {
		t.Fatalf("existing output changed: %q %v", body, err)
	}
}

func TestProfileExportRequiresPassphraseInputWithoutTTY(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Chdir(root)
	if err := os.WriteFile("devtools.toml", []byte("profile='app'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	code, out, diagnostic := invoke(t, app, "", "profile", "export")
	if code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"passphrase_required\"") {
		t.Fatalf("missing passphrase input error: code=%d out=%s err=%s", code, out, diagnostic)
	}
	if !strings.Contains(diagnostic, "--passphrase-file") || !strings.Contains(diagnostic, "--passphrase-stdin") || strings.Contains(diagnostic, "transfer prepare") {
		t.Fatalf("unexpected source-first remedy: %s", diagnostic)
	}
	if _, err := os.Lstat(filepath.Join(root, "app.age")); !os.IsNotExist(err) {
		t.Fatalf("missing passphrase created archive: %v", err)
	}
}

func TestProfileExportAndBackupCreatePublishDifferentRecipientSchemas(t *testing.T) {
	app := testApp(t)
	var profileExport, backupCreate Command
	for _, command := range app.commands {
		switch command.Name {
		case "profile export":
			profileExport = command
		case "backup create":
			backupCreate = command
		}
	}
	if len(profileExport.InputOneOf) != 5 {
		t.Fatalf("profile export should expose default, recipient and passphrase modes: %#v", profileExport.InputOneOf)
	}
	if len(backupCreate.InputOneOf) != 3 {
		t.Fatalf("backup create should retain configured-recipient fallback: %#v", backupCreate.InputOneOf)
	}
}

func TestProfileExportCancellationLeavesNoArchive(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	app := testApp(t)
	outputData(t, app, []string{"var", "set", "MODE", "--profile", "app", "--value", "portable"})
	prepared := outputData(t, app, []string{"profile", "transfer", "prepare"})
	recipient := prepared["item"].(map[string]any)["recipient"].(string)
	output := filepath.Join(root, "canceled.age")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, exportErr := app.exportProfile(ctx, IO{}, Request{Options: map[string]string{
		"profile":   "app",
		"output":    output,
		"recipient": recipient,
	}})
	if exportErr == nil || exportErr.Code != "canceled" || exportErr.ExitCode != 130 {
		t.Fatalf("unexpected cancellation: %+v", exportErr)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("canceled export published an archive: %v", err)
	}
}

func TestProfileSourceFirstInteractiveRoundTrip(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	const passphrase = "SOURCE-FIRST-PASSPHRASE-CANARY"
	source := testApp(t)
	target := testApp(t)
	sourcePrompts := 0
	source.passphrasePrompt = func(_ context.Context, _ IO, _ string) (string, *protocol.Error) {
		sourcePrompts++
		return passphrase, nil
	}
	targetPrompts := 0
	target.passphrasePrompt = func(_ context.Context, _ IO, _ string) (string, *protocol.Error) {
		targetPrompts++
		return passphrase, nil
	}

	outputData(t, source, []string{"var", "set", "MODE", "--profile", "portable", "--value", "source-first"})
	archive := filepath.Join(root, "portable.age")
	code, out, diagnostic := invoke(t, source, "", "profile", "export", "--profile", "portable", "--output", archive)
	if code != 0 || diagnostic != "" || strings.Contains(out+diagnostic, passphrase) {
		t.Fatalf("interactive export failed or leaked passphrase: code=%d out=%s err=%s", code, out, diagnostic)
	}
	if sourcePrompts != 2 {
		t.Fatalf("export prompt count = %d", sourcePrompts)
	}
	mode, err := backup.ArchiveEncryptionMode(archive)
	if err != nil || mode != backup.ArchiveEncryptionPassphrase {
		t.Fatalf("interactive archive mode = %q, %v", mode, err)
	}

	base := []string{"profile", "import", "--file", archive}
	preview := outputData(t, target, base)
	apply := append(append([]string{}, base...), "--apply", preview["digest"].(string), "--request-id", tasks.ID())
	imported := outputData(t, target, apply)
	if imported["changed"] != true || targetPrompts != 2 {
		t.Fatalf("interactive import failed/prompts=%d: %#v", targetPrompts, imported)
	}
	value := outputData(t, target, []string{"var", "get", "MODE", "--profile", "portable"})
	if value["value"] != "source-first" {
		t.Fatalf("source-first round trip mismatch: %#v", value)
	}
	profiles, pathErr := target.dataDirectory()
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(profiles), "profile-transfer", "identity.txt")); !os.IsNotExist(err) {
		t.Fatalf("source-first import created prepared identity: %v", err)
	}
}

func TestProfileImportUsesLocalTransferIdentityAndRecoveryArchive(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	source := testApp(t)
	target := testApp(t)
	outputData(t, source, []string{"var", "set", "MODE", "--profile", "portable", "--value", "source"})
	prepared := outputData(t, target, []string{"profile", "transfer", "prepare"})
	recipient := prepared["item"].(map[string]any)["recipient"].(string)
	archive := filepath.Join(root, "portable.age")
	outputData(t, source, []string{"profile", "export", "--profile", "portable", "--output", archive, "--recipient", recipient})

	base := []string{"profile", "import", "--file", archive}
	preview := outputData(t, target, base)
	if preview["changed"] != false || preview["target_exists"] != false {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	apply := append(append([]string{}, base...), "--apply", preview["digest"].(string), "--request-id", tasks.ID())
	imported := outputData(t, target, apply)
	if imported["changed"] != true || imported["safety_backup"] != "" {
		t.Fatalf("unexpected import: %#v", imported)
	}
	value := outputData(t, target, []string{"var", "get", "MODE", "--profile", "portable"})
	if value["value"] != "source" {
		t.Fatalf("local identity import failed: %#v", value)
	}

	outputData(t, target, []string{"var", "set", "MODE", "--profile", "portable", "--value", "changed"})
	replacePreview := outputData(t, target, base)
	replace := append(append([]string{}, base...), "--replace", "--apply", replacePreview["digest"].(string), "--request-id", tasks.ID())
	replaced := outputData(t, target, replace)
	safety, _ := replaced["safety_backup"].(string)
	if !replaced["changed"].(bool) || safety == "" {
		t.Fatalf("replace did not create safety archive: %#v", replaced)
	}
	if !strings.Contains(filepath.ToSlash(safety), "/profile-transfer/recovery/before-import-") {
		t.Fatalf("unexpected safety archive path: %s", safety)
	}
	info, err := os.Lstat(safety)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("safety archive mode: %#v %v", info, err)
	}

	recoveryBase := []string{"profile", "import", "--file", safety, "--as", "recovered"}
	recoveryPreview := outputData(t, target, recoveryBase)
	recoveryApply := append(append([]string{}, recoveryBase...), "--apply", recoveryPreview["digest"].(string), "--request-id", tasks.ID())
	recovered := outputData(t, target, recoveryApply)
	if recovered["changed"] != true {
		t.Fatalf("safety archive recovery failed: %#v", recovered)
	}
	oldValue := outputData(t, target, []string{"var", "get", "MODE", "--profile", "recovered"})
	if oldValue["value"] != "changed" {
		t.Fatalf("safety archive did not preserve pre-replace state: %#v", oldValue)
	}
}

func TestProfileImportRecipientArchiveWithoutPreparedIdentityFailsWithoutCreatingOne(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	source := testApp(t)
	target := testApp(t)
	outputData(t, source, []string{"var", "set", "MODE", "--profile", "portable", "--value", "source"})
	identity := filepath.Join(root, "external-identity.txt")
	recipientPath := filepath.Join(root, "external-recipient.txt")
	if code, _, diagnostic := invoke(t, source, "", "backup", "keygen", "--identity-file", identity, "--recipient-file", recipientPath); code != 0 {
		t.Fatal(diagnostic)
	}
	recipientBytes, err := os.ReadFile(recipientPath)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "recipient.age")
	outputData(t, source, []string{"profile", "export", "--profile", "portable", "--output", archive, "--recipient", strings.TrimSpace(string(recipientBytes))})

	code, out, diagnostic := invoke(t, target, "", "profile", "import", "--file", archive)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"transfer_identity_missing\"") || !strings.Contains(diagnostic, "\"remedies\"") {
		t.Fatalf("missing prepared identity error/remedy: code=%d out=%s err=%s", code, out, diagnostic)
	}
	profiles, pathErr := target.dataDirectory()
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	localIdentity := filepath.Join(filepath.Dir(profiles), "profile-transfer", "identity.txt")
	if _, err := os.Lstat(localIdentity); !os.IsNotExist(err) {
		t.Fatalf("import created identity implicitly: %v", err)
	}
}

func TestProfileImportExplicitIdentityOverridesLocalIdentity(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	source := testApp(t)
	target := testApp(t)
	outputData(t, source, []string{"var", "set", "MODE", "--profile", "portable", "--value", "external"})
	outputData(t, target, []string{"profile", "transfer", "prepare"})

	identity := filepath.Join(root, "external-identity.txt")
	recipientPath := filepath.Join(root, "external-recipient.txt")
	if code, _, diagnostic := invoke(t, target, "", "backup", "keygen", "--identity-file", identity, "--recipient-file", recipientPath); code != 0 {
		t.Fatal(diagnostic)
	}
	recipientBytes, err := os.ReadFile(recipientPath)
	if err != nil {
		t.Fatal(err)
	}
	recipient := strings.TrimSpace(string(recipientBytes))
	archive := filepath.Join(root, "external.age")
	outputData(t, source, []string{"profile", "export", "--profile", "portable", "--output", archive, "--recipient", recipient})

	if code, out, diagnostic := invoke(t, target, "", "profile", "import", "--file", archive); code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"invalid_backup\"") {
		t.Fatalf("archive unexpectedly decrypted with local identity: code=%d out=%s err=%s", code, out, diagnostic)
	}
	preview := outputData(t, target, []string{"profile", "import", "--file", archive, "--identity-file", identity})
	if preview["changed"] != false || preview["target_exists"] != false {
		t.Fatalf("explicit identity preview failed: %#v", preview)
	}
	apply := []string{"profile", "import", "--file", archive, "--identity-file", identity, "--apply", preview["digest"].(string), "--request-id", tasks.ID()}
	imported := outputData(t, target, apply)
	if imported["changed"] != true {
		t.Fatalf("explicit identity apply failed: %#v", imported)
	}
	value := outputData(t, target, []string{"var", "get", "MODE", "--profile", "portable"})
	if value["value"] != "external" {
		t.Fatalf("explicit identity imported wrong state: %#v", value)
	}
}

func TestProfileImportRecoveryFailurePreventsReplacement(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	source := testApp(t)
	target := testApp(t)
	outputData(t, source, []string{"var", "set", "MODE", "--profile", "portable", "--value", "source"})
	outputData(t, target, []string{"var", "set", "MODE", "--profile", "portable", "--value", "before"})
	prepared := outputData(t, target, []string{"profile", "transfer", "prepare"})
	recipient := prepared["item"].(map[string]any)["recipient"].(string)
	archive := filepath.Join(root, "replace.age")
	outputData(t, source, []string{"profile", "export", "--profile", "portable", "--output", archive, "--recipient", recipient})

	base := []string{"profile", "import", "--file", archive}
	preview := outputData(t, target, base)
	if preview["target_exists"] != true {
		t.Fatalf("replacement target missing: %#v", preview)
	}
	profiles, err := target.dataDirectory()
	if err != nil {
		t.Fatal(err)
	}
	recovery := filepath.Join(filepath.Dir(profiles), "profile-transfer", "recovery")
	if err := os.Mkdir(recovery, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(recovery, 0755); err != nil {
		t.Fatal(err)
	}
	apply := append(append([]string{}, base...), "--replace", "--apply", preview["digest"].(string), "--request-id", tasks.ID())
	code, out, diagnostic := invoke(t, target, "", apply...)
	if code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"backup_error\"") {
		t.Fatalf("unsafe recovery directory accepted: code=%d out=%s err=%s", code, out, diagnostic)
	}
	value := outputData(t, target, []string{"var", "get", "MODE", "--profile", "portable"})
	if value["value"] != "before" {
		t.Fatalf("replacement mutated target before safety backup: %#v", value)
	}
}
