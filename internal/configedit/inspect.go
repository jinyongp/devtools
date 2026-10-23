package configedit

import (
	"path/filepath"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/projectfile"
	"github.com/jinyongp/devtools/internal/protocol"
)

func Inspect(path string) (map[string]any, *protocol.Error) {
	s, e := projectfile.Read(path)
	if e != nil {
		return nil, e
	}
	d, e := project.ParseDocument(s.Bytes, path, filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	document := d.Raw
	if d.Format == "legacy" {
		document, e = migration(d)
		if e != nil {
			return nil, e
		}
	}
	return map[string]any{"config_path": path, "revision": s.Revision, "format": d.Format, "document": document}, nil
}

func Check(path string) (map[string]any, *protocol.Error) {
	s, e := projectfile.Read(path)
	if e != nil {
		return nil, e
	}
	d, e := project.ParseDocument(s.Bytes, path, filepath.Dir(path))
	report := map[string]any{"config_path": path, "revision": s.Revision, "format": "unknown", "valid": e == nil, "diagnostics": []any{}}
	if e == nil {
		report["format"] = d.Format
		return report, nil
	}
	if format, ok := e.Details["format"].(string); ok {
		report["format"] = format
	}
	e.Message = "Project configuration is invalid. Correct the reported fields and expected input rules."
	diagnostics := []any{}
	if causes, ok := e.Details["causes"].([]any); ok {
		for _, cause := range causes {
			c := cause.(map[string]any)
			message := e.Message
			if specific, ok := c["message"].(string); ok {
				message = specific
			}
			diagnostics = append(diagnostics, map[string]any{"code": c["code"], "reason": c["reason"], "message": message, "target": c["target"], "sources": c["sources"], "facts": c["facts"], "remedies": []any{}})
		}
	}
	if len(diagnostics) == 0 {
		sources := e.Details["sources"]
		if sources == nil {
			sources = []any{}
		}
		diagnostics = append(diagnostics, map[string]any{"code": e.Code, "reason": e.Details["reason"], "message": e.Message, "target": map[string]any{"kind": "config", "name": project.Filename, "config_path": path}, "sources": sources, "facts": map[string]any{}, "remedies": []any{}})
	}
	report["diagnostics"] = diagnostics
	e.Details["report"] = report
	return report, e
}
