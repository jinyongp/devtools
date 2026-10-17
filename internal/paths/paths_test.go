package paths

import (
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, "config")
	env := map[string]string{"XDG_CONFIG_HOME": config, "XDG_DATA_HOME": "relative"}
	lookup := func(key string) string { return env[key] }
	linux, err := Resolve("linux", home, lookup)
	if err != nil || linux.Config != filepath.Join(config, "devtools") || linux.Data != filepath.Join(home, ".local", "share", "devtools") || linux.Cache != filepath.Join(home, ".cache", "devtools") {
		t.Fatalf("linux/WSL: %+v %v", linux, err)
	}
	mac, err := Resolve("darwin", home, lookup)
	if err != nil || mac.Config != filepath.Join(home, "Library", "Application Support", "devtools") || mac.Data != filepath.Join(mac.Config, "data") || mac.Cache != filepath.Join(home, "Library", "Caches", "devtools") {
		t.Fatalf("mac: %+v %v", mac, err)
	}
	if _, err := Resolve("freebsd", home, lookup); err == nil {
		t.Fatal("accepted unsupported OS")
	}
	if _, err := Resolve("linux", "relative", lookup); err == nil {
		t.Fatal("accepted relative home")
	}
}

func TestResolveWindowsLocalLayout(t *testing.T) {
	local := t.TempDir()
	got, err := Resolve("windows", local, func(key string) string {
		if key == "LOCALAPPDATA" {
			return local
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Config != filepath.Join(local, "devtools", "config") || got.Data != filepath.Join(local, "devtools", "data") || got.Cache != filepath.Join(local, "devtools", "cache") || got.Runtime != filepath.Join(local, "devtools", "runtime") {
		t.Fatalf("Windows user layout: %+v", got)
	}
	if _, err = Resolve("windows", local, func(string) string { return "relative" }); err == nil {
		t.Fatal("relative LocalAppData accepted")
	}
}
