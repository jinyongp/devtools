package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
)

const (
	taskStorageVersion     = 3
	taskStorageMarker      = "task-v3"
	CheckpointMaxFrames    = 256
	CheckpointMaxTailBytes = 4 << 20
)

type taskV3Marker struct {
	StorageMarker string `json:"storage_marker"`
	Profile       string `json:"profile"`
	Head          string `json:"head"`
}

type taskV3Head struct {
	Version    int    `json:"version"`
	Profile    string `json:"profile"`
	Generation string `json:"generation"`
}

type materializedState struct {
	FormatVersion int                         `json:"format_version"`
	Profile       string                      `json:"profile"`
	Version       int                         `json:"version"`
	Revision      int                         `json:"revision"`
	WALOffset     int64                       `json:"wal_offset"`
	Items         map[string]*Item            `json:"items"`
	ItemOrders    map[string]int              `json:"item_orders"`
	Runs          map[string]*Run             `json:"runs"`
	Tracking      map[string]*DefinitionBasis `json:"tracking"`
	HistoryEvents map[int]Event               `json:"history_events"`
	HistoryRefs   map[string][]int            `json:"history_refs"`
	LastEventAt   string                      `json:"last_event_at,omitempty"`
	Checksum      string                      `json:"checksum"`
}

type v3Resolution struct {
	Marker     taskV3Marker
	Head       taskV3Head
	Generation string
	Root       string
	WAL        string
	Snapshot   string
}

func taskHeadFilename(profile string) string {
	return profilekey.Key(profile) + ".head.json"
}

func taskGenerationRoot(directory, profile string) string {
	return filepath.Join(directory, profilekey.Key(profile)+".generations")
}

func taskGenerationDir(directory, profile, generation string) string {
	return filepath.Join(taskGenerationRoot(directory, profile), generation)
}

func taskMarkerBytes(profile string) ([]byte, error) {
	if !project.ValidProfile(profile) {
		return nil, errors.New("invalid profile")
	}
	return json.Marshal(taskV3Marker{
		StorageMarker: taskStorageMarker,
		Profile:       profile,
		Head:          taskHeadFilename(profile),
	})
}

func taskHeadBytes(profile, generation string) ([]byte, error) {
	if !project.ValidProfile(profile) || !validID(generation) {
		return nil, errors.New("invalid task head")
	}
	return json.Marshal(taskV3Head{Version: taskStorageVersion, Profile: profile, Generation: generation})
}

func decodeOnePrivate(path string, value any) error {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private file required")
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected one stored document")
	}
	return nil
}

func readV3Marker(path, profile string) (taskV3Marker, bool, error) {
	var marker taskV3Marker
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return marker, false, err
	}
	defer file.Close()
	if _, err := privateTaskFileInfo(file); err != nil {
		return marker, false, err
	}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&marker); err != nil {
		return marker, false, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return marker, false, errors.New("expected one stored document")
	}
	if marker.StorageMarker == "" {
		return marker, false, nil
	}
	if marker.StorageMarker != taskStorageMarker || marker.Profile != profile || marker.Head != taskHeadFilename(profile) {
		return marker, true, errors.New("invalid task v3 marker")
	}
	return marker, true, nil
}

func resolveV3(directory, profile string) (v3Resolution, bool, error) {
	var result v3Resolution
	canonical := profilekey.CanonicalPath(directory, profile)
	marker, isV3, err := readV3Marker(canonical, profile)
	if err != nil {
		return result, false, err
	}
	if !isV3 {
		return result, false, nil
	}
	if err := validatePrivateDirectory(directory); err != nil {
		return result, true, err
	}
	headPath := filepath.Join(directory, marker.Head)
	var head taskV3Head
	if err := decodeOnePrivate(headPath, &head); err != nil {
		return result, true, err
	}
	if head.Version != taskStorageVersion || head.Profile != profile || !validID(head.Generation) {
		return result, true, errors.New("invalid task v3 head")
	}
	generations := taskGenerationRoot(directory, profile)
	if err := validatePrivateDirectory(generations); err != nil {
		return result, true, err
	}
	root := filepath.Join(generations, head.Generation)
	if err := validatePrivateDirectory(root); err != nil {
		return result, true, err
	}
	result = v3Resolution{
		Marker:     marker,
		Head:       head,
		Generation: head.Generation,
		Root:       root,
		WAL:        filepath.Join(root, "wal"),
		Snapshot:   filepath.Join(root, "snapshot.json"),
	}
	return result, true, nil
}

