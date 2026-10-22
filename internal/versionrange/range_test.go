package versionrange

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Expectations follow npm's documented range grammar, not parser internals.
func TestRanges(t *testing.T) {
	for _, c := range []struct {
		requirement, version string
		want                 bool
	}{
		{"26", "26.12.3", true}, {"26.x", "26.0.0", true}, {"26.*", "27.0.0", false},
		{"26.12", "26.12.9", true}, {"26.12.X", "26.13.0", false}, {"26.12.*", "26.12.0", true},
		{"26.12.3", "26.12.4", false}, {"=26.12.3", "26.12.3+build.5", true},
		{"^26.2.0", "26.12.0", true}, {"^26.2.0", "27.0.0", false},
		{"~26.12.3", "26.12.8", true}, {"~26.12.3", "26.13.0", false},
		{"~>26.12", "26.12.4", true}, {"~26", "26.99.0", true},
		{">=26 <27", "26.0.0", true}, {">=26 <27", "27.0.0", false},
		{"26 || 28", "28.1.0", true}, {"26 || 28", "27.1.0", false},
		{"26.2 - 26.8", "26.8.99", true}, {"26.2 - 26.8", "26.9.0", false},
		{"26.2.3 - 26.8.9", "26.8.9", true}, {"26.2.3 - 26.8.9", "26.8.10", false},
		{"<=26.2", "26.2.99", true}, {">26.2", "26.2.99", false}, {">26.2", "26.3.0", true},
		{"<26.2", "26.2.0-beta.1", false}, {"<*", "0.0.0", false}, {">x", "99.0.0", false},
		{"^0.2.3", "0.2.9", true}, {"^0.2.3", "0.3.0", false},
		{"^0.0.3", "0.0.4", false}, {"^0.0", "0.0.99", true}, {"^0", "0.9.0", true},
		{"*", "26.0.0-rc.1", false}, {"26", "26.1.0-rc.1", false},
		{">=26.1.0-beta.1 <27", "26.1.0-beta.2", true},
		{">=26.1.0-beta.1 <27", "26.2.0-beta.2", false},
		{">=26.1.0-beta.1 <27", "26.1.0", true},
		{"26.1.0-beta.1", "26.1.0-beta.1+build", true},
		{">=26.1.0-alpha.9 <26.1.0", "26.1.0-alpha.10", true},
		{" >= 26.0.0  < 27.0.0 ", "26.3.0", true},
		{"v26", "v26.1.0", true}, {"1.20", "1.20", true}, {"1.20.0", "1.20", true},
		{"*", "01.2.3", false}, {"*", "1.2.3.4", false}, {"*", "1.2.3_custom", false}, {"*", "1.2.x", false},
		{"26 ||", "28.0.0", true},
		{"1.2.x-beta", "1.2.9", true}, {"1.2.x-beta", "1.2.9-beta", false},
		{"1.2+build", "1.2.9", true}, {"1+build", "1.9.0", true},
		{"*.*.*-alpha", "1.9.0", true}, {"1.2.3 +build", "1.2.3", true},
	} {
		r, err := Parse(c.requirement)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.requirement, err)
			continue
		}
		if got := r.Match(c.version); got != c.want {
			t.Errorf("%q matches %q: %v, want %v", c.requirement, c.version, got, c.want)
		}
	}
}

func TestInvalidRanges(t *testing.T) {
	for _, s := range []string{"", " ", "latest", "lts", "26,28", "26 | 28", "!=26", "=>26", "26.12.3\n", "\t26", "26.12.3_foo", "26.12.3-01", "26.2-beta", "1.x.2", "01.2.3", "9007199254740992.0.0", "9007199254740991", "^9007199254740991.0.0", "^", "26.0.0 +", strings.Repeat("1", Limit+1)} {
		if Valid(s) {
			t.Errorf("accepted %q", s)
		}
	}
}

