package configcontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

type Source struct {
	Kind      string   `json:"kind"`
	Path      []string `json:"path"`
	Offset    *int     `json:"offset,omitempty"`
	EndOffset *int     `json:"end_offset,omitempty"`
	Line      *int     `json:"line,omitempty"`
	Column    *int     `json:"column,omitempty"`
}

// Node is an owned copy of parser state; unstable arena pointers never escape.
type Node struct {
	Path                             []string
	Start, End, ValueStart, ValueEnd int
	Kind                             string
	Comments                         bool
	Keys                             []Span
	KeyBase                          int
}
type Span struct{ Start, End int }
type Syntax struct {
	Bytes    []byte
	Nodes    []Node
	Comments []Node
}
type SyntaxError struct{ Source Source }

func (e *SyntaxError) Error() string { return "Invalid TOML syntax." }

func PathKey(path []string) string { b, _ := json.Marshal(path); return string(b) }
func HasPrefix(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

func ParseSyntax(data []byte) (*Syntax, error) {
	s := &Syntax{Bytes: append([]byte{}, data...)}
	var p unstable.Parser
	p.KeepComments = true
	p.Reset(data)
	current := []string{}
	var visit func(*unstable.Node, []string)
	visit = func(n *unstable.Node, path []string) {
		if n == nil {
			return
		}
		start, end := int(n.Raw.Offset), int(n.Raw.Offset+n.Raw.Length)
		if n.Kind == unstable.Comment {
			s.Comments = append(s.Comments, Node{Path: append([]string{}, path...), Start: start, End: end, Kind: "comment"})
			for child := n.Child(); child != nil; child = child.Next() {
				visit(child, path)
			}
			return
		}
		if n.Kind == unstable.KeyValue {
			keys := []string{}
			spans := []Span{}
			it := n.Key()
			for it.Next() {
				key := it.Node()
				keys = append(keys, string(key.Data))
				spans = append(spans, Span{int(key.Raw.Offset), int(key.Raw.Offset + key.Raw.Length)})
			}
			full := append(append([]string{}, path...), keys...)
			v := n.Value()
			valueStart := spans[len(spans)-1].End
			for valueStart < end && data[valueStart] != '=' {
				valueStart++
			}
			valueStart++
			for valueStart < end && (data[valueStart] == ' ' || data[valueStart] == '\t') {
				valueStart++
			}
			node := Node{Path: full, Start: start, End: end, ValueStart: valueStart, ValueEnd: end, Kind: "value", Keys: spans, KeyBase: len(path)}
			before := len(s.Comments)
			visit(v, full)
			node.Comments = len(s.Comments) > before
			s.Nodes = append(s.Nodes, node)
			return
		}
		if n.Raw.Length > 0 && n.Kind != unstable.InlineTable && n.Kind != unstable.Table && n.Kind != unstable.ArrayTable && n.Kind != unstable.Key {
			s.Nodes = append(s.Nodes, Node{Path: append([]string{}, path...), Start: start, End: end, Kind: "element"})
		}
		i := 0
		for child := n.Child(); child != nil; child = child.Next() {
			childPath := path
			if n.Kind == unstable.Array && child.Kind != unstable.Comment {
				childPath = append(append([]string{}, path...), strconv.Itoa(i))
				i++
			}
			visit(child, childPath)
		}
	}
	for p.NextExpression() {
		n := p.Expression()
		if n.Kind == unstable.Table || n.Kind == unstable.ArrayTable {
			current = []string{}
			spans := []Span{}
			it := n.Key()
			for it.Next() {
				key := it.Node()
				current = append(current, string(key.Data))
				spans = append(spans, Span{int(key.Raw.Offset), int(key.Raw.Offset + key.Raw.Length)})
			}
			start := bytes.LastIndexByte(data[:spans[0].Start], '[')
			if n.Kind == unstable.ArrayTable {
				start--
			}
			end := spans[len(spans)-1].End
			for end < len(data) && (data[end] == ' ' || data[end] == '\t') {
				end++
			}
			end++
			if n.Kind == unstable.ArrayTable {
				end++
			}
			s.Nodes = append(s.Nodes, Node{Path: append([]string{}, current...), Start: start, End: end, Kind: "table", Keys: spans})
			for child := n.Child(); child != nil; child = child.Next() {
				if child.Kind == unstable.Comment {
					visit(child, current)
				}
			}
		} else {
			visit(n, current)
		}
		for next := n.Next(); next != nil; next = next.Next() {
			visit(next, current)
		}
	}
	if err := p.Error(); err != nil {
		var parserError *unstable.ParserError
		if errors.As(err, &parserError) {
			r := p.Range(parserError.Highlight)
			return nil, &SyntaxError{Source: s.At(append([]string{}, parserError.Key...), int(r.Offset), int(r.Offset+r.Length))}
		}
		return nil, err
	}
	sort.SliceStable(s.Nodes, func(i, j int) bool { return s.Nodes[i].Start < s.Nodes[j].Start })
	return s, nil
}

func (s *Syntax) Source(path []string) Source {
	for length := len(path); length > 0; length-- {
		for _, n := range s.Nodes {
			if PathKey(n.Path) == PathKey(path[:length]) {
				return s.At(path, n.Start, n.End)
			}
		}
	}
	return Source{Kind: "config", Path: append([]string{}, path...)}
}
func (s *Syntax) At(path []string, start, end int) Source {
	if start < 0 || end < start || end > len(s.Bytes) {
		return Source{Kind: "config", Path: append([]string{}, path...)}
	}
	line := bytes.Count(s.Bytes[:start], []byte{'\n'}) + 1
	column := start - bytes.LastIndexByte(s.Bytes[:start], '\n')
	return Source{Kind: "config", Path: append([]string{}, path...), Offset: &start, EndOffset: &end, Line: &line, Column: &column}
}
func (s *Syntax) Newline() string {
	if i := bytes.IndexByte(s.Bytes, '\n'); i > 0 && s.Bytes[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}
func (s *Syntax) LineRange(start, end int) (int, int) {
	start = bytes.LastIndexByte(s.Bytes[:start], '\n') + 1
	if i := bytes.IndexByte(s.Bytes[end:], '\n'); i >= 0 {
		end += i + 1
	} else {
		end = len(s.Bytes)
	}
	return start, end
}

func Quote(s string) string {
	// TOML basic strings share JSON escaping except for C1 controls.
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 32 || r >= 127 && r <= 159 {
				b.WriteString(`\u`)
				h := strconv.FormatInt(int64(r), 16)
				b.WriteString(strings.Repeat("0", 4-len(h)))
				b.WriteString(h)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
func Key(s string) string {
	if s != "" && !strings.ContainsAny(s, ". ") {
		ok := true
		for _, r := range s {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				ok = false
			}
		}
		if ok {
			return s
		}
	}
	return Quote(s)
}
func Dotted(path []string) string {
	parts := make([]string, len(path))
	for i, s := range path {
		parts[i] = Key(s)
	}
	return strings.Join(parts, ".")
}
