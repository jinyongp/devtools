package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/collectionquery"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/valuehandoff"
)

type collectionContract struct {
	Scope          string
	Query          collectionquery.Definition
	Columns        map[string]string
	DefaultColumns []string
	States         []string
	View           bool
	NativePage     bool
}

func collectionFor(name string) *collectionContract {
	c := &collectionContract{Scope: "profile", Query: collectionquery.Definition{Filters: map[string]string{}, Sorts: map[string]string{}, Times: map[string]string{}, Keys: []string{"profile", "directory", "id"}}, Columns: map[string]string{}}
	columns := func(names ...string) {
		for _, name := range names {
			c.Columns[name] = name
		}
		c.DefaultColumns = names
	}
	names := func(field string) {
		c.Query.Search = []string{field}
		c.Query.Sorts["name"] = field
		c.Query.DefaultSort = "name"
		c.Query.Keys = []string{field}
	}
	times := func(field string) { c.Query.Times["created"] = field; c.Query.Sorts["created"] = field }
	switch name {
	case "process list":
		c.Scope, c.View = "project", true
		names("command")
		c.Query.Keys = []string{"profile", "directory", "command", "id"}
		c.Query.DefaultSort, c.Query.Sorts["attention"] = "attention", "attention"
		c.Query.Filters["state"], c.Query.Filters["command"], c.Query.Filters["env"] = "state", "command", "env"
		times("created_at")
		c.Query.Times["updated"], c.Query.Times["ended"] = "updated_at", "ended_at"
		c.Query.Sorts["updated"], c.Query.Sorts["ended"] = "updated_at", "ended_at"
		c.States = []string{"running", "starting", "stopped", "exited", "failed", "interrupted", "unknown", "not_started", "conflict"}
		columns("command", "current_state", "last_attempt", "last_changed")
		for _, name := range []string{"state", "created_at", "ended_at", "reason", "execution_id", "active_count", "env", "profile", "directory"} {
			c.Columns[name] = name
		}
	case "expose list":
		c.Scope, c.View = "project", true
		names("host")
		times("created_at")
		c.Query.DefaultSort, c.Query.DefaultOrder = "created", "desc"
		c.Query.Filters["state"], c.Query.Filters["provider"] = "state", "provider"
		c.States = []string{"preparing", "starting", "active", "stopped", "auth_required", "recovery_required"}
		columns("host", "provider", "state", "url", "id")
	case "values execution list":
		c.View = true
		c.Query.Search = []string{"external_execution_id", "runner_id"}
		times("created_at")
		c.Query.Times["updated"], c.Query.Sorts["updated"] = "updated_at", "updated_at"
		c.Query.DefaultSort, c.Query.DefaultOrder = "created", "desc"
		c.Query.Filters["delivery-state"], c.Query.Filters["execution-state"] = "delivery_state", "execution_state"
		c.Query.Keys = []string{"execution_id"}
		columns("execution_id", "external_execution_id", "delivery_state", "execution_state", "created_at", "reason")
	case "variable list", "secret list":
		names("key")
		c.Query.Filters["source"] = "source"
		columns("key", "source", "overrides")
	case "env list":
		names("")
		columns("name")
		c.Columns["name"] = ""
	case "bundle list":
		names("name")
		columns("name", "bundle_id", "value_ids")
	case "command list":
		c.Scope = "directory"
		names("name")
		columns("name", "env", "inject", "exec")
	case "values uses":
		c.Scope = "directory"
		names("command")
		c.Query.Filters["env"], c.Query.Filters["kind"] = "env", "kind"
		c.Query.Keys = []string{"directory", "command", "kind", "env", "execution_id"}
		columns("command", "kind", "env", "selection", "execution_id")
	case "port list":
		c.Scope = "project"
		names("name")
		c.Query.Filters["service"], c.Query.Filters["protocol"] = "name", "protocol"
		c.Query.Sorts["port"] = "port"
		c.Query.Keys = []string{"profile", "directory", "instance_id", "name"}
		columns("name", "port", "profile", "directory", "instance_id")
	case "instance list":
		c.Scope = "project"
		names("alias")
		c.Query.Filters["location-state"] = "location_status"
		c.Query.Keys = []string{"profile", "directory", "instance_id"}
		columns("alias", "location_status", "directory", "instance_id", "profile")
	case "proxy list":
		c.Scope = "project"
		names("host")
		c.Query.Filters["state"], c.Query.Filters["listener"] = "status", "listener"
		c.Query.Keys = []string{"profile", "directory", "host", "proxy", "listener", "instance_id"}
		c.States = []string{"ready", "activation_required", "alias_missing", "location_missing", "assignment_missing", "host_conflict", "config_unavailable", "instance_conflict", "host_invalid", "disabled", "unknown"}
		columns("host", "target_port", "listener", "status", "profile", "directory")
	case "profile list":
		c.Scope = "global"
		names("profile")
		columns("profile", "values", "tasks", "instance_count", "active_process_count")
	case "cleanup archives":
		c.Scope = "global"
		names("kind")
		times("archived_at")
		c.Query.DefaultSort, c.Query.DefaultOrder = "created", "desc"
		c.Query.Filters["kind"], c.Query.Filters["archive-state"] = "kind", "archive_state"
		columns("kind", "profile", "archived_at", "archive_state", "id", "bytes")
	case "task list", "task workstream list", "task validation list":
		c.Scope, c.NativePage = "task", true
		names("title")
		c.Query.DefaultSort = "" // Established task order is intentional.
		times("created_at")
		c.Query.Times["updated"], c.Query.Sorts["updated"] = "updated_at", "updated_at"
		if name != "task validation list" {
			c.Query.Filters["state"] = "state"
			c.States = []string{"open", "done", "canceled", "all"}
			if name == "task workstream list" {
				c.States = []string{"draft", "active", "done", "canceled", "all"}
			}
		} else {
			c.Query.Filters["recorded-result"] = "validation.latest_record.result"
		}
		columns("title", "state", "completion_status", "id")
	case "task current":
		c.Scope, c.NativePage = "task", true
		times("started_at")
		c.Query.Times["updated"], c.Query.Sorts["updated"] = "last_activity_at", "last_activity_at"
		c.Query.Search = []string{"task_id"}
		c.Query.Filters["task"], c.Query.Filters["workstream"] = "task_id", "workstream_id"
		columns("task_id", "state", "started_at", "last_activity_at", "id")
	case "task history", "task workstream history", "task checkpoint list":
		c.Scope, c.NativePage = "task", true
		times("occurred_at")
		c.Query.DefaultSort, c.Query.DefaultOrder = "created", "desc"
		c.Query.Search = []string{"action"}
		c.Query.Filters["action"] = "action"
		columns("occurred_at", "action", "target_id", "sequence", "id")
	default:
		return nil
	}
	if c.NativePage {
		c.Query = tasks.NavigationDefinition(strings.TrimPrefix(name, "task "))
		for _, field := range c.Query.Times {
			c.Columns[field] = field
		}
		if name == "task history" || name == "task workstream history" || name == "task checkpoint list" {
			c.Columns["summary"] = "data.summary"
		}
	}
	if name == "task validation list" {
		columns("title", "recorded_result", "required", "id")
	}
	return c
}

