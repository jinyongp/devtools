package vault

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func TestCanonicalContractVector(t *testing.T) {
	e := NewEncoder("x")
	e.String("a")
	e.Uint(7)
	e.Bool(true)
	got, err := e.Finish()
	// Magic, length-prefixed domain, length-prefixed string, uint64, boolean.
	want, _ := hex.DecodeString("4454563100000001780000000161000000000000000701")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("canonical contract mismatch: %x, %v", got, err)
	}
}

func TestCanonicalBoundariesCannotBeConfused(t *testing.T) {
	encode := func(domain, a, b string, present bool) []byte {
		e := NewEncoder(domain)
		e.String(a)
		e.String(b)
		e.Bool(present)
		out, err := e.Finish()
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	base := encode("slot", "a", "bc", true)
	for _, candidate := range [][]byte{
		encode("slot", "ab", "c", true),
		encode("manifest", "a", "bc", true),
		encode("slot", "a", "bc", false),
	} {
		if bytes.Equal(base, candidate) {
			t.Fatal("authentication contexts collided")
		}
	}
	if bytes.Equal(encode("slot", "", "", false), encode("slot", "", "", true)) {
		t.Fatal("absent and explicitly empty slot collided")
	}
}

func TestCanonicalRejectsInvalidTextAndDocumentOverflow(t *testing.T) {
	for _, domain := range []string{"", string([]byte{0xff})} {
		if out, err := NewEncoder(domain).Finish(); out != nil || !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid domain accepted")
		}
	}
	e := NewEncoder("slot")
	e.String(string([]byte{0xff}))
	if out, err := e.Finish(); out != nil || !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid UTF-8 accepted")
	}
	// A full-size field cannot fit together with the format/domain/length framing.
	e = NewEncoder("slot")
	e.Blob(make([]byte, MaxDocumentBytes))
	if out, err := e.Finish(); out != nil || !errors.Is(err, ErrTooLarge) {
		t.Fatal("oversized document accepted")
	}
}
