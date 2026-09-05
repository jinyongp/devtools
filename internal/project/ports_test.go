package project

import "testing"

func TestPortConfiguration(t *testing.T) {
	good := `profile="test"
[ports.web]
port=3000
strict=false
range=[3000,3099]
[commands.dev]
exec=["server","${bind.PORT}"]
serve=["web"]
[commands.dev.bind]
PORT={port="web"}
URL={template="http://${var.HOST}:${bind.PORT}"}
`
	if _, e := parse([]byte(good), "devtools.toml", "/tmp"); e != nil {
		t.Fatal(e)
	}
	for _, def := range []string{"port=0", "port=65536", "range=[]", "range=[3001,3000]", "strict=true", "range=[1,2,3]"} {
		if _, e := parse([]byte("profile=\"test\"\n[ports.web]\n"+def), "devtools.toml", "/tmp"); e == nil {
			t.Fatal(def)
		}
	}
}
func TestTemplateSinglePass(t *testing.T) {
	out, e := Expand("$${var.X}/${bind.PORT}/${var.HOST}", func(k string) (string, bool) {
		v, ok := map[string]string{"bind.PORT": "3000", "var.HOST": "${literal}"}[k]
		return v, ok
	})
	if e != nil || out != "${var.X}/3000/${literal}" {
		t.Fatal(out, e)
	}
	for _, in := range []string{"${missing}", "${bind.PORT"} {
		if _, e := Expand(in, func(string) (string, bool) { return "", false }); e == nil {
			t.Fatal(in)
		}
	}
}
