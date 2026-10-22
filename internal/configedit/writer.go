package configedit

import (
	"bytes"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type replacement struct {
	start, end int
	text       string
}

func apply(data []byte, edits []replacement) ([]byte, *protocol.Error) {
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start == edits[j].start {
			return edits[i].end < edits[j].end
		}
		return edits[i].start < edits[j].start
	})
	var b bytes.Buffer
	offset := 0
	for _, r := range edits {
		if r.start < offset {
			return nil, failure("config_preservation_failed", "overlapping_source", nil)
		}
		b.Write(data[offset:r.start])
		b.WriteString(r.text)
		offset = r.end
	}
	b.Write(data[offset:])
	return b.Bytes(), nil
}

func write(doc *project.Document, desired map[string]any) ([]byte, *protocol.Error) {
	if reflect.DeepEqual(doc.Raw, desired) {
		return append([]byte{}, doc.Bytes...), nil
	}
	// Rename keys in place, including nested headers, to retain argv and comments.
	before, _ := doc.Raw["commands"].(map[string]any)
	after, _ := desired["commands"].(map[string]any)
	for _, old := range keys(before) {
		if _, ok := after[old]; ok {
			continue
		}
		for _, name := range keys(after) {
			if _, ok := before[name]; ok || !reflect.DeepEqual(before[old], after[name]) {
				continue
			}
			edits := []replacement{}
			seen := map[int]bool{}
			for _, n := range doc.Syntax.Nodes {
				if len(n.Path) < 2 || n.Path[0] != "commands" || n.Path[1] != old || n.KeyBase > 1 {
					continue
				}
				idx := 1 - n.KeyBase
				if idx < len(n.Keys) {
					span := n.Keys[idx]
					if !seen[span.Start] {
						edits = append(edits, replacement{span.Start, span.End, configcontract.Key(name)})
						seen[span.Start] = true
					}
				}
			}
			if len(edits) > 0 {
				data, e := apply(doc.Bytes, edits)
				if e != nil {
					return nil, e
				}
				next, e := project.ParseDocument(data, doc.Path, doc.Context.Root)
				if e != nil {
					return nil, e
				}
				return write(next, desired)
			}
		}
	}
	edits := []replacement{}
	inserts := map[int][]string{}
	nl := doc.Syntax.Newline()
	lookup := func(path []string) *configcontract.Node {
		for _, n := range doc.Syntax.Nodes {
			if n.Kind == "value" && configcontract.PathKey(n.Path) == configcontract.PathKey(path) {
				copy := n
				return &copy
			}
		}
		return nil
	}
	var diff func([]string, any, any, bool, bool) *protocol.Error
	diff = func(path []string, old, next any, oldExists, nextExists bool) *protocol.Error {
		if oldExists == nextExists && reflect.DeepEqual(old, next) {
			return nil
		}
		node := lookup(path)
		a, aMap := old.(map[string]any)
		b, bMap := next.(map[string]any)
		if node != nil && aMap && bMap && reflect.DeepEqual(keys(a), keys(b)) {
			for _, key := range keys(a) {
				if e := diff(append(append([]string{}, path...), key), a[key], b[key], true, true); e != nil {
					return e
				}
			}
			return nil
		}
		if node != nil {
			// A table written inline is one value. Replacing it is safe only when
			// no internal comments would lose their ownership.
			if node.Comments && nextExists {
				return failure("config_preservation_failed", "internal_comments", path)
			}
			if !nextExists {
				start, end := doc.Syntax.LineRange(node.Start, node.End)
				edits = append(edits, replacement{start, end, ""})
			} else {
				edits = append(edits, replacement{node.ValueStart, node.ValueEnd, encodeLike(doc, path, next)})
			}
			return nil
		}
		if aMap && bMap {
			all := map[string]any{}
			for k := range a {
				all[k] = nil
			}
			for k := range b {
				all[k] = nil
			}
			for _, k := range ordered(keys(all), path) {
				av, ao := a[k]
				bv, bo := b[k]
				if e := diff(append(append([]string{}, path...), k), av, bv, ao, bo); e != nil {
					return e
				}
			}
			return nil
		}
		if !nextExists {
			for _, n := range doc.Syntax.Nodes {
				if configcontract.HasPrefix(n.Path, path) {
					start, end := doc.Syntax.LineRange(n.Start, n.End)
					edits = append(edits, replacement{start, end, ""})
				}
			}
			return nil
		}
		// Insert under the nearest existing table. Otherwise open a new table at
		// the end; no existing token or standalone comment is rewritten.
		parent := path[:len(path)-1]
		position := -1
		if len(parent) == 0 {
			position = 0
			for _, n := range doc.Syntax.Nodes {
				if n.Kind == "table" {
					break
				}
				_, position = doc.Syntax.LineRange(n.Start, n.End)
			}
		} else {
			for i, n := range doc.Syntax.Nodes {
				if n.Kind == "table" && configcontract.PathKey(n.Path) == configcontract.PathKey(parent) {
					_, position = doc.Syntax.LineRange(n.Start, n.End)
					for _, following := range doc.Syntax.Nodes[i+1:] {
						if following.Kind == "table" {
							break
						}
						_, position = doc.Syntax.LineRange(following.Start, following.End)
					}
				}
			}
		}
		if position >= 0 {
			if bMap && (len(path) == 1 || len(path) == 2 && (path[0] == "commands" || path[0] == "presets" || path[0] == "tools" && utf8.RuneCountInString(configcontract.Key(path[1])+" = "+project.EncodeValue(next)) > 100)) {
				position = -1
			}
		}
		if position >= 0 {
			inserts[position] = append(inserts[position], configcontract.Key(path[len(path)-1])+" = "+encode(next, nl)+nl)
			return nil
		}
		var text strings.Builder
		if bMap {
			renderTable(&text, path, b, nl)
		} else {
			if len(parent) > 0 {
				text.WriteString("[" + configcontract.Dotted(parent) + "]" + nl)
			}
			text.WriteString(configcontract.Key(path[len(path)-1]) + " = " + encode(next, nl) + nl)
		}
		inserts[len(doc.Bytes)] = append(inserts[len(doc.Bytes)], nl+text.String())
		return nil
	}
	if e := diff(nil, doc.Raw, desired, true, true); e != nil {
		return nil, e
	}
	// Explicit deletion of a parent removes all its declarations. Merge only
	// overlapping deletion lines, never spans that contain standalone comments.
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	merged := []replacement{}
	for _, r := range edits {
		if len(merged) > 0 && r.text == "" && merged[len(merged)-1].text == "" && r.start <= merged[len(merged)-1].end {
			if r.end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = r.end
			}
			continue
		}
		merged = append(merged, r)
	}
	for offset, lines := range inserts {
		text := strings.Join(lines, "")
		if offset > 0 && doc.Bytes[offset-1] != '\n' {
			text = nl + text
		}
		merged = append(merged, replacement{offset, offset, text})
	}
	return apply(doc.Bytes, merged)
}

