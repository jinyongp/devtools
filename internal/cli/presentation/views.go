package presentation

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/jinyongp/devtools/internal/cliui"
)

// Definition records why a field may be summarized or omitted. Unknown fields
// never enter an implicit omission rule, including fields added inside a row.
type Definition struct {
	Family       string
	Title        string
	Required     []Requirement
	Optional     []string
	KnownOmitted []Omission
	Columns      []ViewColumn
}
type Requirement struct{ Path, Type string }
type Omission struct{ Path, Rule string }
type ViewColumn struct{ Path, Label, Display string }

var readViews = map[string]Definition{
	"version": {Family: "detail", Title: "Version", Required: []Requirement{{"version", "string"}, {"commit", "string"}, {"protocol_version", "number"}}},
	"command list": {
		Family: "commands", Title: "Commands",
		Required:     []Requirement{{"profile", "string"}, {"items", "array"}, {"items.*.name", "string"}, {"items.*.exec", "strings"}, {"items.*.inject", "boolean"}, {"items.*.env", "string"}, {"items.*.serve", "strings"}},
		Columns:      []ViewColumn{{"name", "Command", ""}, {"env", "Environment", ""}, {"inject", "Values", "onoff"}, {"exec", "Exec", "argv"}},
		Optional:     []string{"items.*.selection", "items.*.values_update", "items.*.output"},
		KnownOmitted: []Omission{{"items.*.serve", "empty-array"}, {"items.*.values_update", "notify"}, {"items.*.output", "default-output"}},
	},
	"command inspect": {
		Family: "detail", Title: "Command",
		Required:     []Requirement{{"profile", "string"}, {"item", "object"}, {"item.name", "string"}, {"item.exec", "strings"}, {"item.env", "string"}, {"item.inject", "boolean"}},
		Optional:     []string{"values", "item.selection", "item.serve", "item.bind", "item.requirements", "item.ready", "item.output", "item.values_update"},
		KnownOmitted: []Omission{{"item.serve", "empty-array"}, {"item.bind", "empty-object"}, {"item.ready", "null"}, {"item.requirements", "empty-requirements"}, {"item.values_update", "notify"}, {"item.output", "default-output"}},
	},
	"command explain":         {Family: "detail", Title: "Command execution", Required: []Requirement{{"", "object"}}},
	"command env":             {Family: "detail", Title: "Command bindings", Required: []Requirement{{"env", "object"}, {"env_keys", "strings"}, {"pending", "array"}}},
	"config inspect":          {Family: "detail", Title: "Configuration", Required: []Requirement{{"", "object"}}},
	"project inspect":         {Family: "detail", Title: "Project", Required: []Requirement{{"item", "object"}, {"paths", "object"}}},
	"project status":          {Family: "list", Title: "Project processes", Required: []Requirement{{"profile", "string"}, {"directory", "string"}, {"items", "array"}, {"items.*.id", "string"}, {"items.*.command", "string"}, {"items.*.state", "string"}}, Columns: []ViewColumn{{"command", "Command", ""}, {"state", "State", ""}, {"id", "Execution ID", ""}}},
	"project logs":            {Family: "log", Title: "Project logs", Required: []Requirement{{"id", "string"}, {"command", "string"}, {"content", "string"}}},
	"variable list":           {Family: "list", Title: "Variables", Required: []Requirement{{"profile", "string"}, {"items", "array"}, {"items.*.key", "string"}, {"items.*.source", "string"}, {"items.*.overrides", "boolean"}}, Columns: []ViewColumn{{"key", "Key", ""}, {"source", "Source", ""}, {"overrides", "Overrides", ""}}},
	"secret list":             {Family: "list", Title: "Secrets", Required: []Requirement{{"profile", "string"}, {"items", "array"}, {"items.*.key", "string"}, {"items.*.source", "string"}, {"items.*.overrides", "boolean"}}, Columns: []ViewColumn{{"key", "Key", ""}, {"source", "Source", ""}, {"overrides", "Overrides", ""}}},
	"variable get":            {Family: "value", Title: "Variable", Required: []Requirement{{"profile", "string"}, {"value", "string"}, {"metadata", "object"}, {"integrity_verified", "boolean"}}},
	"env list":                {Family: "list", Title: "Environments", Required: []Requirement{{"profile", "string"}, {"items", "strings"}}, Columns: []ViewColumn{{"", "Environment", ""}}},
	"bundle list":             {Family: "list", Title: "Bundles", Required: []Requirement{{"profile", "string"}, {"revision", "string"}, {"items", "array"}, {"items.*.name", "string"}, {"items.*.bundle_id", "string"}}, Columns: []ViewColumn{{"name", "Bundle", ""}, {"bundle_id", "Bundle ID", ""}}},
	"values inspect":          {Family: "detail", Title: "Value inputs", Required: []Requirement{{"profile", "string"}, {"env", "string"}, {"selection", "string"}, {"values", "object"}}},
	"values revision":         {Family: "detail", Title: "Value revision", Required: []Requirement{{"profile", "string"}, {"env", "string"}, {"revision", "string"}, {"integrity_verified", "boolean"}}},
	"values uses":             {Family: "list", Title: "Value uses", Required: []Requirement{{"profile", "string"}, {"items", "array"}, {"items.*.kind", "string"}, {"items.*.command", "string"}, {"items.*.env", "string"}}, Columns: []ViewColumn{{"command", "Command", ""}, {"kind", "Kind", ""}, {"env", "Environment", ""}}},
	"values request show":     {Family: "detail", Title: "Saved value request", Required: []Requirement{{"profile", "string"}, {"item", "object"}}},
	"values execution status": {Family: "detail", Title: "Value execution", Required: []Requirement{{"profile", "string"}, {"item", "object"}}},
	"values execution list":   {Family: "list", Title: "Value executions", Required: []Requirement{{"profile", "string"}, {"items", "array"}, {"items.*.execution_id", "string"}, {"items.*.delivery_state", "string"}, {"items.*.execution_state", "string"}}, Columns: []ViewColumn{{"execution_id", "Execution ID", ""}, {"delivery_state", "Delivery", ""}, {"execution_state", "Execution", ""}}},
	"values runner show":      {Family: "detail", Title: "Value runner", Required: []Requirement{{"profile", "string"}, {"item", "object"}}},
	"vault status":            {Family: "detail", Title: "Vault", Required: []Requirement{{"item", "object"}}},
	"profile list":            {Family: "list", Title: "Profiles", Required: []Requirement{{"items", "array"}, {"items.*.profile", "string"}, {"items.*.values", "boolean"}, {"items.*.tasks", "boolean"}}, Columns: []ViewColumn{{"profile", "Profile", ""}, {"values", "Values", "onoff"}, {"tasks", "Tasks", "onoff"}}},
	"profile inspect":         {Family: "detail", Title: "Profile", Required: []Requirement{{"item", "object"}}},
	"instance list":           {Family: "list", Title: "Instances", Required: []Requirement{{"items", "array"}, {"items.*.instance_id", "string"}, {"items.*.profile", "string"}, {"items.*.directory", "string"}, {"items.*.location_status", "string"}}, Columns: []ViewColumn{{"profile", "Profile", ""}, {"location_status", "Location", ""}, {"directory", "Directory", ""}, {"instance_id", "Instance ID", ""}}},
	"instance show":           {Family: "detail", Title: "Instance", Required: []Requirement{{"item", "object"}}},
	"port list":               {Family: "list", Title: "Ports", Required: []Requirement{{"items", "array"}, {"items.*.name", "string"}, {"items.*.port", "number"}, {"items.*.profile", "string"}, {"items.*.instance_id", "string"}}, Columns: []ViewColumn{{"name", "Name", ""}, {"port", "Port", ""}, {"profile", "Profile", ""}, {"instance_id", "Instance ID", ""}}},
	"port show":               {Family: "detail", Title: "Port", Required: []Requirement{{"item", "object"}}},
	"proxy list":              {Family: "list", Title: "Proxies", Required: []Requirement{{"items", "array"}, {"items.*.kind", "string"}, {"items.*.status", "string"}}, Columns: []ViewColumn{{"kind", "Kind", ""}, {"status", "Status", ""}}},
	"proxy status":            {Family: "detail", Title: "Proxy status", Required: []Requirement{{"", "object"}}},
	"proxy env":               {Family: "detail", Title: "Proxy environment", Required: []Requirement{{"", "object"}}},
	"proxy logs":              {Family: "list", Title: "Proxy events", Required: []Requirement{{"items", "array"}, {"items.*.time", "string"}, {"items.*.listener", "string"}, {"items.*.state", "string"}}, Columns: []ViewColumn{{"time", "Time", ""}, {"listener", "Listener", ""}, {"state", "State", ""}}},
	"expose list":             {Family: "list", Title: "Exposures", Required: []Requirement{{"items", "array"}, {"items.*.id", "string"}, {"items.*.state", "string"}, {"items.*.url", "string"}}, Columns: []ViewColumn{{"id", "ID", ""}, {"state", "State", ""}, {"url", "URL", ""}}},
	"expose status":           {Family: "detail", Title: "Exposure", Required: []Requirement{{"item", "object"}}},
	"dashboard status":        {Family: "detail", Title: "Dashboard", Required: []Requirement{{"item", "object"}}},
	"proxy ca status":         {Family: "detail", Title: "Certificate authority", Required: []Requirement{{"item", "object"}, {"trusted", "boolean"}}},
	"backup status":           {Family: "detail", Title: "Backup status", Required: []Requirement{{"item", "object"}}},
	"backup inspect":          {Family: "detail", Title: "Backup", Required: []Requirement{{"item", "object"}}},
	"process list":            {Family: "list", Title: "Processes", Required: []Requirement{{"items", "array"}, {"items.*.command", "string"}, {"items.*.state", "string"}, {"items.*.id", "string"}}, Columns: []ViewColumn{{"command", "Command", ""}, {"state", "State", ""}, {"id", "Execution ID", ""}}},
	"process status":          {Family: "detail", Title: "Process", Required: []Requirement{{"item", "object"}, {"values", "object"}}},
	"process logs":            {Family: "log", Title: "Process logs", Required: []Requirement{{"id", "string"}, {"content", "string"}}},
}

