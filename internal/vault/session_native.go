package vault

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

// Native APIs can wait inside OS UI or IPC without observing a Go context. A
// short-lived same-binary child bounds that wait; cancellation kills the child
// instead of leaving the daemon's key/KDF mutex permanently occupied.
type sessionNativeProtector struct {
	keyfile     KeyFile
	interactive bool
}
type nativeUnlockRequest struct {
	KeyFile     KeyFile `json:"key_file"`
	Interactive bool    `json:"interactive"`
}
type nativeUnlockResponse struct {
	Key   []byte `json:"key,omitempty"`
	Error string `json:"error,omitempty"`
}

func (p sessionNativeProtector) Identity() ProtectionIdentity {
	return ProtectionIdentity{Backend: p.keyfile.Backend, VaultID: p.keyfile.VaultID}
}
func (p sessionNativeProtector) Wrap(context.Context, []byte) ([]byte, error) { return nil, ErrInvalid }
func (p sessionNativeProtector) Unwrap(ctx context.Context, b []byte) ([]byte, error) {
	if !bytes.Equal(b, p.keyfile.Wrapped) || p.keyfile.Backend == "passphrase" {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		return nil, ErrUnavailable
	}
	var input bytes.Buffer
	if err = writeSessionFrame(&input, nativeUnlockRequest{KeyFile: p.keyfile, Interactive: p.interactive}); err != nil {
		return nil, err
	}
	defer clear(input.Bytes())
	cmd := exec.CommandContext(ctx, exe, "__vault-native-unwrap")
	cmd.Env = sessionDaemonEnvironment(os.Environ())
	cmd.Stdin = &input
	cmd.Stderr = io.Discard
	output := &boundedSessionOutput{body: make([]byte, 0, maxSessionFrame+4)}
	cmd.Stdout = output
	cmd.WaitDelay = time.Second
	defer func() { clear(output.body) }()
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	var response nativeUnlockResponse
	r := bytes.NewReader(output.body)
	if err = readSessionFrame(r, &response); err != nil || r.Len() != 0 {
		clear(response.Key)
		return nil, ErrIntegrity
	}
	if err = sessionError(response.Error); err != nil {
		clear(response.Key)
		return nil, err
	}
	if len(response.Key) != 32 {
		clear(response.Key)
		return nil, ErrIntegrity
	}
	return response.Key, nil
}

type boundedSessionOutput struct{ body []byte }

func (w *boundedSessionOutput) Write(b []byte) (int, error) {
	if len(b) > maxSessionFrame+4-len(w.body) {
		return 0, ErrTooLarge
	}
	w.body = append(w.body, b...)
	return len(b), nil
}

// NativeUnlockHelper uses only anonymous inherited pipes. Its protocol carries
// an already protected key slot in and an owned root key out; no shell, project
// configuration, environment credential, or temporary plaintext file is used.
func NativeUnlockHelper(ctx context.Context, in io.Reader, out io.Writer) error {
	var request nativeUnlockRequest
	if err := readSessionFrame(in, &request); err != nil {
		return err
	}
	k := request.KeyFile
	if err := k.validate(); err != nil || k.Backend == "passphrase" {
		return ErrInvalid
	}
	p, err := nativeProtector(k.Backend, k.VaultID, request.Interactive)
	var response nativeUnlockResponse
	if err == nil {
		var key *Material
		key, err = k.Unlock(ctx, k.Backend, p)
		if err == nil {
			response.Key = key.CopyBytes()
			key.Destroy()
			defer clear(response.Key)
		}
	}
	response.Error = sessionCode(err)
	return writeSessionFrame(out, response)
}