func choicePattern(values []string) string { return "^(" + strings.Join(values, "|") + ")$" }
func mapKeys(values map[string]string) []string {
	keys := []string{}
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (a *App) collectionContract(c *Command) {
	contract := collectionFor(c.Name)
	if contract == nil {
		return
	}
	c.Collection = contract
	add := func(o Option) {
		for i := range c.Options {
			if c.Options[i].Name == o.Name {
				c.Options[i] = o
				return
			}
		}
		c.Options = append(c.Options, o)
	}
	add(Option{Name: "limit", Default: "50", Pattern: `^([1-9]|[1-9][0-9]|1[0-9]{2}|200)$`, Description: "Page size, 1 through 200; defaults to 50."})
	add(Option{Name: "cursor", Description: "Continue the same snapshot with the same scope, filters and sort."})
	add(Option{Name: "query-cache-dir", MinLength: 1, Description: "Absolute private snapshot cache outside the data root; keep it for every continuation."})
	if len(contract.Query.Search) > 0 {
		add(Option{Name: "query", Description: "Case-insensitive substring of public names/commands; never searches values or logs."})
	}
	if len(contract.Query.Sorts) > 0 {
		add(Option{Name: "sort", Pattern: choicePattern(mapKeys(contract.Query.Sorts)), Description: "Sort field; ties use stable resource identity."})
		add(Option{Name: "order", Pattern: "^(asc|desc)$", Description: "Sort direction."})
	}
	if len(contract.Query.Times) > 0 {
		for _, name := range []string{"since", "until"} {
			add(Option{Name: name, MinLength: 1, Description: "RFC3339 timestamp with timezone; selects since <= time < until."})
		}
		add(Option{Name: "time-field", Pattern: choicePattern(mapKeys(contract.Query.Times)), Description: "Recorded timestamp to filter; default created."})
	}
	for _, name := range mapKeys(contract.Query.Filters) {
		o := Option{Name: name, MinLength: 1, Description: "Filter this public metadata field."}
		if name == "task" || name == "workstream" {
			o.Pattern = uuidPattern
		}
		if name == "recorded-result" {
			o.Pattern = "^(none|pass|fail|blocked|skipped)$"
		}
		if name == "protocol" {
			o.Pattern = "^(tcp)$"
		}
		if name == "provider" {
			o.Pattern = "^(lan|cloudflared|tailscale)$"
		}
		if name == "location-state" {
			o.Pattern = "^(available|missing|unknown)$"
		}
		if name == "archive-state" {
			o.Pattern = "^(archived|restored|purged)$"
		}
		if name == "kind" && c.Name == "task checkpoint list" {
			o.Pattern = "^(run.synced|run.checkpointed|run.released|task.completed)$"
		}
		if name == "kind" && c.Name == "values uses" {
			o.Pattern = "^(declared|delivered|prepared)$"
		}
		if name == "state" {
			o.Pattern, o.Repeatable = choicePattern(contract.States), true
			o.Description = "State filter; repeat for OR. Other filters combine with AND."
		}
		if name == "env" && c.Name == "process list" {
			o.MinLength, o.Pattern = 0, "^$|"+project.ProfilePattern
		}
		if name == "delivery-state" || name == "execution-state" {
			o.Repeatable = true
			if name == "delivery-state" {
				o.Pattern = choicePattern(valuehandoff.DeliveryStates)
			} else {
				o.Pattern = choicePattern(valuehandoff.ExecutionStates)
			}
		}
		add(o)
	}
	if contract.View {
		add(Option{Name: "view", Default: "current", Pattern: "^(current|history)$", Description: "Current resources needing attention, or individual historical attempts."})
	}
	if contract.Scope == "project" || contract.Scope == "profile" {
		add(Option{Name: "profile", Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128, Completion: completeProfile, Description: "Select one profile independently of cwd; project resources include all its locations."})
		add(Option{Name: "dir", MinLength: 1, Description: "Select a canonical project directory; intersects other scope selectors."})
	}
	if contract.Scope == "project" {
		add(Option{Name: "all-profiles", Boolean: true, Description: "Explicitly select all profiles; excludes --profile."})
		add(Option{Name: "instance", Pattern: instanceSelectorPattern, Completion: completeInstance, Description: "Instance alias or id:ID; intersects profile/directory."})
		if c.Name == "expose list" {
			add(Option{Name: "global", Boolean: true, Description: "Select projectless exposures only."})
		}
	}
	if c.Name == "cleanup archives" {
		add(Option{Name: "profile", Pattern: project.ProfilePattern, Completion: completeProfile, Description: "Filter archived profile metadata."})
	}
	add(Option{Name: "columns", Repeatable: true, Unique: true, Pattern: choicePattern(mapKeys(contract.Columns)), Description: "Text table column; repeat in display order. Requires effective text output."})
	publicResultField(c.Output, "next_cursor", map[string]any{"type": []string{"string", "null"}}, true)
	publicResultField(c.Output, "selection", map[string]any{"type": "object"}, true)
	publicResultField(c.Output, "page", object(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}, "count": map[string]any{"type": "integer", "minimum": 0}, "has_more": boolSchema()}, "limit", "count", "has_more"), true)
	publicResultField(c.Output, "next_actions", arraySchema(remedySchema()), false)
	run := c.Run
	c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		return a.collectionResult(ctx, streams, *c, r, run)
	}
}