func ordered(fields []string, path []string) []string {
	order := []string{"profile", "bundle", "env_files", "exec", "inject", "env", "output", "stdout", "stderr", "values_update", "ready", "serve", "bind", "requires", "tools", "vars", "secs", "tool_overrides", "executable", "version", "version_args"}
	rank := func(s string) int {
		for i, v := range order {
			if v == s {
				return i
			}
		}
		return len(order)
	}
	if len(path) == 2 && path[0] == "commands" || len(path) == 0 || len(path) == 2 && path[0] == "tools" {
		sort.SliceStable(fields, func(i, j int) bool {
			if rank(fields[i]) == rank(fields[j]) {
				return fields[i] < fields[j]
			}
			return rank(fields[i]) < rank(fields[j])
		})
	}
	return fields
}
func encode(v any, nl string) string {
	text := project.EncodeValue(v)
	if a, ok := v.([]any); ok && utf8.RuneCountInString(text) > 100 {
		var b strings.Builder
		b.WriteString("[" + nl)
		for _, item := range a {
			b.WriteString("  " + project.EncodeValue(item) + "," + nl)
		}
		b.WriteString("]")
		return b.String()
	}
	return text
}
func renderTable(b *strings.Builder, path []string, m map[string]any, nl string) {
	if len(path) == 1 && path[0] == "tools" && len(m) > 0 {
		inline := true
		for i, name := range keys(m) {
			text := configcontract.Key(name) + " = " + encode(m[name], nl)
			if inline && utf8.RuneCountInString(text) <= 100 {
				if i == 0 {
					b.WriteString("[tools]" + nl)
				}
				b.WriteString(text + nl)
			} else {
				// After a child table, reopening [tools] would be invalid TOML.
				// Keep subsequent names in order using child tables as well.
				inline = false
				renderTable(b, []string{"tools", name}, m[name].(map[string]any), nl)
			}
		}
		return
	}
	if len(path) == 1 && (path[0] == "commands" || path[0] == "presets") && len(m) > 0 {
		for _, name := range keys(m) {
			renderTable(b, append(append([]string{}, path...), name), m[name].(map[string]any), nl)
			b.WriteString(nl)
		}
		return
	}
	b.WriteString("[" + configcontract.Dotted(path) + "]" + nl)
	for _, k := range ordered(keys(m), path) {
		b.WriteString(configcontract.Key(k) + " = " + encode(m[k], nl) + nl)
	}
}

