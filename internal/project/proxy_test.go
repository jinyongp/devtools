package project

import (
	"strings"
	"testing"
)

func TestProxyConfiguration(t *testing.T) {
	config := `profile="app"
[ports.web]
range=[3000,3099]
[proxies.app]
host="${instance.alias}.${profile}.${proxy}.localhost"
port="web"
`
	got, err := parse([]byte(config), "/project/devtools.toml", "/project")
	if err != nil || got.Proxies["app"].Port != "web" {
		t.Fatalf("proxy config: %+v %v", got, err)
	}

	invalid := []string{
		`host="app.localhost"`,
		`port="web"`,
		`host="app.localhost"\nport="missing"`,
		`host="${unknown}.app.localhost"\nport="web"`,
		`host="${instance.alias.app.localhost"\nport="web"`,
		`host="App.localhost"\nport="web"`,
		`host="app.example.com"\nport="web"`,
		`host="-app.localhost"\nport="web"`,
		`host="app_.localhost"\nport="web"`,
		`host="app.localhost"\nport="web"\nunknown=true`,
	}
	for _, definition := range invalid {
		t.Run(definition, func(t *testing.T) {
			input := "profile=\"app\"\n[ports.web]\nrange=[3000,3099]\n[proxies.app]\n" + strings.ReplaceAll(definition, `\n`, "\n")
			_, err := parse([]byte(input), "/project/devtools.toml", "/project")
			if err == nil || err.Code != "invalid_config" {
				t.Fatalf("accepted proxy definition %q: %v", definition, err)
			}
		})
	}
}

func TestProxyHostLengthAndLabels(t *testing.T) {
	ports := map[string]Port{"web": {}}
	for _, host := range []string{
		"app.localhost",
		"app-${proxy}.localhost",
		"${instance.alias}.app.localhost",
		strings.Repeat("a", 63) + ".localhost",
	} {
		if !(Proxy{Host: host, Port: "web"}).Valid(ports) {
			t.Fatalf("rejected host %q", host)
		}
	}
	for _, host := range []string{
		"localhost",
		".localhost",
		strings.Repeat("a", 64) + ".localhost",
		strings.Repeat("a.", 125) + "aaa.localhost",
		"app..localhost",
	} {
		if (Proxy{Host: host, Port: "web"}).Valid(ports) {
			t.Fatalf("accepted host %q", host)
		}
	}
	_, err := parse([]byte("profile=\"app\"\n[ports.web]\n[proxies.\"../bad\"]\nhost=\"app.localhost\"\nport=\"web\"\n"), "/project/devtools.toml", "/project")
	if err == nil || err.Code != "invalid_config" {
		t.Fatalf("accepted invalid proxy name: %v", err)
	}
}
