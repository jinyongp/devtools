package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/profilekey"
)

func TestV3FirstMutationPublishesReloadableGeneration(t *testing.T) {
	s := fixture(t)
	requestID := ID()
	result, err := s.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "first"},
		Options: map[string]string{"request-id": requestID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["request_id"] != requestID {
		t.Fatalf("request id mismatch: %#v", result)
	}
	resolution, resolveErr := profilekey.Resolve(s.Directory, s.Profile, "tasks")
	if resolveErr != nil || resolution.Mode != profilekey.ModeCanonical {
		t.Fatalf("profile resolution: %#v %v", resolution, resolveErr)
	}
	v3, isV3, resolveV3Err := resolveV3(s.Directory, s.Profile)
	if resolveV3Err != nil || !isV3 {
		t.Fatalf("v3 resolution: %#v %v", v3, resolveV3Err)
	}
	scan, scanErr := scanWAL(v3.WAL, 0, true)
	if scanErr != nil || len(scan.Frames) != 1 || scan.IncompleteTail {
		t.Fatalf("WAL scan: %#v %v", scan, scanErr)
	}
	if frame := scan.Frames[0]; frame.Receipt == nil {
		t.Fatalf("first frame missing receipt: %#v", frame)
	} else {
		if len(frame.Events) != 2 || frame.Events[0].Action != "profile.upgraded" || frame.Meta.PreviousRevision != 0 || frame.Meta.FinalRevision != 2 {
			t.Fatalf("first eventful mutation was not one atomic upgrade frame: %#v", frame)
		}
		receipt, exists, readErr := readCommittedReceipt(v3, frame.Receipt.Key)
		if readErr != nil || !exists {
			t.Fatalf("receipt read: %#v %v", receipt, readErr)
		}
		if receipt.FrameDigest != frame.Digest || receipt.PayloadDigest != frame.Receipt.PayloadDigest {
			t.Fatalf("receipt/frame mismatch: %#v %#v", receipt, frame)
		}
	}
	journal, state, _, loadErr := s.loadResolved()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(journal.Events) != state.Revision || state.Revision == 0 {
		t.Fatalf("reload mismatch: events=%d revision=%d", len(journal.Events), state.Revision)
	}
	replay, replayErr := s.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "first"},
		Options: map[string]string{"request-id": requestID},
	})
	if replayErr != nil || replay["replayed"] != true {
		t.Fatalf("same request did not replay: %#v %v", replay, replayErr)
	}
	secondID := ID()
	second, secondErr := s.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "second"},
		Options: map[string]string{"request-id": secondID},
	})
	if secondErr != nil {
		current, _, resolveErr := resolveV3(s.Directory, s.Profile)
		manifest, pending, pendingErr := readPendingManifest(current)
		wal, walErr := scanWAL(current.WAL, 0, true)
		rootEntries, _ := os.ReadDir(current.Root)
		pendingEntries, _ := os.ReadDir(pendingRootPath(current))
		t.Fatalf("active v3 mutation failed: %#v %v details=%#v resolve=%v pending=%v %#v pendingErr=%v wal=%#v walErr=%v root=%v pendingEntries=%v", second, secondErr, secondErr.Details, resolveErr, pending, manifest, pendingErr, wal, walErr, entryNames(rootEntries), entryNames(pendingEntries))
	}
	v3, _, resolveV3Err = resolveV3(s.Directory, s.Profile)
	if resolveV3Err != nil {
		t.Fatal(resolveV3Err)
	}
	scan, scanErr = scanWAL(v3.WAL, 0, true)
	if scanErr != nil || len(scan.Frames) != 2 || scan.IncompleteTail {
		t.Fatalf("active WAL scan: %#v %v", scan, scanErr)
	}
	if _, err := s.Read(context.Background()); err != nil {
		t.Fatalf("active v3 reload failed: %v", err)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestPendingCoordinationRoundTrip(t *testing.T) {
	s := fixture(t)
	if err := PrivateDir(s.Directory); err != nil {
		t.Fatal(err)
	}
	resolution, err := createGeneration(s.Directory, s.Profile, ID())
	if err != nil {
		t.Fatal(err)
	}
	requestID := ID()
	frameDigest := strings.Repeat("a", 64)
	receipt := newReceiptCoordination(requestID, "fingerprint", "", Object{"changed": true}, frameDigest)
	if !receipt.valid(requestID) {
		t.Fatalf("receipt should be valid: %#v", receipt)
	}
	manifest, err := writePending(resolution, receipt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.RequestID != requestID || manifest.FrameDigest != frameDigest {
		t.Fatalf("pending manifest mismatch: %#v", manifest)
	}
	read, exists, err := readPendingManifest(resolution)
	if err != nil || !exists || read.RequestID != requestID {
		t.Fatalf("pending read mismatch: %#v %v", read, err)
	}
	if err := removePendingTree(resolution, requestID); err != nil {
		t.Fatal(err)
	}
}

func TestCommitActiveV3Direct(t *testing.T) {
	s := fixture(t)
	firstID := ID()
	first, err := s.Execute(context.Background(), Request{
		Action: "task.add", Body: Object{"title": "first"},
		Options: map[string]string{"request-id": firstID},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = first
	resolution, isV3, resolveErr := resolveV3(s.Directory, s.Profile)
	if resolveErr != nil || !isV3 {
		t.Fatalf("v3 resolution: %v", resolveErr)
	}
	_, state, _, loadErr := s.loadResolved()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	requestID := ID()
	targetID := ID()
	event := Event{
		ID:        requestID,
		Sequence:  state.Revision + 1,
		At:        stamp(),
		RequestID: requestID,
		Action:    "task.add",
		Target:    targetID,
		Data:      Object{"title": "second"},
	}
	before := state.Revision
	state.Apply(event)
	result := Object{
		"changed":           true,
		"request_id":        requestID,
		"revision":          state.Revision,
		"current_revision":  state.Revision,
		"previous_revision": before,
	}
	if err := commitActiveV3(resolution, state, []Event{event}, before, requestID, "fingerprint", "", result, nil, nil); err != nil {
		t.Fatalf("direct active commit: %T %v", err, err)
	}
}

func forceV2Storage(t *testing.T, s Store) Journal {
	t.Helper()
	journal, _, _, err := s.loadResolved()
	if err != nil {
		t.Fatal(err)
	}
	raw, marshalErr := json.Marshal(journal)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	var copy Journal
	if err := json.Unmarshal(raw, &copy); err != nil {
		t.Fatal(err)
	}
	if v3, ok, resolveErr := resolveV3(s.Directory, s.Profile); resolveErr == nil && ok {
		if err := os.Remove(filepath.Join(s.Directory, taskHeadFilename(s.Profile))); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(taskGenerationRoot(s.Directory, s.Profile)); err != nil {
			t.Fatal(err)
		}
		if err := syncPrivateDirectory(s.Directory); err != nil {
			t.Fatal(err)
		}
		_ = v3
	}
	if err := WritePrivate(s.path(), copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func TestV3ProfileEnumerationAcceptsTypedHead(t *testing.T) {
	s := fixture(t)
	if _, err := s.Execute(context.Background(), Request{
		Action: "task.add", Body: Object{"title": "enumerated"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatal(err)
	}
	profiles, err := profilekey.Enumerate(s.Directory, "tasks")
	if err != nil || len(profiles) != 1 || profiles[0] != s.Profile {
		t.Fatalf("v3 enumeration: %#v %v", profiles, err)
	}
}

func TestCreateGenerationRejectsExistingDirectory(t *testing.T) {
	s := fixture(t)
	if err := PrivateDir(s.Directory); err != nil {
		t.Fatal(err)
	}
	generation := ID()
	first, err := createGeneration(s.Directory, s.Profile, generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := WritePrivate(first.Snapshot, snapshotFromState(s.Profile, NewState(), 0, "")); err != nil {
		t.Fatal(err)
	}
	if _, err := createGeneration(s.Directory, s.Profile, generation); err == nil {
		t.Fatal("existing generation directory was reused")
	}
}

func TestResolveV3RejectsSymlinkedGenerationParent(t *testing.T) {
	s := fixture(t)
	if _, err := s.Execute(context.Background(), Request{
		Action: "task.add", Body: Object{"title": "safe"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatal(err)
	}
	resolution, ok, err := resolveV3(s.Directory, s.Profile)
	if err != nil || !ok {
		t.Fatal(err)
	}
	generations := taskGenerationRoot(s.Directory, s.Profile)
	quarantine := filepath.Join(t.TempDir(), "generations")
	if err := os.Rename(generations, quarantine); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(quarantine, generations); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := resolveV3(s.Directory, s.Profile); err == nil || !ok {
		t.Fatal("symlinked generation parent was accepted")
	}
	_ = resolution
}

func TestPendingRecoveryAcceptsPartiallyFinalizedCoordination(t *testing.T) {
	s := fixture(t)
	if _, err := s.Execute(context.Background(), Request{
		Action: "task.add", Body: Object{"title": "seed"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatal(err)
	}
	resolution, ok, err := resolveV3(s.Directory, s.Profile)
	if err != nil || !ok {
		t.Fatal(err)
	}
	_, state, _, loadErr := s.loadResolved()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	requestID := ID()
	event := Event{
		ID: ID(), Sequence: state.Revision + 1, At: stamp(), RequestID: requestID,
		Action: "task.add", Target: ID(), Data: Object{"title": "recovered"},
	}
	before := state.Revision
	state.Apply(event)
	result := Object{
		"changed": true, "request_id": requestID,
		"revision": state.Revision, "current_revision": state.Revision,
		"previous_revision": before,
	}
	receipt := newReceiptCoordination(requestID, "fingerprint", "", result, "")
	frame := buildMutationFrame([]Event{event}, before, requestID, "fingerprint", receipt, nil)
	temp := frameTempPath(resolution)
	digest, _, err := writeFrameFile(temp, frame)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(temp)
	receipt.FrameDigest = digest
	if _, err := writePending(resolution, receipt, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := appendFrameFile(resolution.WAL, temp); err != nil {
		t.Fatal(err)
	}
	manifest, exists, err := readPendingManifest(resolution)
	if err != nil || !exists {
		t.Fatal(err)
	}
	if err := finalizePendingRef(resolution, manifest.Receipt, digest); err != nil {
		t.Fatal(err)
	}
	// Crash here: receipt already moved to committed shard while pending.json remains.
	if err := recoverPendingV3(resolution); err != nil {
		t.Fatalf("partial finalize recovery failed: %v", err)
	}
	committed, exists, err := readCommittedReceipt(resolution, requestID)
	if err != nil || !exists || committed.FrameDigest != digest {
		t.Fatalf("committed receipt lost: %#v %v", committed, err)
	}
	if _, exists, err := readPendingManifest(resolution); err != nil || exists {
		t.Fatalf("pending pointer survived recovery: exists=%v err=%v", exists, err)
	}
}

func TestV3ZeroEventFirstPublicationKeepsLogicalVersionOne(t *testing.T) {
	s := fixture(t)
	requestID := ID()
	result, err := s.Execute(context.Background(), Request{
		Action:  "run.claimed",
		Options: map[string]string{"request-id": requestID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["claimed"] != false || result["changed"] != false || num(result, "revision") != 0 {
		t.Fatalf("unexpected zero-event result: %#v", result)
	}
	resolution, ok, resolveErr := resolveV3(s.Directory, s.Profile)
	if resolveErr != nil || !ok {
		t.Fatalf("v3 resolution: %v", resolveErr)
	}
	scan, scanErr := scanWAL(resolution.WAL, 0, true)
	if scanErr != nil || len(scan.Frames) != 1 {
		t.Fatalf("zero-event WAL: %#v %v", scan, scanErr)
	}
	frame := scan.Frames[0]
	if frame.Meta.EventCount != 0 || frame.Meta.PreviousRevision != 0 || frame.Meta.FinalRevision != 0 || frame.Meta.RequestID != requestID {
		t.Fatalf("zero-event frame changed revision: %#v", frame)
	}
	state, readErr := s.Read(context.Background())
	if readErr != nil || state.Version != 1 || state.Revision != 0 {
		t.Fatalf("zero-event publication changed logical state: %#v %v", state, readErr)
	}
	replay, replayErr := s.Execute(context.Background(), Request{
		Action:  "run.claimed",
		Options: map[string]string{"request-id": requestID},
	})
	if replayErr != nil || replay["replayed"] != true || replay["claimed"] != false {
		t.Fatalf("zero-event receipt did not replay: %#v %v", replay, replayErr)
	}
}

func TestV2ReceiptReplayDoesNotMigrateStorage(t *testing.T) {
	s := fixture(t)
	requestID := ID()
	request := Request{
		Action:  "task.add",
		Body:    Object{"title": "stable"},
		Options: map[string]string{"request-id": requestID},
	}
	first, err := s.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	forceV2Storage(t, s)
	if _, ok, err := resolveV3(s.Directory, s.Profile); err != nil || ok {
		t.Fatalf("fixture is not v2: ok=%v err=%v", ok, err)
	}
	replay, replayErr := s.Execute(context.Background(), request)
	if replayErr != nil || replay["replayed"] != true || num(replay, "revision") != num(first, "revision") {
		t.Fatalf("v2 receipt replay failed: %#v %v", replay, replayErr)
	}
	if _, ok, err := resolveV3(s.Directory, s.Profile); err != nil || ok {
		t.Fatalf("receipt replay migrated storage: ok=%v err=%v", ok, err)
	}
}

func TestV2NoChangeDoesNotMigrateStorage(t *testing.T) {
	s := fixture(t)
	created := call(t, s, "task.add", "", Object{"title": "same"})
	id := itemID(created)
	journal := forceV2Storage(t, s)
	request := Request{
		Action: "task.update",
		Target: id,
		Body:   Object{"title": "same"},
		Options: map[string]string{
			"request-id":  ID(),
			"if-revision": fmt.Sprint(len(journal.Events)),
		},
	}
	if _, err := s.Execute(context.Background(), request); err == nil || err.Code != "no_change" {
		t.Fatalf("expected no_change, got %v", err)
	}
	if _, ok, err := resolveV3(s.Directory, s.Profile); err != nil || ok {
		t.Fatalf("no-change request migrated storage: ok=%v err=%v", ok, err)
	}
}

func TestV2MutationMigratesWithTriggerFrame(t *testing.T) {
	s := fixture(t)
	created := call(t, s, "task.add", "", Object{"title": "before"})
	id := itemID(created)
	journal := forceV2Storage(t, s)
	beforeRevision := len(journal.Events)
	requestID := ID()
	result, err := s.Execute(context.Background(), Request{
		Action: "task.update",
		Target: id,
		Body:   Object{"title": "after"},
		Options: map[string]string{
			"request-id":  requestID,
			"if-revision": fmt.Sprint(beforeRevision),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution, ok, resolveErr := resolveV3(s.Directory, s.Profile)
	if resolveErr != nil || !ok {
		t.Fatalf("migration did not publish v3: %v", resolveErr)
	}
	scan, scanErr := scanWAL(resolution.WAL, 0, true)
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	var trigger *walFrame
	for index := range scan.Frames {
		if scan.Frames[index].Meta.RequestID == requestID {
			trigger = &scan.Frames[index]
			break
		}
	}
	if trigger == nil || trigger.Meta.Kind != "mutation" || trigger.Meta.PreviousRevision != beforeRevision || trigger.Meta.FinalRevision != num(result, "revision") || trigger.Receipt == nil {
		t.Fatalf("triggering mutation not atomic in migrated generation: %#v", trigger)
	}
	receipt, exists, receiptErr := readCommittedReceipt(resolution, requestID)
	if receiptErr != nil || !exists || receipt.FrameDigest != trigger.Digest {
		t.Fatalf("trigger receipt not durable: %#v %v", receipt, receiptErr)
	}
}

func TestActiveV3MutationRepairsInvalidSnapshotOffset(t *testing.T) {
	s := fixture(t)
	if _, err := s.Execute(context.Background(), Request{
		Action: "task.add", Body: Object{"title": "seed"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatal(err)
	}
	resolution, ok, err := resolveV3(s.Directory, s.Profile)
	if err != nil || !ok {
		t.Fatal(err)
	}
	snapshot, _, err := readMaterializedSnapshot(resolution.Snapshot, s.Profile)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(resolution.WAL)
	if err != nil {
		t.Fatal(err)
	}
	beforeOffset := beforeInfo.Size()
	snapshot.WALOffset = 1 << 40
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, apiErr := s.Execute(context.Background(), Request{
		Action: "task.add", Body: Object{"title": "repair"},
		Options: map[string]string{"request-id": ID()},
	}); apiErr != nil {
		t.Fatalf("snapshot corruption blocked valid mutation: %v", apiErr)
	}
	repaired, _, err := readMaterializedSnapshot(resolution.Snapshot, s.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.WALOffset != beforeOffset {
		t.Fatalf("snapshot was not repaired before the new mutation: offset=%d want=%d", repaired.WALOffset, beforeOffset)
	}
	current, currentErr := loadCurrentV3(resolution)
	if currentErr != nil || len(current.Items) != 2 {
		t.Fatalf("repaired snapshot plus bounded tail lost current state: items=%d err=%v", len(current.Items), currentErr)
	}
}

func TestCurrentV3LoaderUsesMaterializedSnapshotAndTail(t *testing.T) {
	s := fixture(t)
	if _, err := s.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "seed"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatal(err)
	}
	resolution, ok, err := resolveV3(s.Directory, s.Profile)
	if err != nil || !ok {
		t.Fatal(err)
	}
	_, fullState, _, loadErr := s.loadResolved()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	for index := 0; index < 5; index++ {
		requestID := ID()
		event := Event{
			ID:        ID(),
			Sequence:  fullState.Revision + 1,
			At:        stamp(),
			RequestID: requestID,
			Action:    "task.add",
			Target:    ID(),
			Data:      Object{"title": fmt.Sprintf("tail-%d", index)},
		}
		before := fullState.Revision
		fullState.Apply(event)
		result := Object{
			"changed":           true,
			"request_id":        requestID,
			"revision":          fullState.Revision,
			"current_revision":  fullState.Revision,
			"previous_revision": before,
		}
		if err := commitActiveV3(resolution, fullState, []Event{event}, before, requestID, "fingerprint-"+requestID, "", result, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	current, currentErr := loadCurrentV3(resolution)
	if currentErr != nil {
		t.Fatal(currentErr)
	}
	_, expected, loadErr := s.loadV3(resolution)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if current.Revision != expected.Revision || len(current.Items) != len(expected.Items) || hash(current.Tracking) != hash(expected.Tracking) {
		t.Fatalf("current projection mismatch: current=%d/%d expected=%d/%d", current.Revision, len(current.Items), expected.Revision, len(expected.Items))
	}
	if len(current.Events) == current.Revision || len(current.Events) != 5 {
		t.Fatalf("current loader replayed full history: events=%d revision=%d", len(current.Events), current.Revision)
	}
}

func TestCurrentV3LoaderFallsBackWithoutRepairingCorruptSnapshot(t *testing.T) {
	s := fixture(t)
	if _, err := s.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "seed"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatal(err)
	}
	resolution, ok, err := resolveV3(s.Directory, s.Profile)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolution.Snapshot, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	state, currentErr := loadCurrentV3(resolution)
	if currentErr != nil || state.Revision == 0 || len(state.Items) != 1 {
		t.Fatalf("WAL fallback failed: %#v %v", state, currentErr)
	}
	var snapshot materializedState
	if err := ReadPrivate(resolution.Snapshot, &snapshot); err == nil {
		t.Fatal("read-only current loader repaired corrupt snapshot")
	}
}

func TestCurrentV3LoaderFallsBackWhenSnapshotContentChanges(t *testing.T) {
	s := fixture(t)
	if _, err := s.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "seed"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatal(err)
	}
	resolution, ok, err := resolveV3(s.Directory, s.Profile)
	if err != nil || !ok {
		t.Fatal(err)
	}
	snapshot, _, err := readMaterializedSnapshot(resolution.Snapshot, s.Profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snapshot.Items {
		item.Title = "tampered"
		break
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshot); err != nil {
		t.Fatal(err)
	}
	state, currentErr := loadCurrentV3(resolution)
	if currentErr != nil {
		t.Fatal(currentErr)
	}
	for _, item := range state.Items {
		if item.Title != "seed" {
			t.Fatalf("current loader trusted modified snapshot content: %q", item.Title)
		}
	}
}

func TestV3RetryRepairsInvalidMaterializedSnapshot(t *testing.T) {
	s := fixture(t)
	request := Request{
		Action:  "task.add",
		Body:    Object{"title": "seed"},
		Options: map[string]string{"request-id": ID()},
	}
	if _, err := s.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	resolution, ok, err := resolveV3(s.Directory, s.Profile)
	if err != nil || !ok {
		t.Fatal(err)
	}
	snapshot, _, err := readMaterializedSnapshot(resolution.Snapshot, s.Profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snapshot.Items {
		item.Title = "tampered"
		break
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshot); err != nil {
		t.Fatal(err)
	}
	replayed, retryErr := s.Execute(context.Background(), request)
	if retryErr != nil || replayed["replayed"] != true {
		t.Fatalf("retry did not replay from WAL state: %#v %v", replayed, retryErr)
	}
	repaired, state, repairErr := readMaterializedSnapshot(resolution.Snapshot, s.Profile)
	if repairErr != nil {
		t.Fatalf("retry did not repair invalid snapshot: %#v %v", repaired, repairErr)
	}
	if state.Revision == 0 {
		t.Fatal("repaired snapshot lost current revision")
	}
	for _, item := range state.Items {
		if item.Title != "seed" {
			t.Fatalf("repaired snapshot retained tampered state: %q", item.Title)
		}
	}
}

func TestActiveV3MutationDoesNotReplayUnrelatedReceiptHistory(t *testing.T) {
	s := fixture(t)
	oldRequestID := ID()
	oldResult, err := s.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "old"},
		Options: map[string]string{"request-id": oldRequestID},
	})
	if err != nil {
		t.Fatal(err)
	}
	oldID := str(objectValue(oldResult["item"]), "id")
	resolution, ok, resolveErr := resolveV3(s.Directory, s.Profile)
	if resolveErr != nil || !ok {
		t.Fatal(resolveErr)
	}
	path, pathErr := receiptPath(resolution, oldRequestID)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, fullErr := s.loadResolved(); fullErr == nil {
		t.Fatal("corrupt historical receipt did not break full coordination load")
	}
	historyState, historyErr := s.Read(context.Background())
	if historyErr != nil || historyState.Revision == 0 || historyState.Items[oldID] == nil {
		t.Fatalf("event-history read depended on receipt sidecar: %#v %v", historyState, historyErr)
	}
	history, historyQueryErr := s.Query(context.Background(), Query{Command: "history", Target: oldID})
	if historyQueryErr != nil || len(history["items"].([]any)) == 0 {
		t.Fatalf("history query depended on receipt sidecar: %#v %v", history, historyQueryErr)
	}

	newResult, mutationErr := s.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "new"},
		Options: map[string]string{"request-id": ID()},
	})
	if mutationErr != nil {
		t.Fatalf("routine v3 mutation replayed unrelated receipt history: %v", mutationErr)
	}
	newID := str(objectValue(newResult["item"]), "id")
	if newID == "" || newID == oldID {
		t.Fatalf("unexpected new mutation result: %#v", newResult)
	}
	current, currentErr := s.Query(context.Background(), Query{Command: "list", Options: map[string]string{"scope": "all"}})
	if currentErr != nil {
		t.Fatalf("current query replayed unrelated receipt history: %v", currentErr)
	}
	items := current["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("current state lost data: %#v", items)
	}
}
