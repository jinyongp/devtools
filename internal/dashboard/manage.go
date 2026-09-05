package dashboard

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

type actionRequest struct {
	Domain  string            `json:"domain"`
	Profile string            `json:"profile"`
	Action  string            `json:"action"`
	Target  string            `json:"target"`
	Body    tasks.Object      `json:"body"`
	Options map[string]string `json:"options"`
	Change  *values.Change    `json:"change,omitempty"`
	Process *services.Request `json:"process,omitempty"`
}

func apiError(w http.ResponseWriter, e *protocol.Error) {
	status := 400
	if e.Code == "revision_conflict" || e.Code == "request_conflict" || e.Code == "claim_conflict" {
		status = 409
	}
	if e.ExitCode == 1 {
		status = 500
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = protocol.Failure(w, e)
}
func invalidAction(w http.ResponseWriter) {
	apiError(w, protocol.NewError("invalid_argument", "Provide a valid structured action request.", 2, nil))
}
func (s *Server) valueStore(profile string) values.Store {
	return values.Store{Directory: filepath.Join(filepath.Dir(s.data), "profiles"), Profile: profile}
}
func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	kind, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		invalidAction(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var req actionRequest
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || !project.ValidProfile(req.Profile) {
		invalidAction(w)
		return
	}
	var result any
	var err *protocol.Error
	switch req.Domain {
	case "process":
		if req.Process == nil || req.Change != nil || req.Action != "" || req.Target != "" || len(req.Body) > 0 || len(req.Options) > 0 {
			invalidAction(w)
			return
		}
		manager := services.Store{Data: filepath.Dir(s.data)}
		if req.Process.Action == "start" {
			p, e := project.Resolve(req.Process.Directory, "")
			if e != nil {
				apiError(w, e)
				return
			}
			if p.Profile != req.Profile {
				invalidAction(w)
				return
			}
		} else {
			record, e := manager.Status(r.Context(), req.Process.ID)
			if e != nil {
				apiError(w, e)
				return
			}
			if record.Profile != req.Profile {
				invalidAction(w)
				return
			}
		}
		result, err = manager.Apply(r.Context(), *req.Process)
	case "values":
		if req.Process != nil || req.Change == nil || req.Action != "" || req.Target != "" || len(req.Body) > 0 || len(req.Options) > 0 {
			invalidAction(w)
			return
		}
		result, err = s.valueStore(req.Profile).Apply(r.Context(), *req.Change)
	case "task":
		if req.Process != nil || req.Change != nil || tasks.Find(req.Action) == nil || req.Options["request-id"] == "" || req.Options["if-revision"] == "" {
			invalidAction(w)
			return
		}
		for k := range req.Options {
			switch k {
			case "request-id", "if-revision", "context", "expected-run", "workstream", "dir":
			default:
				invalidAction(w)
				return
			}
		}
		result, err = (tasks.Store{Directory: s.data, Profile: req.Profile, Cache: s.cache}).Execute(r.Context(), tasks.Request{Action: req.Action, Target: req.Target, Body: req.Body, Options: req.Options})
	default:
		invalidAction(w)
		return
	}
	if err != nil {
		apiError(w, err)
		return
	}
	s.mu.Lock()
	s.results = nil
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = protocol.Success(w, result)
}