func (a *App) collectionResult(ctx context.Context, streams IO, c Command, r Request, run func(context.Context, IO, Request) (any, *protocol.Error)) (any, *protocol.Error) {
	definition := c.Collection.Query
	if c.Name == "process list" && r.Options["view"] == "history" {
		definition.DefaultSort, definition.DefaultOrder = "created", "desc"
	}
	query, parseErr := collectionquery.Parse(definition, r.Options, r.ListOptions)
	if parseErr != nil {
		return nil, argumentError(parseErr.Error()+".", "query")
	}
	if raw, present := r.Options["query-cache-dir"]; present {
		if e := tasks.ValidateQueryCachePath(raw); e != nil {
			return nil, e
		}
	}
	if c.Collection.NativePage {
		value, e := run(ctx, streams, r)
		if e != nil {
			return value, e
		}
		out := publicResultObject(value)
		if out == nil {
			return value, protocol.NewError("internal_error", "Invalid collection result.", 1, nil)
		}
		out["selection"] = map[string]any{"scope": "profile", "profile": out["profile"], "filters": query}
		r = cloneCollectionRequest(r)
		if profile, ok := out["profile"].(string); ok {
			r.Options["profile"] = profile
		}
		return a.collectionPage(c, r, out, query.Limit), nil
	}
	dirs, err := a.currentPaths()
	if err != nil {
		return nil, directoryError(err)
	}
	cache := r.Options["query-cache-dir"]
	if cache == "" {
		cache = filepath.Join(dirs.Cache, "collections")
	}
	data, e := a.dataDirectory()
	if e != nil {
		return nil, e
	}
	pager := tasks.CollectionPager{DataRoot: filepath.Dir(data), CacheDirectory: cache}
	fingerprint, e := collectionFingerprint(c, r, pager.DataRoot)
	if e != nil {
		return nil, e
	}
	if cursor := r.Options["cursor"]; cursor != "" {
		page, e := pager.Continue(fingerprint, cursor, query.Limit)
		if e != nil {
			return nil, e
		}
		if c.Name == "values execution list" {
			selected := cloneCollectionRequest(r)
			if profile, ok := page["profile"].(string); ok {
				selected.Options["profile"] = profile
			}
			store, e := a.executionStore(selected)
			if e != nil {
				return nil, e
			}
			if e := store.AuthenticateQuery(ctx); e != nil {
				return nil, executionRecovery(store, selected, e)
			}
		}
		return a.collectionPage(c, r, map[string]any(page), query.Limit), nil
	}
	scope, e := a.resolveCollectionScope(ctx, c, r)
	if e != nil {
		return nil, e
	}
	provider := cloneCollectionRequest(r)
	provider.Scope = &scope
	if scope.Profile != "" {
		provider.Options["profile"] = scope.Profile
	}
	if scope.Directory != "" {
		provider.Options["dir"] = scope.Directory
	}
	value, e := run(ctx, streams, provider)
	if e != nil {
		return value, e
	}
	out := publicResultObject(value)
	if out == nil {
		return nil, protocol.NewError("internal_error", "Invalid collection result.", 1, nil)
	}
	rows, _ := out["items"].([]any)
	if c.Name == "cleanup archives" {
		for _, value := range rows {
			if row, ok := value.(map[string]any); ok {
				state := "archived"
				if row["restored_at"] != nil {
					state = "restored"
				}
				if row["purged_at"] != nil {
					state = "purged"
				}
				row["archive_state"] = state
			}
		}
	}
	selected := []any{}
	for _, row := range rows {
		if scope.matches(row) {
			selected = append(selected, row)
		}
	}
	selected = collectionquery.Select(selected, definition, query)
	delete(out, "items")
	delete(out, "next_actions")
	delete(out, "next_cursor")
	out["selection"] = map[string]any{"scope": scope, "view": r.Options["view"], "filters": query}
	page, e := pager.First(ctx, fingerprint, tasks.Object(out), selected, query.Limit)
	if e != nil {
		return nil, e
	}
	return a.collectionPage(c, r, map[string]any(page), query.Limit), nil
}

