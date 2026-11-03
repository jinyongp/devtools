package retainedbody

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRetainedBodySharesExactBytesAndRejectsChangedReference(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "private")}
	body := bytes.Repeat([]byte("immutable original result\n"), 5000)
	a, e := s.Put(context.Background(), "process_record", body)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Put(context.Background(), "process_record", body)
	if e != nil || a != b {
		t.Fatal(a, b, e)
	}
	entries, e := os.ReadDir(s.directory())
	if e != nil || len(entries) != 1 {
		t.Fatal(len(entries), e)
	}
	info, e := os.Stat(filepath.Join(s.directory(), a.Digest))
	if e != nil || info.Size() >= int64(len(body)) {
		t.Fatal(info, e)
	}
	actual, e := s.Read(a)
	if e != nil || !bytes.Equal(actual, body) {
		t.Fatal(e)
	}
	a.Kind = "archive_payload"
	if _, e := s.Read(a); e == nil {
		t.Fatal("changed owner kind accepted")
	}
}
