package profilekey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalKeyAndIdentity(t *testing.T) {
	profile := strings.Repeat("A", 128)
	key := Key(profile)
	if len(key) != 67 || !ValidKey(key) || key != strings.ToLower(key) {
		t.Fatalf("key=%q", key)
	}
	dir := t.TempDir()
	identityDir := filepath.Join(dir, ".identity")
	if err := os.Mkdir(identityDir, 0700); err != nil {
		t.Fatal(err)
	}
	body, err := IdentityBytes(profile, "values")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(IdentityPath(dir, profile), body, 0600); err != nil {
		t.Fatal(err)
	}
	ok, err := ReadIdentity(dir, profile, "values")
	if err != nil || !ok {
		t.Fatalf("identity=%v %v", ok, err)
	}
	names, err := EnumerateIdentities(dir, "values")
	if err != nil || len(names) != 1 || names[0] != profile {
		t.Fatalf("identities=%#v %v", names, err)
	}
}

func TestLegacyPathStatusUsesFilesystemAddressability(t *testing.T) {
	dir := t.TempDir()
	addressable := strings.Repeat("a", 125)
	status, err := LegacyPathStatus(dir, addressable)
	if err != nil || status != LegacyMissing {
		t.Fatalf("125 status=%v err=%v", status, err)
	}
	if err := os.WriteFile(LegacyPath(dir, addressable), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err = LegacyPathStatus(dir, addressable)
	if err != nil || status != LegacyExisting {
		t.Fatalf("existing status=%v err=%v", status, err)
	}

	unaddressable := strings.Repeat("b", 126)
	status, err = LegacyPathStatus(dir, unaddressable)
	if err != nil || status != LegacyUnaddressable {
		t.Fatalf("126 status=%v err=%v", status, err)
	}
}

func TestMarkerRecognitionIsFailClosed(t *testing.T) {
	dir := t.TempDir()
	profile := "app"
	path := LegacyPath(dir, profile)
	body, err := MarkerBytes(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	marker, err := ReadMarker(path, profile)
	if err != nil || !marker {
		t.Fatalf("marker=%v err=%v", marker, err)
	}

	bad, _ := MarkerBytes("other")
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	marker, err = ReadMarker(path, profile)
	if err == nil || !marker {
		t.Fatalf("mismatched marker accepted: marker=%v err=%v", marker, err)
	}
}

func TestResolveLegacyCanonicalAndSplitBrain(t *testing.T) {
	dir := t.TempDir()
	profile := "app"
	legacy := LegacyPath(dir, profile)
	if err := os.WriteFile(legacy, []byte(`{"profile":"app"}`), 0600); err != nil {
		t.Fatal(err)
	}
	resolution, err := Resolve(dir, profile, "values")
	if err != nil || resolution.Mode != ModeLegacy {
		t.Fatalf("legacy resolution=%#v err=%v", resolution, err)
	}

	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".identity"), 0700); err != nil {
		t.Fatal(err)
	}
	identity, _ := IdentityBytes(profile, "values")
	if err := os.WriteFile(IdentityPath(dir, profile), identity, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CanonicalPath(dir, profile), []byte(`{"profile":"app"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(dir, profile, "values"); err == nil {
		t.Fatal("canonical pair without addressable downgrade marker was accepted")
	}
	marker, _ := MarkerBytes(profile)
	if err := os.WriteFile(legacy, marker, 0600); err != nil {
		t.Fatal(err)
	}
	resolution, err = Resolve(dir, profile, "values")
	if err != nil || resolution.Mode != ModeCanonical || !resolution.Marker {
		t.Fatalf("canonical resolution=%#v err=%v", resolution, err)
	}
	if err := os.WriteFile(legacy, []byte(`{"profile":"app"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(dir, profile, "values"); err == nil {
		t.Fatal("canonical plus actual legacy state was accepted")
	}
}
