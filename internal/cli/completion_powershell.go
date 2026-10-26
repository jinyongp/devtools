package cli

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
)

//go:embed completion_powershell.ps1
var powershellCompletionTemplate string

func powershellCompletion(nodes map[string]*completionNode, paths []string) string {
	type option struct {
		Name    string
		Boolean bool
		Values  []string
	}
	type node struct {
		Path      string
		Children  []string
		Options   []option
		ChildArgs bool
	}
	metadata := make([]node, 0, len(paths))
	for _, path := range paths {
		source := nodes[path]
		row := node{Path: path, Children: []string{}, Options: []option{}, ChildArgs: source.childArgsAfterName}
		for child := range source.children {
			row.Children = append(row.Children, child)
		}
		sort.Strings(row.Children)
		names := make([]string, 0, len(source.options))
		for name := range source.options {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			values := append([]string{}, patternChoices(source.options[name].Pattern)...)
			row.Options = append(row.Options, option{Name: name, Boolean: source.options[name].Boolean, Values: values})
		}
		metadata = append(metadata, row)
	}
	data, _ := json.Marshal(metadata)
	return strings.Replace(powershellCompletionTemplate, "__DEVTOOLS_METADATA__", strings.ReplaceAll(string(data), "'", "''"), 1)
}
