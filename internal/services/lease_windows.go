package services

import (
	"errors"
	"os"
)

func (l *lockedLease) unlockClose() error {
	if l == nil {
		return nil
	}
	return l.closeReference()
}
func tryLease(path string, create bool) (*lockedLease, bool, error) {
	return nil, false, errors.ErrUnsupported
}
func adoptInheritedLease(file *os.File, path string) (*lockedLease, error) {
	return nil, errors.ErrUnsupported
}
