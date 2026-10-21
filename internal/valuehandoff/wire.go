package valuehandoff

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/vault"
)

const maxControlFrame = 128 << 10
const maxPayloadFrame = 16 << 20

type RunnerAddress struct {
	RootID            string `json:"root_id"`
	VaultID           string `json:"vault_id"`
	RunnerID          string `json:"runner_id"`
	InstanceID        string `json:"instance_id"`
	Endpoint          string `json:"endpoint"`
	Protocol          int    `json:"protocol"`
	RedactionProtocol int    `json:"redaction_protocol"`
}

// CanonicalRoot is shared by CLI control and receiver construction so Windows
// case aliases and Unix symlink aliases cannot compute different pipe identities.
func CanonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if errors.Is(err, os.ErrNotExist) {
		return "", vault.ErrKeyMissing
	}
	if err != nil {
		return "", err
	}
	guard, err := platformfs.OpenRoot(abs, false)
	if err != nil {
		return "", err
	}
	guard.Close()
	return abs, nil
}

func Address(root, runner, instance string) (RunnerAddress, error) {
	if !uuidRE.MatchString(runner) || !uuidRE.MatchString(instance) {
		return RunnerAddress{}, vault.ErrInvalid
	}
	abs, err := CanonicalRoot(root)
	if err != nil {
		return RunnerAddress{}, err
	}
	k, err := (vault.KeyFileStore{Directory: filepath.Join(abs, "vault")}).Read()
	if err != nil {
		return RunnerAddress{}, err
	}
	endpoint, err := vault.PeerAddress(abs, runner, instance)
	if err != nil {
		return RunnerAddress{}, err
	}
	h := sha256.Sum256([]byte(abs))
	return RunnerAddress{hex.EncodeToString(h[:]), k.VaultID, runner, instance, endpoint, 1, 1}, nil
}

type helloFrame struct {
	Protocol        int    `json:"protocol"`
	Type            string `json:"type"`
	RootID          string `json:"root_id"`
	VaultID         string `json:"vault_id"`
	RunnerID        string `json:"runner_id"`
	InstanceID      string `json:"instance_id"`
	ServerChallenge string `json:"server_challenge"`
}
type bindFrame struct {
	Protocol            int    `json:"protocol"`
	Type                string `json:"type"`
	RootID              string `json:"root_id"`
	VaultID             string `json:"vault_id"`
	RunnerID            string `json:"runner_id"`
	InstanceID          string `json:"instance_id"`
	ServerChallenge     string `json:"server_challenge"`
	ClientChallenge     string `json:"client_challenge"`
	ExecutionID         string `json:"execution_id"`
	InputID             string `json:"input_id"`
	ExternalExecutionID string `json:"external_execution_id"`
}
type probeFrame struct {
	Protocol        int    `json:"protocol"`
	Type            string `json:"type"`
	RootID          string `json:"root_id"`
	VaultID         string `json:"vault_id"`
	RunnerID        string `json:"runner_id"`
	InstanceID      string `json:"instance_id"`
	ServerChallenge string `json:"server_challenge"`
	ClientChallenge string `json:"client_challenge"`
}
type readyFrame struct {
	Protocol          int    `json:"protocol"`
	Type              string `json:"type"`
	ServerChallenge   string `json:"server_challenge"`
	ClientChallenge   string `json:"client_challenge"`
	RedactionProtocol int    `json:"redaction_protocol"`
}
type acceptedFrame struct {
	Protocol          int    `json:"protocol"`
	Type              string `json:"type"`
	ServerChallenge   string `json:"server_challenge"`
	ClientChallenge   string `json:"client_challenge"`
	ExecutionID       string `json:"execution_id"`
	InputID           string `json:"input_id"`
	DeliveryID        string `json:"delivery_id"`
	RedactionProtocol int    `json:"redaction_protocol"`
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func writeFrame(w io.Writer, value any, limit int) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	defer clear(b)
	return writeFrameBytes(w, b, limit)
}
func writeFrameBytes(w io.Writer, b []byte, limit int) error {
	if len(b) == 0 || len(b) > limit {
		return vault.ErrTooLarge
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(b)))
	if err := writeAll(w, length[:]); err != nil {
		return err
	}
	return writeAll(w, b)
}
func readFrameBytes(r io.Reader, limit int) ([]byte, error) {
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(length[:])
	if n == 0 || uint64(n) > uint64(limit) {
		return nil, vault.ErrTooLarge
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		clear(b)
		return nil, err
	}
	return b, nil
}
func readControl(r io.Reader, out any) error {
	b, err := readFrameBytes(r, maxControlFrame)
	if err != nil {
		return err
	}
	defer clear(b)
	return DecodeControl(b, out)
}
func connectRunner(ctx context.Context, root string, address RunnerAddress) (vault.PeerConnection, helloFrame, string, error) {
	c, err := vault.DialPeer(ctx, root, address.RunnerID, address.InstanceID)
	if err != nil {
		return nil, helloFrame{}, "", err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}
	if err = c.SetDeadline(deadline); err != nil {
		c.Close()
		return nil, helloFrame{}, "", err
	}
	var h helloFrame
	if err = readControl(c, &h); err != nil {
		c.Close()
		return nil, h, "", err
	}
	if h.Protocol != 1 || h.Type != "hello" || h.RootID != address.RootID || h.VaultID != address.VaultID || h.RunnerID != address.RunnerID || h.InstanceID != address.InstanceID || !idRE.MatchString(h.ServerChallenge) {
		c.Close()
		return nil, h, "", vault.ErrInvalid
	}
	challenge, err := vault.NewID()
	if err != nil {
		c.Close()
		return nil, h, "", err
	}
	return c, h, challenge, nil
}
func probeRunner(ctx context.Context, root string, address RunnerAddress) error {
	c, h, challenge, err := connectRunner(ctx, root, address)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	p := probeFrame{1, "probe", address.RootID, address.VaultID, address.RunnerID, address.InstanceID, h.ServerChallenge, challenge}
	if err = writeFrame(c, p, maxControlFrame); err != nil {
		return err
	}
	var response readyFrame
	if err = readControl(c, &response); err != nil {
		return err
	}
	if response.Protocol != 1 || response.Type != "registered" || response.ServerChallenge != h.ServerChallenge || response.ClientChallenge != challenge || response.RedactionProtocol != 1 {
		return vault.ErrInvalid
	}
	return nil
}
