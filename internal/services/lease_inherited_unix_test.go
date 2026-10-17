//go:build !windows

package services

import (
	"context"

	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestLeaseReferenceSurvivesParentClose(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	id := tasks.ID()
	record := Record{ID: id, Profile: "app", Directory: privateTempDir(t), Command: "web", CreatedAt: time.Now().UTC(), State: "starting"}
	lease, err := s.prepareLaunch(record)
	if err != nil {
		t.Fatal(err)
	}
	dupFD, dupErr := syscall.Dup(int(lease.file.Fd()))
	if dupErr != nil {
		_ = lease.unlockClose()
		t.Fatal(dupErr)
	}
	childReference := os.NewFile(uintptr(dupFD), "inherited-lease")
	if childReference == nil {
		_ = lease.unlockClose()
		t.Fatal("failed to duplicate lease fd")
	}
	if err := lease.closeReference(); err != nil {
		_ = childReference.Close()
		t.Fatal(err)
	}
	available, availabilityErr := s.leaseAvailable(context.Background(), id, false)
	if availabilityErr != nil || available {
		_ = childReference.Close()
		t.Fatalf("lease became free after parent close: available=%v err=%v", available, availabilityErr)
	}
	if err := childReference.Close(); err != nil {
		t.Fatal(err)
	}
	available, availabilityErr = s.leaseAvailable(context.Background(), id, false)
	if availabilityErr != nil || !available {
		t.Fatalf("lease did not release after final reference: available=%v err=%v", available, availabilityErr)
	}
}

func TestAdoptInheritedLeaseValidatesIdentityAndSetsCloseOnExec(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	id := tasks.ID()
	record := Record{ID: id, Profile: "app", Directory: privateTempDir(t), Command: "web", CreatedAt: time.Now().UTC(), State: "starting"}
	parent, launchErr := s.prepareLaunch(record)
	if launchErr != nil {
		t.Fatal(launchErr)
	}
	dupFD, dupErr := syscall.Dup(int(parent.file.Fd()))
	if dupErr != nil {
		_ = parent.unlockClose()
		t.Fatal(dupErr)
	}
	child := os.NewFile(uintptr(dupFD), "lease-child")
	if child == nil {
		_ = parent.unlockClose()
		t.Fatal("failed to wrap inherited lease")
	}
	adopted, adoptErr := adoptInheritedLease(child, s.path(id, "lease.lock"))
	if adoptErr != nil {
		_ = parent.unlockClose()
		_ = child.Close()
		t.Fatal(adoptErr)
	}
	if err := parent.closeReference(); err != nil {
		_ = adopted.unlockClose()
		t.Fatal(err)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, adopted.file.Fd(), syscall.F_GETFD, 0)
	if errno != 0 {
		_ = adopted.unlockClose()
		t.Fatalf("fcntl(F_GETFD): %v", errno)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		_ = adopted.unlockClose()
		t.Fatal("adopted lease fd is not close-on-exec")
	}
	available, availabilityErr := s.leaseAvailable(context.Background(), id, false)
	if availabilityErr != nil || available {
		_ = adopted.unlockClose()
		t.Fatalf("adopted lease did not retain lock: available=%v err=%v", available, availabilityErr)
	}

	wrongPath := filepath.Join(privateTempDir(t), "lease.lock")
	if err := os.WriteFile(wrongPath, []byte{}, 0600); err != nil {
		_ = adopted.unlockClose()
		t.Fatal(err)
	}
	wrong, openErr := os.OpenFile(wrongPath, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if openErr != nil {
		_ = adopted.unlockClose()
		t.Fatal(openErr)
	}
	if _, err := adoptInheritedLease(wrong, s.path(id, "lease.lock")); err == nil {
		_ = wrong.Close()
		_ = adopted.unlockClose()
		t.Fatal("mismatched inherited fd was accepted")
	}
	_ = wrong.Close()

	if err := adopted.unlockClose(); err != nil {
		t.Fatal(err)
	}
	available, availabilityErr = s.leaseAvailable(context.Background(), id, false)
	if availabilityErr != nil || !available {
		t.Fatalf("adopted lease did not release: available=%v err=%v", available, availabilityErr)
	}
}
