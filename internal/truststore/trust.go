// Package truststore installs and removes only the selected devtools CA.
package truststore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/privilege"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Request struct {
	Action      string `json:"action"`
	Certificate []byte `json:"certificate"`
}
type Manager struct {
	Isolated bool
	Terminal bool
	Run      privilege.Runner
}

func (m Manager) Apply(ctx context.Context, action string, authority *ca.Authority) *protocol.Error {
	if m.Isolated {
		return protocol.NewError("isolated_operation_not_allowed", "OS trust changes are unavailable in isolated mode. Export the public CA for a client instead.", 3, nil)
	}
	if action != "trust" && action != "untrust" {
		return protocol.NewError("invalid_argument", "Unknown trust operation.", 2, nil)
	}
	run := m.Run
	if run == nil {
		run = privilege.Run
	}
	return run(ctx, "trust", Request{Action: action, Certificate: authority.PEM()}, m.Terminal)
}

func Trusted(authority *ca.Authority) bool {
	_, e := authority.Certificate.Verify(x509.VerifyOptions{KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	return e == nil
}

// System is injectable for tests; the helper command always uses its zero value
// so untrusted stdin cannot select paths, executables, or keychains.
type System struct {
	GOOS      string
	Directory string
	Tool      string
	Run       func(context.Context, string, ...string) ([]byte, error)
}

func (s System) execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	if s.Run != nil {
		return s.Run(ctx, name, args...)
	}
	command := exec.CommandContext(ctx, name, args...)
	return command.CombinedOutput()
}

func decode(data []byte) (*x509.Certificate, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("one public certificate is required")
	}
	cert, e := x509.ParseCertificate(block.Bytes)
	if e != nil || !cert.IsCA || cert.CheckSignatureFrom(cert) != nil {
		return nil, errors.New("valid self-signed CA required")
	}
	return cert, nil
}

func (s System) Apply(ctx context.Context, request Request) error {
	if request.Action != "trust" && request.Action != "untrust" {
		return errors.New("invalid trust action")
	}
	cert, e := decode(request.Certificate)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(sum[:])
	goos := s.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "darwin" {
		return s.darwin(ctx, request, fingerprint)
	}
	if goos != "linux" {
		return errors.New("unsupported trust store")
	}
	directory, tool := s.Directory, s.Tool
	if directory == "" {
		if _, e := os.Stat("/usr/sbin/update-ca-certificates"); e == nil {
			directory = "/usr/local/share/ca-certificates"
			tool = "/usr/sbin/update-ca-certificates"
		} else if _, e := os.Stat("/usr/bin/update-ca-trust"); e == nil {
			directory = "/etc/pki/ca-trust/source/anchors"
			tool = "/usr/bin/update-ca-trust"
		} else {
			return errors.New("supported CA store updater is unavailable")
		}
	}
	info, e := os.Lstat(directory)
	if e != nil || !info.IsDir() {
		return errors.New("trust directory is unavailable or a symlink")
	}
	path := filepath.Join(directory, "devtools-"+fingerprint+".crt")
	data, e := readRegular(path)
	if e == nil {
		existing, e := decode(data)
		if e != nil || !bytes.Equal(existing.Raw, cert.Raw) {
			return errors.New("owned CA path contains a different certificate")
		}
		if request.Action == "untrust" {
			if e := os.Remove(path); e != nil {
				return e
			}
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	} else if request.Action == "trust" {
		file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0644)
		if e != nil {
			return e
		}
		_, e = file.Write(request.Certificate)
		if e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	// Always reconcile the trust bundle, including a retry after response loss
	// between the owned file mutation and the previous updater invocation.
	args := []string{}
	if strings.HasSuffix(tool, "update-ca-trust") {
		args = []string{"extract"}
	}
	_, e = s.execute(ctx, tool, args...)
	return e
}

func readRegular(path string) ([]byte, error) {
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil || !i.Mode().IsRegular() || i.Size() > 1<<20 {
		return nil, errors.New("regular CA certificate file required")
	}
	return io.ReadAll(io.LimitReader(f, 1<<20))
}

func (s System) darwin(ctx context.Context, request Request, fingerprint string) error {
	keychain := "/Library/Keychains/System.keychain"
	if request.Action == "untrust" {
		output, e := s.execute(ctx, "/usr/bin/security", "find-certificate", "-a", "-Z", keychain)
		if e != nil {
			return e
		}
		if !strings.Contains(strings.ToLower(string(output)), fingerprint) {
			return nil
		}
	}
	directory, e := os.MkdirTemp("/private/tmp", "devtools-trust-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "ca.crt")
	if e := os.WriteFile(path, request.Certificate, 0600); e != nil {
		return e
	}
	if request.Action == "trust" {
		_, e = s.execute(ctx, "/usr/bin/security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", keychain, path)
		return e
	}
	if _, e = s.execute(ctx, "/usr/bin/security", "remove-trusted-cert", "-d", path); e != nil {
		return e
	}
	_, e = s.execute(ctx, "/usr/bin/security", "delete-certificate", "-Z", fingerprint, keychain)
	return e
}
