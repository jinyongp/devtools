package configedit

import (
	"bytes"
	"reflect"
	"sort"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func migrationComments(doc *project.Document, candidate []byte) ([]byte, *protocol.Error) {
	next, e := project.ParseDocument(candidate, doc.Path, doc.Context.Root)
	if e != nil {
		return nil, e
	}
	edits := []replacement{}
	claimed := map[int]bool{}
	exists := func(path []string) bool {
		for _, n := range next.Syntax.Nodes {
			if configcontract.PathKey(n.Path) == configcontract.PathKey(path) {
				return true
			}
		}
		return false
	}
	for _, comment := range doc.Syntax.Comments {
		lineStart := bytes.LastIndexByte(doc.Bytes[:comment.Start], '\n') + 1
		if len(bytes.TrimSpace(doc.Bytes[lineStart:comment.Start])) == 0 {
			continue
		}
		var owner *configcontract.Node
		for _, n := range doc.Syntax.Nodes {
			if n.Kind != "value" && n.Kind != "table" {
				continue
			}
			start, end := doc.Syntax.LineRange(n.Start, n.End)
			if start <= comment.Start && end >= comment.End {
				copy := n
				if n.Start < comment.Start && n.End > comment.Start {
					return nil, failure("config_preservation_failed", "migration_internal_comments", n.Path)
				}
				if n.End <= comment.Start && (owner == nil || n.End > owner.End || n.End == owner.End && n.Start < owner.Start) {
					owner = &copy
				}
			}
		}
		if owner == nil {
			continue
		}
		path := owner.Path
		index := -1
		if len(path) > 0 && path[0] == "requirements" {
			index = 0
		} else if len(path) > 2 && path[0] == "commands" && path[2] == "requirements" {
			index = 2
		}
		if index < 0 {
			continue
		}
		tail := path[index+1:]
		target := []string{"defaults"}
		if index == 2 {
			target = []string{"commands", path[1]}
		}
		if len(tail) > 0 && tail[0] == "tools" {
			if index == 2 && len(tail) > 1 {
				override := append([]string{"commands", path[1], "tool_overrides"}, tail[1:]...)
				if exists(override) {
					target = override
				} else {
					target = append([]string{"tools"}, tail[1:]...)
				}
			} else {
				target = append([]string{"tools"}, tail[1:]...)
			}
		} else {
			target = append(target, tail...)
		}
		if !exists(target) && index == 2 && len(tail) == 1 {
			commands, _ := next.Raw["commands"].(map[string]any)
			command, _ := commands[path[1]].(map[string]any)
			requires, _ := command["requires"].([]any)
			if len(requires) == 1 {
				target = []string{"presets", requires[0].(string), tail[0]}
			}
		}
		found := false
		for _, n := range next.Syntax.Nodes {
			if configcontract.PathKey(n.Path) != configcontract.PathKey(target) {
				continue
			}
			if claimed[n.End] {
				return nil, failure("config_preservation_failed", "migration_comment_ownership", path)
			}
			for _, c := range next.Syntax.Comments {
				_, end := next.Syntax.LineRange(n.Start, n.End)
				if c.Start >= n.End && c.Start < end {
					return nil, failure("config_preservation_failed", "migration_comment_ownership", path)
				}
			}
			text := " " + string(doc.Bytes[comment.Start:comment.End])
			// A migrated leaf can live inside an inline table. Terminate the
			// comment before its closing delimiter (TOML 1.1 permits newlines).
			_, lineEnd := next.Syntax.LineRange(n.Start, n.End)
			if len(bytes.TrimSpace(candidate[n.End:lineEnd])) != 0 {
				text += next.Syntax.Newline()
			}
			edits = append(edits, replacement{n.End, n.End, text})
			claimed[n.End] = true
			found = true
			break
		}
		if !found {
			return nil, failure("config_preservation_failed", "migration_comment_ownership", path)
		}
	}
	data, e := apply(candidate, edits)
	if e != nil {
		return nil, e
	}
	syntax, err := configcontract.ParseSyntax(data)
	if err != nil {
		return nil, failure("config_preservation_failed", "migration_comment_syntax", nil)
	}
	comments := func(s *configcontract.Syntax) []string {
		out := []string{}
		for _, c := range s.Comments {
			out = append(out, string(s.Bytes[c.Start:c.End]))
		}
		sort.Strings(out)
		return out
	}
	if !reflect.DeepEqual(comments(doc.Syntax), comments(syntax)) {
		return nil, failure("config_preservation_failed", "migration_comment_ownership", nil)
	}
	return data, nil
}
