package project

import "testing"

func TestTypedBindingFieldsAreExclusive(t *testing.T) {
	for _, binding := range []string{`{port='api',proxy='api'}`, `{proxy='api',format='url'}`, `{port='api',format='https'}`, `{proxy='api',template='x'}`, `{port='api',template='x'}`} {
		config := "profile='app'\n[ports.api]\n[commands.web]\nexec=['true']\n[commands.web.bind]\nURL=" + binding + "\n"
		if _, e := parse([]byte(config), "", t.TempDir()); e == nil {
			t.Fatal("accepted", binding)
		}
	}
}
