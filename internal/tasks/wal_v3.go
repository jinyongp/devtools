package tasks

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
)

func privateTaskFileInfo(file *os.File) (os.FileInfo, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private file required")
	}
	return info, nil
}

const (
	walFrameVersion  = 1
	walHeaderSize    = 48
	walMetaMaxBytes  = 64 << 10
	walEventMaxBytes = 4 << 20
	walRefMaxBytes   = 64 << 10
)

var walMagic = [4]byte{'D', 'T', 'V', '3'}

type walFrameMeta struct {
	Kind             string `json:"kind"`
	PreviousRevision int    `json:"previous_revision"`
	FinalRevision    int    `json:"final_revision"`
	RequestID        string `json:"request_id,omitempty"`
	Fingerprint      string `json:"fingerprint,omitempty"`
	EventCount       int    `json:"event_count"`
	HasReceipt       bool   `json:"has_receipt,omitempty"`
	ContextCount     int    `json:"context_count,omitempty"`
}

type coordinationRef struct {
	Kind          string `json:"kind"`
	Key           string `json:"key"`
	PayloadDigest string `json:"payload_digest"`
}

type walFrame struct {
	Meta        walFrameMeta
	Events      []Event
	Receipt     *coordinationRef
	Contexts    []coordinationRef
	Digest      string
	StartOffset int64
	EndOffset   int64
}

type walScan struct {
	Frames         []walFrame
	ValidOffset    int64
	IncompleteTail bool
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != stringsLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func stringsLower(value string) string {
	for _, r := range value {
		if r >= 'A' && r <= 'F' {
			return ""
		}
	}
	return value
}

func validCoordinationRef(ref coordinationRef) bool {
	if ref.Kind != "receipt" && ref.Kind != "context" || ref.Key == "" || !validDigest(ref.PayloadDigest) {
		return false
	}
	if ref.Kind == "receipt" {
		return validID(ref.Key)
	}
	return len(ref.Key) == sha256.Size*2 && validDigest(ref.Key)
}

func validateFrame(frame walFrame) error {
	meta := frame.Meta
	if meta.Kind != "mutation" && meta.Kind != "historical" && meta.Kind != "metadata" {
		return errors.New("invalid WAL frame kind")
	}
	if meta.PreviousRevision < 0 || meta.FinalRevision < meta.PreviousRevision || meta.EventCount != len(frame.Events) || meta.ContextCount != len(frame.Contexts) || meta.HasReceipt != (frame.Receipt != nil) {
		return errors.New("invalid WAL frame metadata")
	}
	if meta.FinalRevision != meta.PreviousRevision+len(frame.Events) {
		return errors.New("invalid WAL frame revision")
	}
	if meta.Kind == "metadata" && len(frame.Events) != 0 {
		return errors.New("metadata frame has events")
	}
	if meta.Kind == "mutation" {
		if !validID(meta.RequestID) || meta.Fingerprint == "" || frame.Receipt == nil {
			return errors.New("invalid mutation frame")
		}
	}
	if frame.Receipt != nil {
		if !validCoordinationRef(*frame.Receipt) || frame.Receipt.Kind != "receipt" {
			return errors.New("invalid receipt reference")
		}
		if !validID(meta.RequestID) || meta.Fingerprint == "" || frame.Receipt.Key != meta.RequestID {
			return errors.New("receipt/frame identity mismatch")
		}
	}
	for _, ref := range frame.Contexts {
		if !validCoordinationRef(ref) || ref.Kind != "context" {
			return errors.New("invalid context reference")
		}
	}
	for index, event := range frame.Events {
		if event.Sequence != meta.PreviousRevision+index+1 {
			return errors.New("non-contiguous WAL event sequence")
		}
		if meta.RequestID != "" && event.RequestID != meta.RequestID {
			return errors.New("WAL event request mismatch")
		}
	}
	return nil
}

func writeSubrecord(writer io.Writer, value any, max int) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(body) == 0 || len(body) > max {
		return errors.New("WAL subrecord exceeds limit")
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(body)))
	if _, err := writer.Write(size[:]); err != nil {
		return err
	}
	_, err = writer.Write(body)
	return err
}