func DefinitionFor(command string) Definition {
	d := readViews[command]
	if d.Family == "" {
		d = operationDefinition(command)
	}
	d.Required = append([]Requirement(nil), d.Required...)
	d.Optional = append([]string(nil), d.Optional...)
	d.KnownOmitted = append([]Omission(nil), d.KnownOmitted...)
	d.Columns = append([]ViewColumn(nil), d.Columns...)
	return d
}

func matches(value any, path []string, kind string) bool {
	if len(path) > 0 && path[0] != "" {
		if path[0] == "*" {
			values, ok := value.([]any)
			if !ok {
				return false
			}
			for _, v := range values {
				if !matches(v, path[1:], kind) {
					return false
				}
			}
			return true
		}
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		v, exists := object[path[0]]
		return exists && matches(v, path[1:], kind)
	}
	switch kind {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "strings":
		_, ok := stringArray(value)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := value.(json.Number)
		return ok
	}
	return false
}

func buildView(definition Definition, value any) (cliui.Document, bool) {
	if definition.Family == "" {
		return cliui.Document{}, false
	}
	for _, requirement := range definition.Required {
		if !matches(value, strings.Split(requirement.Path, "."), requirement.Type) {
			return cliui.Document{}, false
		}
	}
	data, ok := value.(map[string]any)
	if !ok {
		return cliui.Document{}, false
	}
	if definition.Family == "list" {
		return listView(definition, data), true
	}
	if definition.Family == "commands" {
		return commandListView(data), true
	}
	if definition.Family == "mutation" || definition.Family == "preview" || definition.Family == "diagnostic" || definition.Family == "task" {
		return operationView(definition, data)
	}
	blocks := []cliui.Block{cliui.Heading(cliui.Literal(definition.Title), 1)}
	consumed := map[string]bool{}
	blocks = append(blocks, identity(data, consumed)...)
	blocks = append(blocks, scope(data, consumed)...)
	if item, ok := data["item"].(map[string]any); ok {
		consumed["item"] = true
		blocks = append(blocks, detail(item, "item", definition)...)
	}
	if definition.Family == "value" || definition.Family == "log" {
		key := "value"
		if definition.Family == "log" {
			key = "content"
		} else if metadata, valid := data["metadata"].(map[string]any); valid {
			consumed["metadata"] = true
			blocks = append(blocks, detail(metadata, "metadata", definition)...)
		}
		consumed[key] = true
		text := data[key].(string)
		if text == "" {
			text = `""`
		}
		blocks = append(blocks, cliui.Section(cliui.Literal(title(key)), cliui.Paragraph(cliui.Literal(text))))
	}
	blocks = append(blocks, remaining(data, consumed, "", definition)...)
	return cliui.NewDocument(blocks...), true
}