func cloneCollectionRequest(r Request) Request {
	options := map[string]string{}
	for key, value := range r.Options {
		options[key] = value
	}
	r.Options = options
	return r
}

func collectionFingerprint(c Command, r Request, dataRoot string) (string, *protocol.Error) {
	options := map[string]any{}
	definition := c.Collection.Query
	if c.Name == "process list" && r.Options["view"] == "history" {
		definition.DefaultSort, definition.DefaultOrder = "created", "desc"
	}
	query, err := collectionquery.Parse(definition, r.Options, r.ListOptions)
	if err != nil {
		return "", argumentError(err.Error()+".", "query")
	}
	for key, value := range r.Options {
		if key == "cursor" || key == "limit" || key == "columns" || key == "query-cache-dir" || key == "isolated-root" || key == "query" || key == "sort" || key == "order" || key == "time-field" || key == "since" || key == "until" {
			continue
		}
		if _, filter := definition.Filters[key]; filter {
			continue
		}
		if values := r.ListOptions[key]; len(values) > 0 {
			copied := append([]string{}, values...)
			sort.Strings(copied)
			options[key] = copied
		} else {
			options[key] = value
		}
	}
	if c.Collection.Scope != "global" && c.Collection.Scope != "task" && (r.Options["profile"] == "" && r.Options["all-profiles"] != "true" && r.Options["global"] != "true" || r.Options["dir"] != "") {
		dir := r.Options["dir"]
		if dir == "" {
			dir = "."
		}
		canonical, err := location.Canonical(dir)
		if err != nil {
			return "", argumentError("Cannot resolve the collection directory.", "dir")
		}
		options["dir"] = canonical
	}
	b, err := json.Marshal(map[string]any{"root": dataRoot, "command": c.Name, "args": r.Args, "options": options, "query": query})
	if err != nil {
		return "", argumentError("Cannot normalize collection inputs.", "query")
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func (a *App) collectionPage(c Command, r Request, out map[string]any, limit int) map[string]any {
	rows, _ := out["items"].([]any)
	cursor, _ := out["next_cursor"].(string)
	if cursor == "" {
		out["next_cursor"] = nil
	}
	out["page"] = map[string]any{"limit": limit, "count": len(rows), "has_more": cursor != ""}
	if cursor != "" {
		argv := collectionArgv(c, r, cursor)
		out["next_actions"] = []protocol.Remedy{protocol.LocalAction(argv, nil, "Read the next page with the same selection.", "read").Remedy(r.Invocation)}
	}
	return out
}

func collectionArgv(c Command, r Request, cursor string) []string {
	argv := []string{"devtools"}
	if len(r.ListOptions["columns"]) > 0 {
		argv = append(argv, "--format", "text")
	}
	argv = append(argv, strings.Fields(c.Name)...)
	argv = append(argv, r.Args...)
	for _, o := range c.Options {
		if o.Name == "cursor" || o.Name == "isolated-root" {
			continue
		}
		value, present := r.Options[o.Name]
		if !present {
			continue
		}
		if c.Name == "task current" && o.Name == "dir" {
			if canonical, err := location.Canonical(value); err == nil {
				value = canonical
			}
		}
		if o.Repeatable {
			for _, v := range r.ListOptions[o.Name] {
				argv = append(argv, "--"+o.Name+"="+v)
			}
		} else {
			argv = append(argv, "--"+o.Name+"="+value)
		}
	}
	// Pin an inferred cwd for directory collections without changing profile-only
	// selection. The fingerprint normalizes implicit and explicit dir equally.
	if c.Collection.Scope != "global" && c.Collection.Scope != "task" && r.Options["profile"] == "" && r.Options["all-profiles"] != "true" && r.Options["global"] != "true" && r.Options["dir"] == "" {
		if dir, err := location.Canonical("."); err == nil {
			argv = append(argv, "--dir", dir)
		}
	}
	if cursor != "" {
		argv = append(argv, "--cursor", cursor)
	}
	return argv
}
