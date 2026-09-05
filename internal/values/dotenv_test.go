package values

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	input := "\ufeff# header\r\nexport A = plain # comment\r\nEMPTY=\nHASH=a#b\nSINGLE=' $A \\n '\nDOUBLE=\"line\\nnext\\t\\\"\\\\\\$A\" # comment\nMULTI=\"first\nsecond\"\nLITERAL=$(touch /tmp/never); $A ${A}\n"
	got, err := ParseDotenv(input)
	want := map[string]string{"A": "plain", "EMPTY": "", "HASH": "a#b", "SINGLE": " $A \\n ", "DOUBLE": "line\nnext\t\"\\$A", "MULTI": "first\nsecond", "LITERAL": "$(touch /tmp/never); $A ${A}"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, error %v", got, err)
	}
}

func TestDotenvErrorsDoNotExposeInput(t *testing.T) {
	for _, input := range []string{"BAD-KEY=CANARY", "A=CANARY\nA=CANARY", "A='CANARY", "A=\"CANARY\" trailing", "A=CANARY\x00", "A=\xffCANARY", "CANARY"} {
		_, err := ParseDotenv(input)
		if err == nil || err.Code != "invalid_dotenv" || err.Details["line"] == nil {
			t.Fatalf("expected safe parse error, got %v", err)
		}
		if strings.Contains(err.Message, "CANARY") {
			t.Fatal("input exposed")
		}
	}
}

func TestDotenvPreservesQuotedWhitespace(t *testing.T) {
	got, err := ParseDotenv("export MULTI=\"first  \n  second  \n\"\nSINGLE=' first  \n second '\nUNKNOWN=\"\\q\"\n")
	want := map[string]string{"MULTI": "first  \n  second  \n", "SINGLE": " first  \n second ", "UNKNOWN": "\\q"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, error %v", got, err)
	}
}