func validStorageComponent(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return false
	}
	return true
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private directory required")
	}
	return nil
}

func ensurePrivateDirectory(parent, name string) (string, error) {
	if !validStorageComponent(name) {
		return "", errors.New("invalid storage component")
	}
	if err := validatePrivateDirectory(parent); err != nil {
		return "", err
	}
	path := filepath.Join(parent, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0700); err != nil {
			return "", err
		}
		if err := syncPrivateDirectory(parent); err != nil {
			return "", err
		}
		info, err = os.Lstat(path)
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("private directory required")
	}
	return path, nil
}

func syncPrivateDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func createGeneration(directory, profile, generation string) (v3Resolution, error) {
	var result v3Resolution
	if !project.ValidProfile(profile) || !validID(generation) {
		return result, errors.New("invalid generation")
	}
	if err := validatePrivateDirectory(directory); err != nil {
		return result, err
	}
	generations, err := ensurePrivateDirectory(directory, profilekey.Key(profile)+".generations")
	if err != nil {
		return result, err
	}
	root := filepath.Join(generations, generation)
	if err := os.Mkdir(root, 0700); err != nil {
		return result, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(root)
			_ = syncPrivateDirectory(generations)
		}
	}()
	if err := validatePrivateDirectory(root); err != nil {
		return result, err
	}
	if err := syncPrivateDirectory(generations); err != nil {
		return result, err
	}
	if err := maintenance.Write(filepath.Join(root, "wal"), []byte{}); err != nil {
		return result, err
	}
	success = true
	result = v3Resolution{
		Marker:     taskV3Marker{StorageMarker: taskStorageMarker, Profile: profile, Head: taskHeadFilename(profile)},
		Head:       taskV3Head{Version: taskStorageVersion, Profile: profile, Generation: generation},
		Generation: generation,
		Root:       root,
		WAL:        filepath.Join(root, "wal"),
		Snapshot:   filepath.Join(root, "snapshot.json"),
	}
	return result, nil
}

func snapshotFromState(profile string, state *State, walOffset int64, lastEventAt string) materializedState {
	items := map[string]*Item{}
	itemOrders := map[string]int{}
	for id, item := range state.Items {
		raw, _ := json.Marshal(item)
		var copied Item
		_ = json.Unmarshal(raw, &copied)
		copied.Order = item.Order
		items[id] = &copied
		itemOrders[id] = item.Order
	}
	runs := map[string]*Run{}
	for id, run := range state.Runs {
		copied := *run
		runs[id] = &copied
	}
	tracking := map[string]*DefinitionBasis{}
	raw, _ := json.Marshal(state.Tracking)
	_ = json.Unmarshal(raw, &tracking)
	var historyEvents map[int]Event
	var historyRefs map[string][]int
	if state.historyComplete {
		historyEvents = map[int]Event{}
		raw, _ = json.Marshal(state.HistoryEvents)
		_ = json.Unmarshal(raw, &historyEvents)
		historyRefs = map[string][]int{}
		raw, _ = json.Marshal(state.HistoryRefs)
		_ = json.Unmarshal(raw, &historyRefs)
	}
	snapshot := materializedState{
		FormatVersion: taskStorageVersion,
		Profile:       profile,
		Version:       state.Version,
		Revision:      state.Revision,
		WALOffset:     walOffset,
		Items:         items,
		ItemOrders:    itemOrders,
		Runs:          runs,
		Tracking:      tracking,
		HistoryEvents: historyEvents,
		HistoryRefs:   historyRefs,
		LastEventAt:   lastEventAt,
	}
	snapshot.Checksum = materializedStateChecksum(snapshot)
	return snapshot
}