func identity(data map[string]any, consumed map[string]bool) []cliui.Block {
	var fields []cliui.Field
	for _, key := range []string{"name", "id", "command", "version", "protocol_version", "commit", "state", "status", "revision", "request_id"} {
		if value, exists := data[key]; exists && !consumed[key] && scalar(value) {
			fields = append(fields, cliui.Field{Label: cliui.Literal(title(key)), Value: display(value)})
			consumed[key] = true
		}
	}
	if len(fields) > 0 {
		return []cliui.Block{cliui.Fields(fields...)}
	}
	return nil
}

func scope(data map[string]any, consumed map[string]bool) []cliui.Block {
	var fields []cliui.Field
	for _, key := range []string{"profile", "env", "directory"} {
		if value, exists := data[key]; exists {
			if _, ok := value.(string); ok {
				fields = append(fields, cliui.Field{Label: cliui.Styled(title(key), cliui.Muted), Value: display(value)})
				consumed[key] = true
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return []cliui.Block{cliui.Fields(fields...)}
}

func display(value any) cliui.Text {
	if s, ok := value.(string); ok && s != "" {
		return cliui.Literal(s)
	}
	return cliui.Literal(literal(value))
}

func omit(path string, value any, definition Definition) bool {
	for _, omission := range definition.KnownOmitted {
		if path != omission.Path {
			continue
		}
		switch omission.Rule {
		case "empty-array":
			v, ok := value.([]any)
			return ok && len(v) == 0
		case "empty-object":
			v, ok := value.(map[string]any)
			return ok && len(v) == 0
		case "null":
			return value == nil
		case "notify":
			return value == "notify"
		case "default-output":
			v, ok := value.(map[string]any)
			return ok && len(v) == 1 && v["mode"] == "auto"
		case "empty-requirements":
			v, ok := value.(map[string]any)
			if !ok || len(v) != 3 {
				return false
			}
			tools, toolsOK := v["tools"].(map[string]any)
			vars, varsOK := v["vars"].([]any)
			secs, secsOK := v["secs"].([]any)
			return toolsOK && varsOK && secsOK && len(tools)+len(vars)+len(secs) == 0
		}
	}
	return false
}

func remaining(data map[string]any, consumed map[string]bool, prefix string, definition Definition) []cliui.Block {
	rest := map[string]any{}
	for key, value := range data {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if !consumed[key] && !omit(path, value, definition) {
			rest[key] = value
		}
	}
	if len(rest) == 0 {
		return nil
	}
	return []cliui.Block{cliui.Section(cliui.Literal("Details"), tree(rest)...)}
}

func detail(data map[string]any, prefix string, definition Definition) []cliui.Block {
	consumed := map[string]bool{}
	var fields []cliui.Field
	for _, key := range []string{"name", "key", "id", "profile", "state", "status", "completion_status", "execution_status", "env", "inject", "selection", "exec"} {
		value, exists := data[key]
		if !exists || !scalar(value) && key != "exec" {
			continue
		}
		text := display(value)
		if key == "exec" {
			argv, valid := stringArray(value)
			if !valid {
				continue
			}
			text = cliui.Literal(argvDisplay(argv))
		}
		fields = append(fields, cliui.Field{Label: cliui.Literal(title(key)), Value: text})
		consumed[key] = true
	}
	var result []cliui.Block
	if len(fields) > 0 {
		result = append(result, cliui.Fields(fields...))
	}
	return append(result, remaining(data, consumed, prefix, definition)...)
}

func argvDisplay(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		parts[i] = arg
		if arg == "" || strings.ContainsAny(arg, "\"'\\$`;|&<>()*?[]{}") || strings.ContainsFunc(arg, unicode.IsSpace) {
			parts[i] = strconv.Quote(arg)
		}
	}
	if len(parts) == 0 {
		return "[]"
	}
	return strings.Join(parts, " ")
}

// A command inventory answers "what can I run?". Configuration belongs in
// command inspect; repeating its defaults beneath every row defeats that task.
func commandListView(data map[string]any) cliui.Document {
	items := data["items"].([]any)
	profile, _ := data["profile"].(string)
	blocks := []cliui.Block{
		cliui.Heading(cliui.Literal(fmt.Sprintf("Commands (%d)", len(items))), 1),
		cliui.Fields(cliui.Field{Label: cliui.Styled("Profile", cliui.Muted), Value: cliui.Literal(profile)}),
	}
	var fields []cliui.Field
	for _, value := range items {
		row := value.(map[string]any)
		argv, _ := stringArray(row["exec"])
		text := argvDisplay(argv)
		var settings []string
		if row["inject"] == true {
			environment, _ := row["env"].(string)
			if environment == "" {
				environment = "common"
			}
			settings = append(settings, "values: "+environment)
		} else if environment, ok := row["env"].(string); ok && environment != "" {
			settings = append(settings, "env: "+environment)
		}
		if selection, ok := row["selection"].(string); ok && selection != "" && selection != "all" {
			settings = append(settings, "bundle: "+selection)
		}
		if served, ok := stringArray(row["serve"]); ok && len(served) > 0 {
			settings = append(settings, "serves: "+strings.Join(served, ", "))
		}
		if output, ok := row["output"].(map[string]any); ok {
			switch output["mode"] {
			case "raw":
				settings = append(settings, "unmasked output")
			case "redacted":
				settings = append(settings, "masked output")
			}
		}
		if row["values_update"] == "restart" {
			settings = append(settings, "restart on value changes")
		}
		if len(settings) > 0 {
			text += "  [" + strings.Join(settings, "; ") + "]"
		}
		fields = append(fields, cliui.Field{Label: cliui.Styled(row["name"].(string), cliui.Info), Value: cliui.Literal(text)})
	}
	if len(fields) == 0 {
		blocks = append(blocks, cliui.Paragraph(cliui.Words("No commands configured.")))
	} else {
		blocks = append(blocks, cliui.Fields(fields...))
	}
	if warnings, exists := data["warnings"]; exists {
		blocks = append(blocks, named("warnings", warnings)...)
	}
	if cursor, ok := data["next_cursor"].(string); ok && cursor != "" {
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Next page"), Value: cliui.Literal(cursor)}))
	}
	return cliui.NewDocument(blocks...)
}

func listView(definition Definition, data map[string]any) cliui.Document {
	items := data["items"].([]any)
	blocks := []cliui.Block{cliui.Heading(cliui.Literal(fmt.Sprintf("%s (%d)", definition.Title, len(items))), 1)}
	consumed := map[string]bool{"items": true}
	blocks = append(blocks, scope(data, consumed)...)
	if len(items) == 0 {
		blocks = append(blocks, cliui.Paragraph(cliui.Words("No items.")))
	} else {
		columns := make([]cliui.Column, len(definition.Columns))
		for i, column := range definition.Columns {
			columns[i] = cliui.Column{Label: cliui.Literal(column.Label)}
		}
		rows := make([]cliui.Row, len(items))
		for i, item := range items {
			row, _ := item.(map[string]any)
			used := map[string]bool{}
			var extra []cliui.Block
			for _, column := range definition.Columns {
				value := item
				if column.Path != "" {
					value = row[column.Path]
					used[column.Path] = true
				}
				text := display(value)
				switch column.Display {
				case "onoff":
					label := "Off"
					if value == true {
						label = "On"
					}
					text = cliui.Literal(label)
				case "argv":
					argv, _ := stringArray(value)
					text = cliui.Literal(argvDisplay(argv))
				}
				rows[i].Cells = append(rows[i].Cells, text)
			}
			if row != nil {
				extra = append(extra, remaining(row, used, "items.*", definition)...)
			}
			if len(extra) > 0 {
				label := fmt.Sprintf("Item %d", i+1)
				if len(rows[i].Cells) > 0 {
					label += ": " + rows[i].Cells[0].Spans[0].Text
				}
				rows[i].Details = []cliui.Block{cliui.Section(cliui.Literal(label), extra...)}
			}
		}
		table, _ := cliui.Table(columns, rows)
		blocks = append(blocks, table)
	}
	blocks = append(blocks, remaining(data, consumed, "", definition)...)
	return cliui.NewDocument(blocks...)
}
