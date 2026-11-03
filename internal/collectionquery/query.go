// Package collectionquery selects public metadata rows before pagination.
// It has no storage, session or presentation side effects.
package collectionquery

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Definition struct {
	Search       []string
	Filters      map[string]string
	Sorts        map[string]string
	Times        map[string]string
	Keys         []string
	DefaultSort  string
	DefaultOrder string
}

type Query struct {
	Limit     int                 `json:"-"`
	Sort      string              `json:"sort,omitempty"`
	Order     string              `json:"order"`
	Search    string              `json:"query,omitempty"`
	TimeField string              `json:"time_field,omitempty"`
	Since     *time.Time          `json:"since,omitempty"`
	Until     *time.Time          `json:"until,omitempty"`
	Filters   map[string][]string `json:"filters"`
}

func Parse(d Definition, options map[string]string, lists map[string][]string) (Query, error) {
	q := Query{Limit: 50, Sort: d.DefaultSort, Order: d.DefaultOrder, Search: normalize(options["query"]), Filters: map[string][]string{}}
	if q.Order == "" {
		q.Order = "asc"
	}
	if value := options["limit"]; value != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > 200 {
			return q, fmt.Errorf("limit must be between 1 and 200")
		}
		q.Limit = n
	}
	if value := options["sort"]; value != "" {
		q.Sort = value
	}
	if q.Sort != "" {
		if _, ok := d.Sorts[q.Sort]; !ok {
			return q, fmt.Errorf("choose an advertised sort field")
		}
	}
	if value := options["order"]; value != "" {
		q.Order = value
	}
	if q.Order != "asc" && q.Order != "desc" {
		return q, fmt.Errorf("order must be asc or desc")
	}
	q.TimeField = options["time-field"]
	if q.TimeField == "" && len(d.Times) > 0 {
		q.TimeField = "created"
		if d.Times[q.TimeField] == "" {
			for _, name := range sortedKeys(d.Times) {
				q.TimeField = name
				break
			}
		}
	}
	if q.TimeField != "" && d.Times[q.TimeField] == "" {
		return q, fmt.Errorf("choose an advertised time field")
	}
	for name, into := range map[string]**time.Time{"since": &q.Since, "until": &q.Until} {
		if raw := options[name]; raw != "" {
			if len(d.Times) == 0 {
				return q, fmt.Errorf("this collection has no recorded time field")
			}
			value, e := time.Parse(time.RFC3339Nano, raw)
			if e != nil {
				return q, fmt.Errorf("%s requires RFC3339 with a timezone", name)
			}
			utc := value.UTC()
			*into = &utc
		}
	}
	if q.Since != nil && q.Until != nil && !q.Since.Before(*q.Until) {
		return q, fmt.Errorf("since must be earlier than until")
	}
	for name := range d.Filters {
		values := append([]string{}, lists[name]...)
		if value, present := options[name]; len(values) == 0 && present {
			values = []string{value}
		}
		if len(values) > 0 {
			sort.Strings(values)
			q.Filters[name] = unique(values)
		}
	}
	return q, nil
}

func sortedKeys(values map[string]string) []string {
	out := []string{}
	for k := range values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func unique(values []string) []string {
	out := []string{}
	for _, v := range values {
		if len(out) == 0 || v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
func normalize(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func Value(row any, path string) any {
	if path == "" {
		return row
	}
	for _, part := range strings.Split(path, ".") {
		m, ok := row.(map[string]any)
		if !ok {
			return nil
		}
		row = m[part]
	}
	return row
}

func String(row any, path string) string {
	v := Value(row, path)
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func matches(row any, d Definition, q Query) bool {
	for name, values := range q.Filters {
		actual := String(row, d.Filters[name])
		found := false
		for _, value := range values {
			if value == actual || value == "all" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if q.Search != "" {
		found := false
		for _, field := range d.Search {
			if strings.Contains(normalize(String(row, field)), q.Search) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if q.Since != nil || q.Until != nil {
		timestamp, e := time.Parse(time.RFC3339Nano, String(row, d.Times[q.TimeField]))
		if e != nil || q.Since != nil && timestamp.Before(*q.Since) || q.Until != nil && !timestamp.Before(*q.Until) {
			return false
		}
	}
	return true
}

func compare(a, b any) int {
	// Numeric fields (ports/priority) must not use lexical ordering.
	number := func(v any) (float64, bool) {
		switch x := v.(type) {
		case json.Number:
			n, e := x.Float64()
			return n, e == nil
		case float64:
			return x, true
		case int:
			return float64(x), true
		case int64:
			return float64(x), true
		}
		return 0, false
	}
	x, xn := number(a)
	y, yn := number(b)
	if xn && yn {
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		return 0
	}
	left, right := "", ""
	if a != nil {
		left = fmt.Sprint(a)
	}
	if b != nil {
		right = fmt.Sprint(b)
	}
	if ta, e := time.Parse(time.RFC3339Nano, left); e == nil {
		if tb, e := time.Parse(time.RFC3339Nano, right); e == nil {
			if ta.Before(tb) {
				return -1
			}
			if ta.After(tb) {
				return 1
			}
			return 0
		}
	}
	return strings.Compare(normalize(left), normalize(right))
}

func Select(rows []any, d Definition, q Query) []any {
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		if matches(row, d, q) {
			items = append(items, row)
		}
	}
	if q.Sort == "" {
		return items
	} // Preserve an owner's established order.
	key := func(row any) string {
		values := []any{}
		for _, path := range d.Keys {
			values = append(values, Value(row, path))
		}
		b, _ := json.Marshal(values)
		return string(b)
	}
	sort.SliceStable(items, func(i, j int) bool {
		order := compare(Value(items[i], d.Sorts[q.Sort]), Value(items[j], d.Sorts[q.Sort]))
		if order == 0 {
			return key(items[i]) < key(items[j])
		}
		if q.Order == "desc" {
			return order > 0
		}
		return order < 0
	})
	return items
}
