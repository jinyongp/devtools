// Package netops journals retry-safe local network mutations without recording
// their raw input (which can contain private OS helper parameters).
package netops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Store struct{ Data string }
type Journal struct {
	TLSReferences []string                   `json:"tls_references,omitempty"`
	Fingerprint   string                     `json:"fingerprint"`
	Phase         string                     `json:"phase"`
	Private       map[string]json.RawMessage `json:"private,omitempty"`
	Result        json.RawMessage            `json:"result,omitempty"`
	Done          bool                       `json:"done"`
	path          string
}

func (j *Journal) ReferenceTLS(listenerID string) *protocol.Error {
	for _, id := range j.TLSReferences {
		if id == listenerID {
			return nil
		}
	}
	j.TLSReferences = append(j.TLSReferences, listenerID)
	return j.Save()
}
func (s Store) PendingTLS() ([]string, *protocol.Error) {
	out := []string{}
	entries, e := os.ReadDir(filepath.Join(s.Data, "network-operations"))
	if errors.Is(e, os.ErrNotExist) {
		return out, nil
	}
	if e != nil {
		return nil, Failure("io_error", "Cannot inspect pending network references.")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !uuid.MatchString(id) {
			continue
		}
		var j Journal
		if tasks.ReadPrivate(filepath.Join(s.Data, "network-operations", entry.Name()), &j) != nil {
			return nil, Failure("invalid_storage", "Cannot inspect pending network references.")
		}
		if !j.Done && len(j.TLSReferences) > 0 {
			out = append(out, id)
		}
	}
	return out, nil
}

func Failure(code, message string) *protocol.Error {
	exit := 3
	if code == "canceled" {
		exit = 130
	}
	if code == "io_error" {
		exit = 1
	}
	if code == "invalid_argument" {
		exit = 2
	}
	return protocol.NewError(code, message, exit, nil)
}
func (j *Journal) Save() *protocol.Error {
	if err := tasks.WritePrivate(j.path, j); err != nil {
		return Failure("io_error", "Cannot persist private network operation.")
	}
	return nil
}
func (j *Journal) Set(key string, value any) *protocol.Error {
	b, e := json.Marshal(value)
	if e != nil {
		return Failure("io_error", "Cannot encode private operation state.")
	}
	if j.Private == nil {
		j.Private = map[string]json.RawMessage{}
	}
	j.Private[key] = b
	return j.Save()
}
func (j *Journal) Get(key string, value any) (bool, *protocol.Error) {
	b, ok := j.Private[key]
	if !ok {
		return false, nil
	}
	if json.Unmarshal(b, value) != nil {
		return false, Failure("invalid_storage", "Invalid private operation state.")
	}
	return true, nil
}

func (s Store) Apply(ctx context.Context, id string, input any, resources []string, fn func(*Journal) (any, *protocol.Error)) (any, *protocol.Error) {
	if !uuid.MatchString(id) {
		return nil, protocol.NewError("invalid_argument", "A request UUID is required.", 2, nil)
	}
	root := filepath.Join(s.Data, "network-operations")
	if e := tasks.PrivateDir(root); e != nil {
		return nil, Failure("io_error", "Cannot access private network operations.")
	}
	id = strings.ToLower(id)
	unlock, e := tasks.Lock(ctx, filepath.Join(root, id+".lock"))
	if e != nil {
		return nil, Failure("canceled", "Cannot acquire operation lock.")
	}
	defer unlock()
	keys := append([]string{}, resources...)
	sort.Strings(keys)
	previous := ""
	for _, key := range keys {
		if key == previous {
			continue
		}
		previous = key
		sum := sha256.Sum256([]byte(key))
		release, e := tasks.Lock(ctx, filepath.Join(root, "resource-"+hex.EncodeToString(sum[:])+".lock"))
		if e != nil {
			return nil, Failure("canceled", "Cannot acquire resource lock.")
		}
		defer release()
	}
	encoded, e := json.Marshal(input)
	if e != nil {
		return nil, Failure("invalid_argument", "Cannot encode operation input.")
	}
	digest := sha256.Sum256(encoded)
	j := Journal{path: filepath.Join(root, id+".json")}
	e = tasks.ReadPrivate(j.path, &j)
	if e == nil {
		if j.Fingerprint != hex.EncodeToString(digest[:]) {
			return nil, Failure("request_conflict", "Request UUID belongs to different input.")
		}
		if j.Done {
			var result map[string]any
			if json.Unmarshal(j.Result, &result) != nil {
				return nil, Failure("invalid_storage", "Invalid operation result.")
			}
			result["replayed"] = true
			return result, nil
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, Failure("invalid_storage", "Cannot read private operation journal.")
	} else {
		j.Fingerprint = hex.EncodeToString(digest[:])
		j.Phase = "preparing"
		if _, execution := input.(executionInput); execution {
			// Publish the intent marker with the first journal, including when
			// selection fails or the initiating CLI exits before saving a plan.
			j.Private = map[string]json.RawMessage{"execution-intent-version": json.RawMessage("1")}
		}
		if e := j.Save(); e != nil {
			return nil, e
		}
	}
	result, err := fn(&j)
	if err != nil {
		return result, err
	}
	b, e := json.Marshal(result)
	if e != nil {
		return nil, Failure("io_error", "Cannot encode operation result.")
	}
	j.Result = b
	j.Done = true
	j.Phase = "complete"
	if e := j.Save(); e != nil {
		return nil, e
	}
	var public map[string]any
	if json.Unmarshal(b, &public) != nil {
		return nil, Failure("io_error", "Invalid operation result type.")
	}
	public["replayed"] = false
	return public, nil
}
