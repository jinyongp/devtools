package tasks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rawWALFrame(t *testing.T, meta walFrameMeta, trailing []byte) []byte {
	t.Helper()
	var body bytes.Buffer
	if err := writeSubrecord(&body, meta, walMetaMaxBytes); err != nil {
		t.Fatal(err)
	}
	body.Write(trailing)
	header := make([]byte, walHeaderSize)
	copy(header[:4], walMagic[:])
	binary.BigEndian.PutUint16(header[4:6], walFrameVersion)
	binary.BigEndian.PutUint64(header[8:16], uint64(body.Len()))
	sum := sha256.Sum256(body.Bytes())
	copy(header[16:48], sum[:])
	return append(header, body.Bytes()...)
}

func TestWALRejectsTrailingBodyData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal")
	body := rawWALFrame(t, walFrameMeta{
		Kind:             "metadata",
		PreviousRevision: 0,
		FinalRevision:    0,
	}, []byte{0x01})
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanWAL(path, 0, true); err == nil {
		t.Fatal("WAL frame with trailing body data was accepted")
	}
}

func TestWALAbsurdCountFailsWithoutPreallocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal")
	body := rawWALFrame(t, walFrameMeta{
		Kind:             "historical",
		PreviousRevision: 0,
		FinalRevision:    1 << 30,
		EventCount:       1 << 30,
	}, nil)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanWAL(path, 0, true); err == nil {
		t.Fatal("WAL frame with impossible event count was accepted")
	}
}

func TestActiveV3MutationTruncatesIncompleteTail(t *testing.T) {
	s := fixture(t)
	if _, err := s.Execute(testContext(), Request{
		Action: "task.add", Body: Object{"title": "seed"},
		Options: map[string]string{"request-id": ID()},
	}); err != nil {
		t.Fatal(err)
	}
	resolution, ok, err := resolveV3(s.Directory, s.Profile)
	if err != nil || !ok {
		t.Fatal(err)
	}
	file, err := os.OpenFile(resolution.WAL, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{'D', 'T', 'V'}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := scanWAL(resolution.WAL, 0, true)
	if err != nil || !before.IncompleteTail {
		t.Fatalf("expected incomplete tail: %#v %v", before, err)
	}
	if _, apiErr := s.Execute(testContext(), Request{
		Action: "task.add", Body: Object{"title": "after-crash"},
		Options: map[string]string{"request-id": ID()},
	}); apiErr != nil {
		t.Fatal(apiErr)
	}
	after, err := scanWAL(resolution.WAL, 0, true)
	if err != nil || after.IncompleteTail || len(after.Frames) != 2 {
		t.Fatalf("incomplete tail was not repaired: %#v %v", after, err)
	}
}

func testContext() context.Context {
	return context.Background()
}

func TestWALRejectsReceiptRequestIdentityMismatch(t *testing.T) {
	requestID := ID()
	otherID := ID()
	ref := coordinationRef{
		Kind:          "receipt",
		Key:           otherID,
		PayloadDigest: strings.Repeat("a", 64),
	}
	frame := walFrame{
		Meta: walFrameMeta{
			Kind:             "mutation",
			PreviousRevision: 0,
			FinalRevision:    0,
			RequestID:        requestID,
			Fingerprint:      "fingerprint",
			HasReceipt:       true,
		},
		Receipt: &ref,
	}
	if err := validateFrame(frame); err == nil {
		t.Fatal("WAL frame accepted receipt from a different request")
	}
}