func writeFrameFile(path string, frame walFrame) (string, int64, error) {
	if err := validateFrame(frame); err != nil {
		return "", 0, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return "", 0, err
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(make([]byte, walHeaderSize)); err != nil {
		return "", 0, err
	}
	bodyHash := sha256.New()
	bodyWriter := io.MultiWriter(file, bodyHash)
	if err := writeSubrecord(bodyWriter, frame.Meta, walMetaMaxBytes); err != nil {
		return "", 0, err
	}
	for _, event := range frame.Events {
		if err := writeSubrecord(bodyWriter, event, walEventMaxBytes); err != nil {
			return "", 0, err
		}
	}
	if frame.Receipt != nil {
		if err := writeSubrecord(bodyWriter, *frame.Receipt, walRefMaxBytes); err != nil {
			return "", 0, err
		}
	}
	for _, ref := range frame.Contexts {
		if err := writeSubrecord(bodyWriter, ref, walRefMaxBytes); err != nil {
			return "", 0, err
		}
	}
	end, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return "", 0, err
	}
	bodyLength := end - walHeaderSize
	header := make([]byte, walHeaderSize)
	copy(header[:4], walMagic[:])
	binary.BigEndian.PutUint16(header[4:6], walFrameVersion)
	binary.BigEndian.PutUint64(header[8:16], uint64(bodyLength))
	copy(header[16:48], bodyHash.Sum(nil))
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	if _, err := file.Write(header); err != nil {
		return "", 0, err
	}
	if err := file.Sync(); err != nil {
		return "", 0, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	frameHash := sha256.New()
	size, err := io.Copy(frameHash, file)
	if err != nil {
		return "", 0, err
	}
	if size != end {
		return "", 0, errors.New("frame size changed")
	}
	if err := file.Close(); err != nil {
		return "", 0, err
	}
	success = true
	return hex.EncodeToString(frameHash.Sum(nil)), size, nil
}

func readSubrecord(reader io.Reader, max int, value any) error {
	var size [4]byte
	if _, err := io.ReadFull(reader, size[:]); err != nil {
		return err
	}
	n := int(binary.BigEndian.Uint32(size[:]))
	if n <= 0 || n > max {
		return errors.New("invalid WAL subrecord length")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(reader, body); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid WAL subrecord")
	}
	return nil
}

func parseFrameBody(reader io.Reader) (walFrame, error) {
	var frame walFrame
	if err := readSubrecord(reader, walMetaMaxBytes, &frame.Meta); err != nil {
		return frame, err
	}
	if frame.Meta.EventCount < 0 || frame.Meta.ContextCount < 0 {
		return frame, errors.New("invalid WAL counts")
	}
	frame.Events = []Event{}
	for i := 0; i < frame.Meta.EventCount; i++ {
		var event Event
		if err := readSubrecord(reader, walEventMaxBytes, &event); err != nil {
			return frame, err
		}
		frame.Events = append(frame.Events, event)
	}
	if frame.Meta.HasReceipt {
		var ref coordinationRef
		if err := readSubrecord(reader, walRefMaxBytes, &ref); err != nil {
			return frame, err
		}
		frame.Receipt = &ref
	}
	frame.Contexts = []coordinationRef{}
	for i := 0; i < frame.Meta.ContextCount; i++ {
		var ref coordinationRef
		if err := readSubrecord(reader, walRefMaxBytes, &ref); err != nil {
			return frame, err
		}
		frame.Contexts = append(frame.Contexts, ref)
	}
	if err := validateFrame(frame); err != nil {
		return frame, err
	}
	return frame, nil
}

func scanWAL(path string, startOffset int64, collect bool) (walScan, error) {
	var result walScan
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return result, err
	}
	defer file.Close()
	info, err := privateTaskFileInfo(file)
	if err != nil {
		return result, err
	}
	if startOffset < 0 || startOffset > info.Size() {
		return result, errors.New("invalid WAL offset")
	}
	if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
		return result, err
	}
	offset := startOffset
	result.ValidOffset = startOffset
	for offset < info.Size() {
		remaining := info.Size() - offset
		if remaining < walHeaderSize {
			result.IncompleteTail = true
			return result, nil
		}
		header := make([]byte, walHeaderSize)
		if _, err := io.ReadFull(file, header); err != nil {
			return result, err
		}
		if !bytes.Equal(header[:4], walMagic[:]) || binary.BigEndian.Uint16(header[4:6]) != walFrameVersion || header[6] != 0 || header[7] != 0 {
			return result, errors.New("invalid WAL frame header")
		}
		bodyLength := int64(binary.BigEndian.Uint64(header[8:16]))
		if bodyLength <= 0 {
			return result, errors.New("invalid WAL frame length")
		}
		if bodyLength > info.Size()-(offset+walHeaderSize) {
			result.IncompleteTail = true
			return result, nil
		}
		bodyReader := io.LimitReader(file, bodyLength)
		bodyHash := sha256.New()
		frameHash := sha256.New()
		_, _ = frameHash.Write(header)
		tee := io.TeeReader(bodyReader, io.MultiWriter(bodyHash, frameHash))
		frame, err := parseFrameBody(tee)
		if err != nil {
			return result, err
		}
		trailing, err := io.Copy(io.Discard, tee)
		if err != nil {
			return result, err
		}
		if trailing != 0 {
			return result, errors.New("WAL frame has trailing body data")
		}
		if !bytes.Equal(bodyHash.Sum(nil), header[16:48]) {
			return result, errors.New("WAL checksum mismatch")
		}
		end := offset + walHeaderSize + bodyLength
		frame.Digest = hex.EncodeToString(frameHash.Sum(nil))
		frame.StartOffset = offset
		frame.EndOffset = end
		if collect {
			result.Frames = append(result.Frames, frame)
		}
		offset = end
		result.ValidOffset = end
	}
	return result, nil
}

func appendFrameFile(walPath, framePath string) (int64, error) {
	frame, err := os.OpenFile(framePath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, err
	}
	defer frame.Close()
	if _, err := privateTaskFileInfo(frame); err != nil {
		return 0, err
	}
	wal, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, err
	}
	defer wal.Close()
	if _, err := privateTaskFileInfo(wal); err != nil {
		return 0, err
	}
	size, err := io.Copy(wal, frame)
	if err == nil {
		err = wal.Sync()
	}
	return size, err
}

func truncateWAL(path string, offset int64) error {
	file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := privateTaskFileInfo(file); err != nil {
		return err
	}
	if err := file.Truncate(offset); err != nil {
		return err
	}
	return file.Sync()
}
