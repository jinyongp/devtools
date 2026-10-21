package vault

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnerPeerTransportRootSeparationAndOwnedListener(t *testing.T) {
	root := filepath.Join(privateTempDir(t), strings.Repeat("long", 35))
	runner, instance := "01234567-89ab-4cde-8fab-0123456789ab", "01234567-89ab-4cde-8fab-0123456789ac"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l, err := ListenPeer(ctx, root, runner, instance)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	a, err := PeerAddress(root, runner, instance)
	if err != nil {
		t.Fatal(err)
	}
	b, err := PeerAddress(root+"-other", runner, instance)
	if err != nil || a == b {
		t.Fatal("transport roots aliased", err)
	}
	if _, err = ListenPeer(ctx, root, runner, instance); err == nil {
		t.Fatal("duplicate instance stole listener")
	}
	done := make(chan error, 1)
	go func() {
		c, err := l.Accept(ctx)
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		var b [1]byte
		_, err = io.ReadFull(c, b[:])
		if err == nil {
			_, err = c.Write(b[:])
		}
		done <- err
	}()
	c, err := DialPeer(ctx, root, runner, instance)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	if _, err = c.Write([]byte{7}); err != nil {
		t.Fatal(err)
	}
	var received [1]byte
	if _, err = io.ReadFull(c, received[:]); err != nil || received[0] != 7 {
		t.Fatal("same-user exchange failed", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := ListenPeer(ctx, root, runner, instance)
	if err != nil {
		t.Fatal("owned cleanup prevented restart", err)
	}
	replacement.Close()
	if _, err = PeerAddress(root, strings.ToUpper(runner), instance); err == nil {
		t.Fatal("noncanonical runner UUID accepted")
	}
}