func TestCompleteIntersection(t *testing.T) {
	for _, c := range []struct {
		ranges []string
		want   bool
	}{
		{nil, true}, {[]string{"26", ">=26.2.0 <27"}, true}, {[]string{"26", "28"}, false},
		{[]string{"26 || 28", "26 || 30", "28 || 30"}, false},
		{[]string{"26 || 28", "26 || 30"}, true},
		{[]string{">1.2.3 <1.2.4"}, false},
		{[]string{">1.2.3-alpha <1.2.3-alpha.0"}, false},
		{[]string{">1.2.3-alpha <=1.2.3-alpha.0"}, true},
		{[]string{"1.2.3-beta", "*"}, false},
		{[]string{">=1.2.3-alpha <1.2.3", ">=1.2.3-beta <1.2.3"}, true},
		{[]string{">=1.2.3-alpha <1.2.3", ">=1.2.2-alpha <1.2.3"}, false},
		{[]string{"26.12.3+foo", "26.12.3+bar"}, true},
		{[]string{"<0.0.0"}, false}, {[]string{"0.0.0"}, true},
		{[]string{"*", ">9007199254740991.9007199254740991.9007199254740991"}, false},
	} {
		if got := Intersects(c.ranges...); got != c.want {
			t.Errorf("Intersects(%q): %v, want %v", c.ranges, got, c.want)
		}
	}
	// Many equivalent OR branches must not trigger exponential combinations.
	ranges := make([]string, 60)
	for i := range ranges {
		ranges[i] = "26 || 28 || 26 || 28"
	}
	ranges = append(ranges, "30")
	if Intersects(ranges...) {
		t.Fatal("unexpected intersection")
	}
}

// Optional differential check uses npm's own installed implementation as an
// independent oracle. Product and ordinary Go tests do not need Node or npm.
func TestNPMReference(t *testing.T) {
	reference := os.Getenv("DEVTOOLS_SEMVER_REFERENCE")
	if reference == "" {
		t.Skip("set DEVTOOLS_SEMVER_REFERENCE to npm's semver module for differential verification")
	}
	ranges := []string{"*", "x", "26", "26.12", "26.*", "26.12.X", "26.12.3", "=26.12.3", "^26.2.0", "~26.12.3", "~>26.12", "26 || 28", ">=26 <27", "26.2 - 28.1", "26.2.3 - 28.1.4", "^0", "^0.0", "^0.0.3", "^0.2", "~0.2", "^26.0.0-beta.1", ">=26.0.0-beta.1 <27", ">26", "<26.2", "<=26.2", ">*", "<*", ">=x", "<=X", "26 ||", ">= 26 < 28", "v26", ">1.2.3-alpha <1.2.3-alpha.0"}
	for _, p := range []string{"0", "0.0", "0.0.3", "0.2", "0.2.3", "1", "1.x", "1.2", "1.2.*", "1.2.3", "*", "*.x.x"} {
		for _, op := range []string{"", ">", ">=", "<", "<=", "=", "^", "~"} {
			for _, suffix := range []string{"", "-beta.1", "+build"} {
				ranges = append(ranges, op+p+suffix)
			}
		}
	}
	ranges = append(ranges, "9007199254740991", "^9007199254740991.0.0", "1.2.3 +meta")
	versions := []string{"0.0.0", "0.0.3", "0.0.4", "0.2.0", "0.2.3", "0.3.0", "1.2.3-alpha", "1.2.3-alpha.0", "1.2.3-alpha.0.0"}
	for _, major := range []string{"25", "26", "27", "28", "29"} {
		for _, minor := range []string{"0", "1", "2", "11", "12", "13"} {
			for _, patch := range []string{"0", "1", "3", "9"} {
				for _, suffix := range []string{"", "-beta.1", "-beta.2", "+build.1"} {
					versions = append(versions, major+"."+minor+"."+patch+suffix)
				}
			}
		}
	}
	input, _ := json.Marshal(map[string]any{"ranges": ranges, "versions": versions})
	cmd := exec.Command("node", "-e", `const fs=require('fs'),s=require(process.argv[1]),i=JSON.parse(fs.readFileSync(0,'utf8')); process.stdout.write(JSON.stringify(i.ranges.map(r=>({valid:s.validRange(r)!==null,matches:i.versions.map(v=>s.satisfies(v,r))}))))`, reference)
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("npm reference: %v: %s", err, out)
	}
	var expected []struct {
		Valid   bool
		Matches []bool
	}
	if err := json.Unmarshal(out, &expected); err != nil {
		t.Fatal(err)
	}
	for i, r := range ranges {
		parsed, err := Parse(r)
		if (err == nil) != expected[i].Valid {
			t.Errorf("npm parse mismatch: %q: %v", r, err)
		}
		for j, v := range versions {
			if got := parsed.Match(v); got != expected[i].Matches[j] {
				t.Errorf("npm mismatch: %q, %q: got %v want %v", r, v, got, expected[i].Matches[j])
			}
		}
	}
	t.Logf("npm conformance: %d range parses, %d range/version comparisons", len(ranges), len(ranges)*len(versions))
}