func materializedStateChecksum(snapshot materializedState) string {
	if snapshot.HistoryEvents == nil && snapshot.HistoryRefs == nil {
		legacy := struct {
			FormatVersion int                         `json:"format_version"`
			Profile       string                      `json:"profile"`
			Version       int                         `json:"version"`
			Revision      int                         `json:"revision"`
			WALOffset     int64                       `json:"wal_offset"`
			Items         map[string]*Item            `json:"items"`
			ItemOrders    map[string]int              `json:"item_orders"`
			Runs          map[string]*Run             `json:"runs"`
			Tracking      map[string]*DefinitionBasis `json:"tracking"`
			LastEventAt   string                      `json:"last_event_at,omitempty"`
			Checksum      string                      `json:"checksum"`
		}{
			FormatVersion: snapshot.FormatVersion,
			Profile:       snapshot.Profile,
			Version:       snapshot.Version,
			Revision:      snapshot.Revision,
			WALOffset:     snapshot.WALOffset,
			Items:         snapshot.Items,
			ItemOrders:    snapshot.ItemOrders,
			Runs:          snapshot.Runs,
			Tracking:      snapshot.Tracking,
			LastEventAt:   snapshot.LastEventAt,
		}
		raw, _ := json.Marshal(legacy)
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	snapshot.Checksum = ""
	raw, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (m materializedState) state(profile string) (*State, error) {
	historyMissing := m.HistoryEvents == nil && m.HistoryRefs == nil
	historyPartial := (m.HistoryEvents == nil) != (m.HistoryRefs == nil)
	if m.FormatVersion != taskStorageVersion || m.Profile != profile || m.Version != 1 && m.Version != JournalVersion || m.Revision < 0 || m.WALOffset < 0 || m.Items == nil || m.ItemOrders == nil || len(m.ItemOrders) != len(m.Items) || m.Runs == nil || m.Tracking == nil || historyPartial || !validDigest(m.Checksum) || m.Checksum != materializedStateChecksum(m) {
		return nil, errors.New("invalid materialized snapshot")
	}
	historyEvents := m.HistoryEvents
	historyRefs := m.HistoryRefs
	if historyMissing {
		historyEvents = map[int]Event{}
		historyRefs = map[string][]int{}
	}
	state := &State{
		Items:            m.Items,
		Runs:             m.Runs,
		Events:           []Event{},
		Revision:         m.Revision,
		Version:          m.Version,
		Tracking:         m.Tracking,
		HistoryEvents:    historyEvents,
		HistoryRefs:      historyRefs,
		historyRefCounts: map[int]int{},
		historyComplete:  !historyMissing,
	}
	for id, item := range state.Items {
		order, ok := m.ItemOrders[id]
		if item == nil || item.ID != id || !ok || order <= 0 || order > m.Revision {
			return nil, errors.New("invalid materialized item")
		}
		item.Order = order
	}
	for id, run := range state.Runs {
		if run == nil || run.ID != id {
			return nil, errors.New("invalid materialized run")
		}
	}
	if state.historyComplete {
		if err := state.rebuildHistoryCounts(); err != nil {
			return nil, err
		}
	}
	if m.LastEventAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, m.LastEventAt); err != nil {
			return nil, errors.New("invalid last event time")
		}
	}
	return state, nil
}

func writeMaterializedSnapshot(path string, snapshot materializedState) error {
	return WritePrivate(path, snapshot)
}

func readMaterializedSnapshot(path, profile string) (materializedState, *State, error) {
	var snapshot materializedState
	if err := decodeOnePrivate(path, &snapshot); err != nil {
		return snapshot, nil, err
	}
	state, err := snapshot.state(profile)
	return snapshot, state, err
}
