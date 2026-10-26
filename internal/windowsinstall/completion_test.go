package windowsinstall

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func utf16Profile(text string, order binary.ByteOrder) []byte {
	units := append([]uint16{0xfeff}, utf16.Encode([]rune(text))...)
	result := make([]byte, len(units)*2)
	for i, unit := range units {
		order.PutUint16(result[i*2:], unit)
	}
	return result
}

func TestCompletionProfileAppendPreservesEncodingAndExistingBytes(t *testing.T) {
	for name, original := range map[string][]byte{
		"empty":    nil,
		"UTF8":     []byte("# 한글\nSet-Alias mine Get-Date"),
		"UTF8 BOM": append([]byte{0xef, 0xbb, 0xbf}, []byte("# 한글\r\n")...),
		"ANSI":     {'#', ' ', 0xe9, '\r', '\n'},
		"UTF16 LE": utf16Profile("# 한글\r\n", binary.LittleEndian),
		"UTF16 BE": utf16Profile("# 한글\n", binary.BigEndian),
	} {
		t.Run(name, func(t *testing.T) {
			before := bytes.Clone(original)
			suffix, err := completionProfileAppend(original)
			if err != nil || len(suffix) == 0 {
				t.Fatal("missing loader", err)
			}
			if !bytes.Equal(original, before) {
				t.Fatal("existing profile changed")
			}
			combined := append(bytes.Clone(original), suffix...)
			again, err := completionProfileAppend(combined)
			if err != nil || len(again) != 0 {
				t.Fatal("registration duplicated", err)
			}
		})
	}
}

func TestCompletionProfileRefusesAmbiguousOrSignedEdits(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(completionBegin + "\nuser-edited loader\n" + completionEnd),
		[]byte(completionBegin),
		[]byte(completionEnd),
		[]byte("# SIG # Begin signature block\nsignature\n"),
		{0xff, 0xfe, 0x61},
		{0xff, 0xfe, 0, 0, '#', 0, 0, 0},
		{0, 0, 0xfe, 0xff, 0, 0, 0, '#'},
	} {
		if _, err := completionProfileAppend(body); err == nil {
			t.Fatalf("ambiguous/signed profile accepted: %q", body)
		}
	}
	// The loader remains recognized when the rest of the file uses mixed lines.
	if suffix, err := completionProfileAppend([]byte("# custom\r\n" + completionLoader)); err != nil || len(suffix) != 0 {
		t.Fatal("mixed newlines duplicated registration", err)
	}
	if strings.ContainsAny(completionLoader, "·—•→") {
		t.Fatal("decorative separators in generated loader")
	}
}
