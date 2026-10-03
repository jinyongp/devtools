package tasks

import (
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	stdhash "hash"
)

// json.Encoder writes the same canonical value as json.Marshal followed by a
// newline. Hold its final byte so the checksum keeps its existing contract,
// without allocating another copy of the entire encoded document.
type jsonHashWriter struct {
	stdhash.Hash
	last    byte
	pending bool
}

func (w *jsonHashWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.pending {
		w.Hash.Write([]byte{w.last})
	}
	w.Hash.Write(p[:len(p)-1])
	w.last, w.pending = p[len(p)-1], true
	return len(p), nil
}

func streamJSONHash(value any) string {
	w := &jsonHashWriter{Hash: sha256.New()}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		w.Reset()
	}
	return hex.EncodeToString(w.Sum(nil))
}

// A workstream's common document bodies precede all other fields in the
// canonical task-definition JSON. Hash that identical prefix once per
// assessment pass, then resume it for each task. Existing signatures stay
// byte-for-byte identical, including escaping, epochs and field ordering.
func commonDefinitionHasher(common Object) func(Object) string {
	raw, err := json.Marshal(common)
	if err != nil {
		panic(err)
	}
	prefix := sha256.New()
	prefix.Write([]byte(`{"common":`))
	prefix.Write(raw)
	prefix.Write([]byte(","))
	saved, err := prefix.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		panic(err)
	}
	return func(definition Object) string {
		// The common field was already hashed. The remaining definition always
		// contains self; replace its opening brace with the saved prefix.
		delete(definition, "common")
		raw, err := json.Marshal(definition)
		if err != nil {
			panic(err)
		}
		digest := sha256.New()
		if err := digest.(encoding.BinaryUnmarshaler).UnmarshalBinary(saved); err != nil {
			panic(err)
		}
		digest.Write(raw[1:])
		return hex.EncodeToString(digest.Sum(nil))
	}
}
