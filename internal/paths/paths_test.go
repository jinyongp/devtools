package paths

import "testing"

func TestResolve(t *testing.T) {
	env := map[string]string{"XDG_CONFIG_HOME": "/config", "XDG_DATA_HOME": "relative"}
	lookup := func(key string) string { return env[key] }
	linux, err := Resolve("linux", "/home/me", lookup)
	if err != nil || linux.Config != "/config/devtools" || linux.Data != "/home/me/.local/share/devtools" || linux.Cache != "/home/me/.cache/devtools" {
		t.Fatalf("linux/WSL: %+v %v", linux, err)
	}
	mac, err := Resolve("darwin", "/Users/me", lookup)
	if err != nil || mac.Config != "/Users/me/Library/Application Support/devtools" || mac.Data != mac.Config+"/data" || mac.Cache != "/Users/me/Library/Caches/devtools" {
		t.Fatalf("mac: %+v %v", mac, err)
	}
	if _, err := Resolve("windows", "/home/me", lookup); err == nil {
		t.Fatal("accepted unsupported OS")
	}
	if _, err := Resolve("linux", "relative", lookup); err == nil {
		t.Fatal("accepted relative home")
	}
}
