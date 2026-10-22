// Package versionrange evaluates npm-style SemVer requirements without a runtime
// dependency on Node. A range is a union of intervals, with a per-interval set of
// version tuples explicitly admitting prereleases (npm's default policy).
package versionrange

import (
	"cmp"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

const Limit = 128
const maxNumber = 9007199254740991 // npm's Number.MAX_SAFE_INTEGER.

var invalid = errors.New("invalid npm SemVer range")
var partialPattern = regexp.MustCompile(`^v?([0-9]+|[xX*])(?:\.([0-9]+|[xX*]))?(?:\.([0-9]+|[xX*]))?(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
var operatorSpace = regexp.MustCompile(`([<>]=?|=|\^|~>?) +`)
var observedPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type version struct {
	n   [3]uint64
	pre string
}
type partial struct {
	v     version
	count int
}
type bound struct {
	v         version
	inclusive bool
}
type interval struct {
	low, high *bound
	pre       map[[3]uint64]bool
}

// Range is immutable after parsing. Metadata is ignored for version precedence.
type Range struct{ sets []interval }

func number(s string) (uint64, bool) {
	if len(s) > 1 && s[0] == '0' {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil && n <= maxNumber
}

func parsePartial(s string) (partial, error) {
	// npm permits build metadata on partials, including an otherwise empty
	// partial. Metadata never narrows the resulting version family.
	if strings.HasPrefix(s, "+") {
		s = "*" + s
	}
	m := partialPattern.FindStringSubmatch(s)
	if m == nil {
		return partial{}, invalid
	}
	p := partial{}
	wild := false
	for i, s := range m[1:4] {
		if s == "" || s == "x" || s == "X" || s == "*" {
			wild = true
			continue
		}
		if wild {
			return partial{}, invalid
		}
		n, ok := number(s)
		if !ok {
			return partial{}, invalid
		}
		p.v.n[i] = n
		p.count++
	}
	if m[4] != "" && m[3] == "" {
		return partial{}, invalid
	}
	for _, id := range strings.Split(m[4], ".") {
		if len(id) > 1 && id[0] == '0' && numeric(id) {
			return partial{}, invalid
		}
	}
	if p.count == 3 {
		p.v.pre = m[4]
	}
	return p, nil
}

func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func compare(a, b version) int {
	for i := range a.n {
		if c := cmp.Compare(a.n[i], b.n[i]); c != 0 {
			return c
		}
	}
	if a.pre == b.pre {
		return 0
	}
	if a.pre == "" {
		return 1
	}
	if b.pre == "" {
		return -1
	}
	aa, bb := strings.Split(a.pre, "."), strings.Split(b.pre, ".")
	for i := 0; i < len(aa) && i < len(bb); i++ {
		x, y := aa[i], bb[i]
		if x == y {
			continue
		}
		xn, yn := numeric(x), numeric(y)
		if xn && yn {
			if c := cmp.Compare(len(x), len(y)); c != 0 {
				return c
			}
		}
		if xn != yn {
			if xn {
				return -1
			}
			return 1
		}
		return strings.Compare(x, y)
	}
	return cmp.Compare(len(aa), len(bb))
}

func (s *interval) constrain(op string, v version) {
	if v.pre != "" {
		s.pre[v.n] = true
	}
	b := &bound{v: v, inclusive: op == ">=" || op == "<=" || op == "="}
	if op == ">" || op == ">=" || op == "=" {
		if s.low == nil || compare(v, s.low.v) > 0 || compare(v, s.low.v) == 0 && !b.inclusive {
			s.low = b
		}
	}
	if op == "<" || op == "<=" || op == "=" {
		if s.high == nil || compare(v, s.high.v) < 0 || compare(v, s.high.v) == 0 && !b.inclusive {
			s.high = b
		}
	}
}

func next(v version, index int) version {
	v.n[index]++
	for i := index + 1; i < 3; i++ {
		v.n[i] = 0
	}
	v.pre = "0"
	return v
}

func (s *interval) term(token string) error {
	op := ""
	for len(token) > 0 && strings.ContainsRune("<>=~^", rune(token[0])) {
		op += token[:1]
		token = token[1:]
	}
	switch op {
	case "", "=", ">", ">=", "<", "<=", "~", "~>", "^":
	default:
		return invalid
	}
	p, err := parsePartial(token)
	if err != nil {
		return err
	}
	v := p.v
	if p.count == 0 {
		if op == "<" || op == ">" {
			s.constrain("<", version{pre: "0"})
		}
		return nil
	}
	if op == "~" || op == "~>" || op == "^" {
		s.constrain(">=", v)
		i := 0
		if op != "^" {
			if p.count >= 2 {
				i = 1
			}
		} else {
			for i < p.count-1 && v.n[i] == 0 {
				i++
			}
		}
		s.constrain("<", next(v, i))
		return nil
	}
	if p.count == 3 {
		if op == "" {
			op = "="
		}
		s.constrain(op, v)
		return nil
	}
	switch op {
	case "", "=":
		s.constrain(">=", v)
		s.constrain("<", next(v, p.count-1))
	case ">":
		v = next(v, p.count-1)
		v.pre = ""
		s.constrain(">=", v)
	case "<=":
		s.constrain("<", next(v, p.count-1))
	case "<":
		v.pre = "0"
		s.constrain("<", v)
	case ">=":
		s.constrain(">=", v)
	}
	return nil
}

// Parse accepts bounded, nonempty configuration requirements. Use "*" for any
// stable version; omission of a tool's version means presence-only inspection.
func Parse(raw string) (Range, error) {
	if len(raw) > Limit || strings.TrimSpace(raw) == "" {
		return Range{}, invalid
	}
	for _, c := range raw {
		if c < ' ' || c > '~' {
			return Range{}, invalid
		}
	}
	r := Range{}
	for _, part := range strings.Split(raw, "||") {
		s := interval{pre: map[[3]uint64]bool{}}
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 3 && fields[1] == "-" {
			a, e := parsePartial(fields[0])
			if e != nil {
				return Range{}, e
			}
			b, e := parsePartial(fields[2])
			if e != nil {
				return Range{}, e
			}
			if a.count > 0 {
				s.constrain(">=", a.v)
			}
			if b.count == 3 {
				s.constrain("<=", b.v)
			} else if b.count > 0 {
				s.constrain("<", next(b.v, b.count-1))
			}
		} else {
			part = operatorSpace.ReplaceAllString(part, "$1")
			for _, token := range strings.Fields(part) {
				if e := s.term(token); e != nil {
					return Range{}, e
				}
			}
		}
		for _, b := range []*bound{s.low, s.high} {
			if b != nil {
				for _, n := range b.v.n {
					if n > maxNumber {
						return Range{}, invalid
					}
				}
			}
		}
		r.sets = append(r.sets, s)
	}
	return r, nil
}

func Valid(raw string) bool { _, err := Parse(raw); return err == nil }

// Observed accepts strict numeric major.minor[.patch] output (optional v).
// Two-component output is normalized by adding patch zero. Arbitrary coercion,
// extra numeric components, leading zeroes and non-SemVer suffixes are rejected.
func Observed(raw string) bool { _, ok := observed(raw); return ok }
func observed(raw string) (version, bool) {
	if len(raw) > Limit || !observedPattern.MatchString(raw) {
		return version{}, false
	}
	p, err := parsePartial(raw)
	return p.v, err == nil && p.count >= 2
}

func (s interval) contains(v version) bool {
	if s.low != nil {
		c := compare(v, s.low.v)
		if c < 0 || c == 0 && !s.low.inclusive {
			return false
		}
	}
	if s.high != nil {
		c := compare(v, s.high.v)
		if c > 0 || c == 0 && !s.high.inclusive {
			return false
		}
	}
	return true
}

func (r Range) Match(raw string) bool {
	v, ok := observed(raw)
	if !ok {
		return false
	}
	return r.match(v)
}

func (r Range) match(v version) bool {
	for _, s := range r.sets {
		if s.contains(v) && (v.pre == "" || s.pre[v.n]) {
			return true
		}
	}
	return false
}

func Matches(raw, observed string) bool { r, e := Parse(raw); return e == nil && r.Match(observed) }

// candidates returns the least possible stable release and prerelease for each
// admitted tuple. An exclusive prerelease's immediate successor appends .0.
func (s interval) candidates() []version {
	out := []version{}
	v := version{}
	if s.low != nil {
		v = s.low.v
		v.pre = ""
		if s.low.v.pre == "" && !s.low.inclusive {
			for i := 2; i >= 0; i-- {
				if v.n[i] < maxNumber {
					v.n[i]++
					break
				}
				v.n[i] = 0
				if i == 0 {
					return out
				}
			}
		}
	}
	out = append(out, v)
	for n := range s.pre {
		v := version{n: n, pre: "0"}
		if s.low != nil && compare(v, s.low.v) < 0 {
			if s.low.v.n != n || s.low.v.pre == "" {
				continue
			}
			v = s.low.v
		}
		if s.low != nil && compare(v, s.low.v) == 0 && !s.low.inclusive {
			v.pre += ".0"
		}
		out = append(out, v)
	}
	return out
}

// Intersects tests the complete conjunction, including OR branches and each
// range's own prerelease admission. Pairwise overlap is insufficient.
func Intersects(requirements ...string) bool {
	ranges := make([]Range, len(requirements))
	for i, raw := range requirements {
		r, err := Parse(raw)
		if err != nil {
			return false
		}
		ranges[i] = r
	}
	if len(ranges) == 0 {
		return true
	}
	// Any nonempty intersection has a least member. That member is a minimum
	// candidate of one participating interval. Enumerating witnesses avoids an
	// exponential Cartesian product of OR branches across many tool aliases.
	seen := map[version]bool{}
	for _, r := range ranges {
		for _, branch := range r.sets {
			for _, v := range branch.candidates() {
				if seen[v] {
					continue
				}
				seen[v] = true
				all := true
				for _, other := range ranges {
					if !other.match(v) {
						all = false
						break
					}
				}
				if all {
					return true
				}
			}
		}
	}
	return false
}
