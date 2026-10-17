//go:build !windows

package truststore

import (
	"context"
	"crypto/x509"
	"errors"
)

func (s System) windows(context.Context, Request, string, *x509.Certificate) error {
	return errors.New("unsupported trust store")
}