func format(doc *project.Document) ([]byte, *protocol.Error) {
	desired := clone(doc.Raw)
	roots := [][]string{{"tools"}, {"requirements", "tools"}}
	for _, name := range keys(object(desired, "commands")) {
		roots = append(roots, []string{"commands", name, "requirements", "tools"}, []string{"commands", name, "tool_overrides"})
	}
	if _, exists := doc.Raw["commands"]; !exists {
		delete(desired, "commands")
	}
	for _, root := range roots {
		var value any = desired
		for _, key := range root {
			m, _ := value.(map[string]any)
			value = m[key]
		}
		tools, _ := value.(map[string]any)
		for name, value := range tools {
			tool, _ := value.(map[string]any)
			if tool["executable"] != name {
				continue
			}
			path := append(append(append([]string{}, root...), name), "executable")
			source := doc.Source(path)
			commented := false
			if source.Offset != nil && source.EndOffset != nil {
				start, end := doc.Syntax.LineRange(*source.Offset, *source.EndOffset)
				for _, c := range doc.Syntax.Comments {
					if c.Start >= start && c.Start < end {
						commented = true
					}
				}
			}
			if !commented {
				copy := clone(tool)
				delete(copy, "executable")
				tools[name] = copy
			}
		}
	}
	if !reflect.DeepEqual(desired, doc.Raw) {
		data, e := write(doc, desired)
		if e != nil {
			return nil, e
		}
		next, e := project.ParseDocument(data, doc.Path, doc.Context.Root)
		if e != nil {
			return nil, e
		}
		doc = next
	}
	edits := []replacement{}
	for _, n := range doc.Syntax.Nodes {
		if n.Kind != "value" || n.KeyBase > 0 && n.Start < n.ValueStart && bytes.Contains(doc.Bytes[n.Start:n.ValueStart], []byte{'\n'}) {
			continue
		}
		// Nested inline entries belong to the containing value; leave their tokens
		// untouched. Multiline scalar/array content also stays in place.
		contained := false
		for _, parent := range doc.Syntax.Nodes {
			if parent.Kind == "value" && parent.Start < n.Start && parent.ValueStart <= n.Start && parent.ValueEnd >= n.End {
				contained = true
				break
			}
		}
		if contained {
			continue
		}
		keyEnd := n.ValueStart
		prefix := doc.Bytes[n.Start:keyEnd]
		eq := bytes.LastIndexByte(prefix, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(string(prefix[:eq]))
		edits = append(edits, replacement{n.Start, n.ValueStart, key + " = "})
	}
	data, e := apply(doc.Bytes, edits)
	if e != nil {
		return nil, e
	}
	next, e := project.ParseDocument(data, doc.Path, doc.Context.Root)
	if e != nil {
		return nil, e
	}
	data, e = compactTools(next)
	if e != nil {
		return nil, e
	}
	return append(bytes.TrimRight(data, "\r\n"), []byte(doc.Syntax.Newline())...), nil
}

func compactTools(doc *project.Document) ([]byte, *protocol.Error) {
	edits := []replacement{}
	parentDeclared, inParent := false, false
	for i, node := range doc.Syntax.Nodes {
		if node.Kind != "table" {
			continue
		}
		if len(node.Path) == 1 && node.Path[0] == "tools" {
			parentDeclared, inParent = true, true
			continue
		}
		if len(node.Path) != 2 || node.Path[0] != "tools" {
			inParent = false
			continue
		}
		value, ok := declared(doc.Raw, node.Path)
		if !ok {
			inParent = false
			continue
		}
		text := configcontract.Key(node.Path[1]) + " = " + encodeLike(doc, node.Path, value)
		start, end := doc.Syntax.LineRange(node.Start, node.End)
		for _, child := range doc.Syntax.Nodes[i+1:] {
			if child.Kind == "table" {
				break
			}
			if child.Kind == "value" {
				_, end = doc.Syntax.LineRange(child.Start, child.End)
			}
		}
		commented := false
		for _, c := range doc.Syntax.Comments {
			if c.Start >= start && c.Start < end {
				commented = true
			}
		}
		if commented || utf8.RuneCountInString(text) > 100 || parentDeclared && !inParent {
			inParent = false
			continue
		}
		if !parentDeclared {
			text = "[tools]" + doc.Syntax.Newline() + text
			parentDeclared = true
		}
		edits = append(edits, replacement{start, end, text + doc.Syntax.Newline()})
		inParent = true
	}
	return apply(doc.Bytes, edits)
}

func encodeLike(doc *project.Document, path []string, value any) string {
	m, ok := value.(map[string]any)
	if !ok {
		return encode(value, doc.Syntax.Newline())
	}
	fields := []string{}
	seen := map[string]bool{}
	for _, node := range doc.Syntax.Nodes {
		if node.Kind == "value" && len(node.Path) == len(path)+1 && configcontract.HasPrefix(node.Path, path) {
			key := node.Path[len(path)]
			if _, ok := m[key]; ok && !seen[key] {
				fields = append(fields, key)
				seen[key] = true
			}
		}
	}
	for _, key := range ordered(keys(m), path) {
		if !seen[key] {
			fields = append(fields, key)
		}
	}
	parts := []string{}
	for _, key := range fields {
		child := append(append([]string{}, path...), key)
		text := encodeLike(doc, child, m[key])
		if old, ok := declared(doc.Raw, child); ok && reflect.DeepEqual(old, m[key]) {
			for _, node := range doc.Syntax.Nodes {
				if node.Kind == "value" && configcontract.PathKey(node.Path) == configcontract.PathKey(child) {
					text = string(doc.Bytes[node.ValueStart:node.ValueEnd])
					break
				}
			}
		}
		parts = append(parts, configcontract.Key(key)+" = "+text)
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}
func declared(raw map[string]any, path []string) (any, bool) {
	var value any = raw
	for _, key := range path {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return value, true
}
