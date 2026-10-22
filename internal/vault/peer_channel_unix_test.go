//go:build linux || darwin

package vault

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunnerDialValidatesActualEndpoint(t *testing.T) {
	for _, condition := range []string{"private", "public parent", "public socket", "socket symlink", "parent symlink", "not a socket", "missing socket"} {
		t.Run(condition, func(t *testing.T) {
			root := privateTempDir(t)
			const runner, instance = "01234567-89ab-4cde-8fab-0123456789ab", "01234567-89ab-4cde-8fab-0123456789ac"
			address, err := PeerAddress(root, runner, instance)
			if err != nil {
				t.Fatal(err)
			}
			parent := filepath.Dir(address)
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			listener.SetUnlinkOnClose(false)
			t.Cleanup(func() {
				listener.Close()
				os.Remove(address)
				os.Remove(address + ".original")
				os.Remove(parent)
			})
			if err := os.Chmod(address, 0600); err != nil {
				t.Fatal(err)
			}
			switch condition {
			case "public parent":
				err = os.Chmod(parent, 0755)
			case "public socket":
				err = os.Chmod(address, 0666)
			case "socket symlink":
				if err = os.Rename(address, address+".original"); err == nil {
					err = os.Symlink(address+".original", address)
				}
			case "parent symlink":
				realParent := filepath.Join(root, "real-parent")
				if err = os.Rename(parent, realParent); err == nil {
					err = os.Symlink(realParent, parent)
				}
			case "not a socket", "missing socket":
				if err = os.Remove(address); err == nil && condition == "not a socket" {
					err = os.WriteFile(address, nil, 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if condition != "private" {
				// A legacy helper-created tree must not make an unsafe actual
				// endpoint acceptable, even though old DialPeer checked only it.
				directory, _ := peerDirectory(root, runner, instance)
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			connection, err := DialPeer(ctx, root, runner, instance)
			if connection != nil {
				connection.Close()
			}
			if condition == "private" {
				if err != nil {
					t.Fatal("public endpoint alone was insufficient", err)
				}
			} else if !errors.Is(err, ErrRecovery) {
				t.Fatal("unsafe endpoint accepted", err)
			}
			if condition == "private" {
				if _, err := os.Lstat(filepath.Join(root, "value-runner-transports")); !os.IsNotExist(err) {
					t.Fatal("dial prepared an internal transport tree", err)
				}
			}
		})
	}
}

func TestRunnerListenerPreservesExternalEndpoint(t *testing.T) {
	for _, condition := range []string{"live", "stale", "file", "symlink", "public parent", "public socket"} {
		t.Run(condition, func(t *testing.T) {
			root := privateTempDir(t)
			const runner, instance = "01234567-89ab-4cde-8fab-0123456789ab", "01234567-89ab-4cde-8fab-0123456789ac"
			address, err := PeerAddress(root, runner, instance)
			if err != nil {
				t.Fatal(err)
			}
			parent := filepath.Dir(address)
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			listener.SetUnlinkOnClose(false)
			t.Cleanup(func() {
				listener.Close()
				os.Remove(address)
				os.Remove(address + ".original")
				os.Remove(parent)
			})
			if err := os.Chmod(address, 0600); err != nil {
				t.Fatal(err)
			}
			switch condition {
			case "stale":
				err = listener.Close()
			case "file", "symlink":
				if err = os.Rename(address, address+".original"); err == nil {
					if condition == "file" {
						err = os.WriteFile(address, nil, 0600)
					} else {
						err = os.Symlink(address+".original", address)
					}
				}
			case "public parent":
				err = os.Chmod(parent, 0755)
			case "public socket":
				err = os.Chmod(address, 0666)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(address)
			if err != nil {
				t.Fatal(err)
			}
			parentBefore, err := os.Lstat(parent)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			competitor, err := ListenPeer(ctx, root, runner, instance)
			if competitor != nil {
				competitor.Close()
			}
			if err == nil {
				t.Fatal("listener took over an existing endpoint")
			}
			after, err := os.Lstat(address)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("failed listener changed an existing endpoint", err)
			}
			parentAfter, err := os.Lstat(parent)
			if err != nil || !os.SameFile(parentBefore, parentAfter) || parentBefore.Mode() != parentAfter.Mode() {
				t.Fatal("failed listener changed an existing parent", err)
			}
		})
	}
}

func TestRunnerListenerClosePreservesReplacement(t *testing.T) {
	for _, replace := range []string{"socket", "parent"} {
		t.Run(replace, func(t *testing.T) {
			root := privateTempDir(t)
			const runner, instance = "01234567-89ab-4cde-8fab-0123456789ab", "01234567-89ab-4cde-8fab-0123456789ac"
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			original, err := ListenPeer(ctx, root, runner, instance)
			if err != nil {
				t.Fatal(err)
			}
			defer original.Close()
			address, _ := PeerAddress(root, runner, instance)
			parent := filepath.Dir(address)
			old := address
			if replace == "parent" {
				old = parent
			}
			if err := os.Rename(old, filepath.Join(root, "old-"+replace)); err != nil {
				t.Fatal(err)
			}
			if replace == "parent" {
				if err := os.Mkdir(parent, 0700); err != nil {
					t.Fatal(err)
				}
			}
			replacement, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { replacement.Close(); os.Remove(parent) }()
			if err := os.Chmod(address, 0600); err != nil {
				t.Fatal(err)
			}
			if err := original.Close(); err != nil {
				t.Fatal(err)
			}
			connection, err := DialPeer(ctx, root, runner, instance)
			if err != nil {
				t.Fatal("old listener cleanup removed its replacement", err)
			}
			connection.Close()
		})
	}
}
