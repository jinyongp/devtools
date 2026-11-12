package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecutionAdmissionIsNotReusableAfterRelease(t *testing.T) {
	root := privateTempDir(t)
	ctx, release, err := AcquireExecution(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, childRelease, err := AcquireExecution(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	childRelease()
	release()
	exclusive, err := AcquireExclusiveExecution(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer exclusive()
	expiring, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, unexpected, err := AcquireExecution(expiring, root)
	if unexpected != nil {
		unexpected()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("released execution context bypassed exclusive admission: %v", err)
	}
}
