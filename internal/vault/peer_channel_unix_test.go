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
