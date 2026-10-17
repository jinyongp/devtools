//go:build !windows

package services

import (
	"errors"
	"os"
	"path/filepath"

	"syscall"

	"github.com/jinyongp/devtools/internal/tasks"
)

func (l *lockedLease) unlockClose() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func tryLease(path string, create bool) (*lockedLease, bool, error) {
	flags := os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		flags |= os.O_CREATE
		if err := tasks.PrivateDir(filepath.Dir(path)); err != nil {
			return nil, false, err
		}
	}
	file, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, false, err
	}
	if !privateRegular(file) {
		_ = file.Close()
		return nil, false, errors.New("private lease required")
	}
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return &lockedLease{file: file}, true, nil
	}
	_ = file.Close()
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EINTR) {
		return nil, false, nil
	}
	return nil, false, err
}

func adoptInheritedLease(file *os.File, path string) (*lockedLease, error) {
	if err := validateLeaseIdentity(file, path); err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.New("inherited lease unavailable")
	}
	syscall.CloseOnExec(int(file.Fd()))
	return &lockedLease{file: file}, nil
}
