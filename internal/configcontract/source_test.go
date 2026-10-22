package configcontract

import (
	"strings"
	"testing"
)

func TestOwnedSourceRangesKeepLiteralUnicodeKeysAndComments(t *testing.T) {
	data := []byte("# 문서\r\nprofile='sample'\r\n[commands.\"dev.웹\"] # command\r\nexec=['node', '개발'] # argv\r\n[tools]\r\nnode={}\r\n")
	syntax, err := ParseSyntax(data)
	if err != nil {
		t.Fatal(err)
	}
	source := syntax.Source([]string{"commands", "dev.웹", "exec", "1"})
	if source.Line == nil || *source.Line != 4 || source.Offset == nil || source.EndOffset == nil || string(data[*source.Offset:*source.EndOffset]) != "'개발'" {
		t.Fatal(source)
	}
	if len(syntax.Comments) != 3 || syntax.Newline() != "\r\n" {
		t.Fatal("comment or newline information lost")
	}
	if value, issue := Decode([]byte(strings.Repeat("[", 130) + "0" + strings.Repeat("]", 130))); issue == nil || value != nil || issue.Rule != "maximum_depth_128" {
		t.Fatal("unbounded JSON nesting accepted", issue)
	}
}
