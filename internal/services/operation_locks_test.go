package services

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestIndependentOperationsAndSingletonExclusion(t *testing.T) {
	for _, sameProfile := range []bool{false, true} {
		t.Run(map[bool]string{false: "other-profile", true: "other-instance"}[sameProfile], func(t *testing.T) {
			s := Store{Data: privateTempDir(t)}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			makeProject := func(profile string) string {
				dir := privateTempDir(t)
				if e := fixtureWriteFile(filepath.Join(dir, "devtools.toml"), []byte("profile='"+profile+"'\n[commands.web]\nexec=['unused']\n"), 0600); e != nil {
					t.Fatal(e)
				}
				return dir
			}
			a := makeProject("app")
			profile := "other"
			if sameProfile {
				profile = "app"
			}
			b := makeProject(profile)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			releaseA := func() { once.Do(func() { close(release) }) }
			defer releaseA()
			aDone := make(chan *protocol.Error, 1)
			q := Request{Action: "start", Directory: a, Command: "web", RequestID: tasks.ID(), BeforeStart: func(context.Context) *protocol.Error {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return failure("requirements_failed")
			}}
			go func() { _, e := s.Apply(ctx, q); aDone <- e }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("A never entered", ctx.Err())
			}
			// B must finish while A is still inside its callback, not merely
			// after a shorter shutdown timeout or a guessed wall-clock threshold.
			bDone := make(chan *protocol.Error, 1)
			go func() {
				_, e := s.Apply(ctx, Request{Action: "start", Directory: b, Command: "web", RequestID: tasks.ID(), BeforeStart: func(context.Context) *protocol.Error { return failure("requirements_failed") }})
				bDone <- e
			}()
			select {
			case e := <-bDone:
				if e == nil || e.Code != "requirements_failed" {
					t.Fatal("independent callback failed", e)
				}
			case <-ctx.Done():
				t.Fatal("independent operation waited for A", ctx.Err())
			}
			select {
			case e := <-aDone:
				t.Fatal("A was not held", e)
			default:
			}
			// Both same-request and new-request/same-singleton contenders must
			// time out without reaching preflight or writing a new receipt.
			for _, sameRequest := range []bool{false, true} {
				id := tasks.ID()
				if sameRequest {
					id = q.RequestID
				}
				waitCtx, stop := context.WithTimeout(ctx, 80*time.Millisecond)
				_, e := s.Apply(waitCtx, Request{Action: "start", Directory: a, Command: "web", RequestID: id, BeforeStart: func(context.Context) *protocol.Error {
					t.Error("contender bypassed owner")
					return failure("requirements_failed")
				}})
				stop()
				if e == nil || e.Code != "deadline_exceeded" {
					t.Fatal("contender did not preserve cancellation", e)
				}
				if !sameRequest {
					if _, e := os.Stat(filepath.Join(s.root(), "receipts", id+".json")); !os.IsNotExist(e) {
						t.Fatal("canceled contender wrote receipt", e)
					}
				}
			}
			waitCtx, stop := context.WithTimeout(ctx, 80*time.Millisecond)
			unlock, e := s.Lock(waitCtx)
			stop()
			if e == nil {
				unlock()
				t.Fatal("maintenance bypassed active operation")
			}
			releaseA()
			select {
			case <-aDone:
			case <-ctx.Done():
				t.Fatal("A failed to release")
			}
			unlock, e = s.Lock(ctx)
			if e != nil {
				t.Fatal("maintenance remained blocked", e)
			}
			unlock()
		})
	}
}

func TestPublicationLockProtectsLiveStagingFromCleanup(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	release, e := s.publicationLock(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	path := filepath.Join(s.root(), launchStagingName(tasks.ID())+"live")
	if e := fixtureMkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if e := s.cleanupLaunchStagingContext(ctx); e == nil || e.Code != "deadline_exceeded" {
		t.Fatal("cleanup bypassed publication", e)
	}
	if _, e := os.Stat(path); e != nil {
		t.Fatal("live stage removed", e)
	}
	release()
	release = nil
	if e := s.cleanupLaunchStaging(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("orphan stage retained", e)
	}
}
