package diagnostics

import "testing"

func TestWSLPrivateStorageRejectsDrvFSAndNetworkFilesystems(t *testing.T) {
	for _, kind := range []uint64{0x01021997, 0x5346544e, 0xff534d42, 0x6969} {
		name, supported, known := linuxFilesystem(kind)
		if supported || !known || name == "unknown" {
			t.Fatal(kind, name, supported, known)
		}
	}
	name, supported, known := linuxFilesystem(0xef53)
	if name != "ext" || !supported || !known {
		t.Fatal(name, supported, known)
	}
}
