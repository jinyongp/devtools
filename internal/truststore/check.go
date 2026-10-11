package truststore

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"os"
	"os/exec"
	"time"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/protocol"
)

type TrustStatus struct {
	Known   bool `json:"known"`
	Trusted bool `json:"trusted"`
}

// CheckPublic is called in a fresh process so Go's cached system pool cannot
// report the pre-untrust state after the OS bundle has been reconciled.
func CheckPublic(certificate []byte) TrustStatus {
	cert, e := decode(certificate)
	if e != nil {
		return TrustStatus{}
	}
	if _, e := x509.SystemCertPool(); e != nil {
		return TrustStatus{}
	}
	_, e = cert.Verify(x509.VerifyOptions{CurrentTime: cert.NotBefore.Add(time.Hour), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	return TrustStatus{Known: true, Trusted: e == nil}
}
func Check(ctx context.Context, authority *ca.Authority) (TrustStatus, *protocol.Error) {
	path, e := os.Executable()
	if e != nil {
		return TrustStatus{}, protocol.NewError("trust_unknown", "Cannot check the system trust store.", 3, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "__trust-check")
	cmd.Stdin = bytes.NewReader(authority.PEM())
	var output bytes.Buffer
	cmd.Stdout = &output
	if cmd.Run() != nil {
		return TrustStatus{}, protocol.NewError("trust_unknown", "Cannot check the system trust store.", 3, nil)
	}
	var status TrustStatus
	if json.Unmarshal(output.Bytes(), &status) != nil || !status.Known {
		return status, protocol.NewError("trust_unknown", "Cannot check the system trust store.", 3, nil)
	}
	return status, nil
}
