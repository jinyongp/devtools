package project

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestRangeInputParity(t *testing.T) {
	for _, r := range []string{"26", "26.12", "^26.2.0", "~26.12.3", ">=26 <27", "26 || 28", "26.2 - 28.1", "*", "", " ", "bad", "26 | 28", "1.x.2", "1.2.3\n"} {
		want := r != "" && r != " " && r != "bad" && r != "26 | 28" && r != "1.x.2" && r != "1.2.3\n"
		for _, decl := range []string{
			"[tools.node]\nversion=" + strconv.Quote(r),
			"[requirements.tools.node]\nversion=" + strconv.Quote(r),
			"[commands.test]\nexec=['node']\n[commands.test.requirements.tools.node]\nversion=" + strconv.Quote(r),
			"[tools]\nnode={}\n[commands.test]\nexec=['node']\ntools=['node']\n[commands.test.tool_overrides.node]\nversion=" + strconv.Quote(r),
		} {
			_, err := ParseDocument([]byte("profile='test'\n"+decl), "devtools.toml", ".")
			if (err == nil) != want {
				t.Errorf("range %q, %s: %v", r, decl, err)
			}
		}
	}
}

func TestStaticRangeConflict(t *testing.T) {
	for _, c := range []struct {
		ranges []string
		valid  bool
	}{
		{[]string{"26", ">=26.2 <27"}, true},
		{[]string{"26", "28"}, false},
		{[]string{"26 || 28", "26 || 30", "28 || 30"}, false},
	} {
		var config strings.Builder
		config.WriteString("profile='sample'\n[tools]\n")
		names := []string{}
		for i, r := range c.ranges {
			name := fmt.Sprintf("tool%d", i)
			names = append(names, strconv.Quote(name))
			fmt.Fprintf(&config, "%s={executable='node',version=%q}\n", name, r)
		}
		fmt.Fprintf(&config, "[commands.check]\nexec=['node']\ntools=[%s]\n", strings.Join(names, ","))
		_, err := ParseDocument([]byte(config.String()), "devtools.toml", ".")
		if (err == nil) != c.valid {
			t.Errorf("%q: %v", c.ranges, err)
		}
	}
}
