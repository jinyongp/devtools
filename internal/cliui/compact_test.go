package cliui

import (
	"strings"
	"testing"
)

func TestCompactTableOmitsOnlyDeclaredAncillaryColumns(t *testing.T) {
	id := strings.Repeat("identity", 12)
	table, err := CompactTable([]Column{{Literal("Name")}, {Literal("Optional")}, {Literal("ID")}}, []Row{{Cells: []Text{Literal("web"), Literal("ancillary information"), Literal(id)}}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Render(NewDocument(table), Options{Width: 40})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(strings.SplitN(s, "\n", 2)[0], "Optional") || strings.Contains(s, "ancillary information") || strings.Count(s, id) != 1 || strings.Count(s, "Optional columns omitted.") != 1 || strings.Count(s, "\n") != 3 {
		t.Fatal(s)
	}
}
