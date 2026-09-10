package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"

	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/jinyongp/devtools/internal/backup"
	"github.com/jinyongp/devtools/internal/cleanup"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
	"os"
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
	Cleanup *cleanupRequest   `json:"cleanup,omitempty"`
	Backup  *backupRequest    `json:"backup,omitempty"`
}
type cleanupRequest struct {
	Action    string   `json:"action"`
	Plan      string   `json:"plan"`
	IDs       []string `json:"ids"`
	ID        string   `json:"id"`
	RequestID string   `json:"request_id"`
}
type backupRequest struct {
	Action    string `json:"action"`
	File      string `json:"file"`
	Identity  string `json:"identity_file"`
	Source    string `json:"source_profile"`
	Replace   bool   `json:"replace"`
	Digest    string `json:"digest"`
	RequestID string `json:"request_id"`
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Dashboard sessions manage definitions and interventions; agents own execution.
func managementTaskAction(action string) bool {
	switch action {
	case "task.add", "task.update", "task.depends", "task.cancel", "task.reopen",
		"workstream.create", "workstream.update", "workstream.depends", "workstream.cancel", "workstream.reopen",
		"workstream.activate", "workstream.close", "workstream.edited", "spec.set", "plan.set", "run.revoked":
		return true
	}
	return false
}

func (s *Server) cleanupEngine() cleanup.Engine {
	d, _ := paths.Current()
	return cleanup.Engine{Data: filepath.Dir(s.data), Cache: filepath.Dir(filepath.Dir(s.cache)), Config: d.Config}
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
	if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || (!project.ValidProfile(req.Profile) && !(req.Domain == "cleanup" && req.Profile == "")) {
		invalidAction(w)
		return
	}
	if req.Cleanup != nil && req.Domain != "cleanup" || req.Backup != nil && req.Domain != "backup" {
		invalidAction(w)
		return
	}
	var result any
	var err *protocol.Error
	switch req.Domain {
	case "cleanup", "backup":
		if req.Process != nil || req.Change != nil || req.Action != "" || req.Target != "" || len(req.Body) > 0 || len(req.Options) > 0 {
			invalidAction(w)
			return
		}
		if req.Domain == "cleanup" {
			if req.Cleanup == nil {
				invalidAction(w)
				return
			}
			q := req.Cleanup
			engine := s.cleanupEngine()
			switch q.Action {
			case "apply":
				result, err = engine.Apply(r.Context(), q.Plan, q.IDs, q.RequestID)
			case "restore":
				result, err = engine.Restore(r.Context(), q.ID)
			case "purge":
				result, err = engine.Purge(r.Context(), q.ID)
			default:
				invalidAction(w)
				return
			}
		} else {
			if req.Backup == nil {
				invalidAction(w)
				return
			}
			result, err = s.backupAction(r.Context(), req.Profile, *req.Backup)
		}
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
		if req.Process.Action == "check" {
			if req.Process.Directory != "" || req.Process.Command != "" || req.Process.Env != nil || req.Process.Capture != nil {
				invalidAction(w)
				return
			}
			result, err = manager.Check(r.Context(), req.Process.ID)
		} else {
			result, err = manager.Apply(r.Context(), *req.Process)
		}
	case "values":
		if req.Process != nil || req.Change == nil || req.Action != "" || req.Target != "" || len(req.Body) > 0 || len(req.Options) > 0 {
			invalidAction(w)
			return
		}
		result, err = s.valueStore(req.Profile).Apply(r.Context(), *req.Change)
	case "task":
		preview := req.Action == "workstream.edited" && req.Options["dry-run"] == "true"
		if req.Process != nil || req.Change != nil || !managementTaskAction(req.Action) || (!preview && req.Options["request-id"] == "") || req.Options["if-revision"] == "" {
			invalidAction(w)
			return
		}
		for k := range req.Options {
			switch k {
			case "request-id", "if-revision", "expected-run":
			case "dry-run":
				if req.Action != "workstream.edited" || (req.Options[k] != "true" && req.Options[k] != "false") {
					invalidAction(w)
					return
				}
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

// Create uses a request-specific output and durable intent so uncertain HTTP
// responses can be retried without producing additional encrypted files.
func (s *Server) backupAction(ctx context.Context, profile string, q backupRequest) (any, *protocol.Error) {
	c := s.cleanupEngine()
	engine := backup.Engine{Data: c.Data, Config: c.Config, Cache: c.Cache}
	if q.Action == "restore" {
		if (q.Digest != "") != (q.RequestID != "") {
			return nil, protocol.NewError("invalid_argument", "Apply requires digest and request ID.", 2, nil)
		}
		source := q.Source
		if source == "" {
			source = profile
		}
		return engine.Restore(ctx, q.File, q.Identity, source, profile, q.Digest, q.RequestID, q.Replace)
	}
	if q.Action != "create" || !uuidPattern.MatchString(q.RequestID) || q.File != "" || q.Identity != "" || q.Source != "" || q.Digest != "" || q.Replace {
		return nil, protocol.NewError("invalid_argument", "Provide a backup action and request UUID.", 2, nil)
	}
	unlock, e := tasks.Lock(ctx, filepath.Join(c.Data, "backup-http.lock"))
	if e != nil {
		return nil, protocol.NewError("storage_error", "Cannot coordinate backup.", 1, nil)
	}
	defer unlock()
	var config backup.Config
	if tasks.ReadPrivate(filepath.Join(c.Config, "backup.json"), &config) != nil || !filepath.IsAbs(config.Directory) {
		return nil, protocol.NewError("backup_not_configured", "Configure backup location and recipient with devtools backup configure.", 3, nil)
	}
	path := filepath.Join(config.Directory, "devtools-"+q.RequestID+".age")
	intent := filepath.Join(c.Data, "backup-http", q.RequestID+".json")
	b, _ := json.Marshal(map[string]string{"profile": profile, "path": path})
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(b))
	var previous struct {
		Fingerprint string `json:"fingerprint"`
		Completed   bool   `json:"completed"`
	}
	complete := func() error {
		b, _ := json.Marshal(map[string]any{"fingerprint": fingerprint, "completed": true})
		return maintenance.Write(intent, b)
	}
	if e = tasks.ReadPrivate(intent, &previous); e == nil {
		if previous.Fingerprint != fingerprint {
			return nil, protocol.NewError("request_conflict", "Request ID belongs to another backup.", 3, nil)
		}
		if previous.Completed {
			return map[string]any{"path": path, "replayed": true}, nil
		}
		if _, e = maintenance.Read(path, 128<<20); e == nil {
			if complete() != nil {
				return nil, protocol.NewError("storage_error", "Cannot store backup result.", 1, nil)
			}
			return map[string]any{"path": path, "replayed": true}, nil
		} else if !errors.Is(e, os.ErrNotExist) {
			return nil, protocol.NewError("backup_error", "Cannot read backup output.", 1, nil)
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, protocol.NewError("storage_error", "Cannot read backup intent.", 1, nil)
	} else {
		if _, e = os.Lstat(path); !errors.Is(e, os.ErrNotExist) {
			return nil, protocol.NewError("request_conflict", "Backup output already exists.", 3, nil)
		}
	}
	raw, _ := json.Marshal(map[string]string{"fingerprint": fingerprint})
	if maintenance.Write(intent, raw) != nil {
		return nil, protocol.NewError("storage_error", "Cannot store backup intent.", 1, nil)
	}
	if os.MkdirAll(config.Directory, 0700) != nil {
		return nil, protocol.NewError("backup_error", "Cannot prepare backup directory.", 1, nil)
	}
	if _, err := engine.Create(ctx, profile, path, ""); err != nil {
		return nil, err
	}
	if complete() != nil {
		return nil, protocol.NewError("storage_error", "Cannot store backup result.", 1, nil)
	}
	return map[string]any{"path": path, "replayed": false}, nil
}
