package tasks

import (
	"context"
	"iter"
	"maps"

	"github.com/jinyongp/devtools/internal/idref"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (s *State) resolveReference(ctx context.Context, kind, input string) (string, *protocol.Error) {
	return idref.Resolve(ctx, idref.UUID, input, s.referenceKeys(kind))
}

func (s *State) referenceKeys(kind string) iter.Seq[string] {
	return func(yield func(string) bool) {
		if kind == "run" {
			for id := range s.Runs {
				if !yield(id) {
					return
				}
			}
			return
		}
		for id, item := range s.Items {
			if item.Kind == kind && !yield(id) {
				return
			}
		}
	}
}

// ReferenceKind identifies CLI field selectors. Machine request bodies keep
// their exact ID contract.
func ReferenceKind(field, targetKind string) string {
	switch field {
	case "workstream_id":
		return "workstream"
	case "task_id", "task_ids":
		return "task"
	case "validation_ids":
		return "validation"
	case "depends_on":
		return targetKind
	case "basis_id":
		return "validation-basis"
	case "record_id", "validation_record_ids":
		return "validation-record"
	default:
		return ""
	}
}

func (r Request) hasAbbreviatedReferences(def *Definition) bool {
	if r.Target != "" && !validID(r.Target) || r.Options["workstream"] != "" && !validID(r.Options["workstream"]) {
		return true
	}
	if r.CLIReferenceFields {
		for field := range r.Body {
			if ReferenceKind(field, def.Kind) == "" {
				continue
			}
			if value := str(r.Body, field); value != "" && !validID(value) {
				return true
			}
			for _, value := range arr(r.Body, field) {
				if !validID(value) {
					return true
				}
			}
		}
	}
	return false
}

func (s *State) resolveRequestReferences(ctx context.Context, def *Definition, r *Request) *protocol.Error {
	var condition *protocol.Error
	if r.Target != "" {
		r.Target, condition = s.resolveReference(ctx, def.Kind, r.Target)
		if condition != nil {
			return condition
		}
	}
	if input := r.Options["workstream"]; input != "" && !validID(input) {
		id, condition := s.resolveReference(ctx, "workstream", input)
		if condition != nil {
			return condition
		}
		r.Options = maps.Clone(r.Options)
		r.Options["workstream"] = id
	}
	if r.CLIReferenceFields {
		for _, field := range def.Fields {
			kind := ReferenceKind(field, def.Kind)
			if kind == "" {
				continue
			}
			keys := s.referenceKeys(kind)
			if kind == "validation-basis" || kind == "validation-record" {
				keys = s.evidenceReferenceKeys(kind, r.Target)
			}
			if input := str(r.Body, field); input != "" && !validID(input) {
				id, condition := idref.Resolve(ctx, idref.UUID, input, keys)
				if condition != nil {
					return condition
				}
				r.Body[field] = id
			}
			if inputs := arr(r.Body, field); len(inputs) != 0 {
				ids, condition := idref.ResolveMany(ctx, idref.UUID, inputs, keys)
				if condition != nil {
					return condition
				}
				values := make([]any, 0, len(ids))
				for _, id := range ids {
					values = append(values, id)
				}
				r.Body[field] = values
			}
		}
	}
	if e := validateBody(def, r.Body); e != nil {
		return inputGuidance(*r, e)
	}
	return nil
}

func (s *State) evidenceReferenceKeys(kind, target string) iter.Seq[string] {
	return func(yield func(string) bool) {
		if kind == "validation-basis" {
			if v := s.Items[target]; v != nil {
				bases, _ := v.Props["bases"].(map[string]any)
				for id := range bases {
					if !yield(id) {
						return
					}
				}
			}
			return
		}
		owner := s.Items[target]
		for id, v := range s.Items {
			if v.Kind != "validation" || owner != nil && owner.Kind == "validation" && id != target {
				continue
			}
			for _, record := range objects(v.Props, "records") {
				if !yield(str(record, "record_id")) {
					return
				}
			}
		}
	}
}
