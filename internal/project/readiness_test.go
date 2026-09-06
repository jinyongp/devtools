package project

import "testing"

func TestReadinessConfiguration(t *testing.T) {
	for _, definition := range []string{
		`exec=["curl","--fail","http://127.0.0.1/health"]`,
		`exec=["./ready"]
timeout="30s"`,
	} {
		if _, err := parse([]byte("profile=\"test\"\n[commands.web]\nexec=[\"server\"]\n[commands.web.ready]\n"+definition), "test", t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	for _, definition := range []string{`exec=[]`, `exec=[""]`, `exec=["a\u0000b"]`, `exec=["true"]
timeout="0s"`, `exec=["true"]
timeout="31s"`, `exec=["true"]
timeout="bad"`, `http="/health"`} {
		if _, err := parse([]byte("profile=\"test\"\n[commands.web]\nexec=[\"server\"]\n[commands.web.ready]\n"+definition), "test", t.TempDir()); err == nil {
			t.Fatal("invalid readiness accepted", definition)
		}
	}
}
