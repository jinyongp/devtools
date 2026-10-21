//go:build linux || darwin

package vault

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func init() { nativePeerFixture = runUnixPeerFixture }

func runUnixPeerFixture(mode, path string) int {
	if os.Getuid() != 65534 {
		return 2
	}
	switch mode {
	case "server":
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			return 3
		}
		defer listener.Close()
		if os.Chmod(path, 0777) != nil {
			return 4
		}
		fmt.Println("ready-uid-65534")
		listener.SetDeadline(time.Now().Add(10 * time.Second))
		connection, err := listener.AcceptUnix()
		if err != nil {
			return 5
		}
		defer connection.Close()
	case "client":
		connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			return 6
		}
		connection.Close()
	default:
		return 7
	}
	return 0
}

// Run only as root on a disposable runner. The peer is a real process with a
// different kernel UID. Public fixture sockets contain no keys or session data.
func TestNativeUnixPeersRejectOtherUID(t *testing.T) {
	if os.Getenv("DEVTOOLS_TEST_NATIVE_PEERS") != "1" {
		t.Skip("requires disposable privileged native peer fixture")
	}
	if os.Getenv("DEVTOOLS_DISPOSABLE_HOST") != "1" || os.Geteuid() != 0 {
		t.Fatal("native peer fixture requires an explicitly disposable root process")
	}
	temporary, err := os.MkdirTemp("/tmp", "devtools-native-peer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(temporary); err != nil {
			t.Error("cannot remove owned native peer fixture", err)
		}
	})
	root, err := filepath.EvalSymlinks(temporary)
	if err != nil || os.Chmod(root, 0755) != nil {
		t.Fatal("cannot prepare traversable synthetic fixture root")
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "peer-fixture")
	if err := os.WriteFile(binary, body, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child := func(mode, socket string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, "__vault-peer-fixture", mode, socket)
		cmd.Env = []string{}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
		return cmd
	}
	t.Run("client rejects other-user server", func(t *testing.T) {
		directory := filepath.Join(root, "foreign-server")
		if err := os.Mkdir(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(directory, 65534, 65534); err != nil {
			t.Fatal("cannot prepare other-user socket directory", err)
		}
		path := filepath.Join(directory, "server.sock")
		cmd := child("server", path)
		ready, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal("cannot start different-UID server", err)
		}
		defer func() { cmd.Process.Kill(); cmd.Wait() }()
		line, err := bufio.NewReader(ready).ReadString('\n')
		if err != nil || line != "ready-uid-65534\n" {
			t.Fatal("different-UID server did not become ready")
		}
		connection, err := dialSession(ctx, path)
		if connection != nil {
			connection.Close()
		}
		if !errors.Is(err, ErrDenied) {
			t.Fatal("client did not reject foreign server UID", err)
		}
	})
	t.Run("server rejects other-user client", func(t *testing.T) {
		path := filepath.Join(root, "client.sock")
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		if err := os.Chmod(path, 0777); err != nil {
			t.Fatal(err)
		}
		listener.SetDeadline(time.Now().Add(10 * time.Second))
		cmd := child("client", path)
		if err := cmd.Start(); err != nil {
			t.Fatal("cannot start different-UID client", err)
		}
		defer func() { cmd.Process.Kill(); cmd.Wait() }()
		connection, err := listener.AcceptUnix()
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		if err := verifySessionPeer(connection); !errors.Is(err, ErrDenied) {
			t.Fatal("server did not reject foreign client UID", err)
		}
	})
	t.Run("runner transport rejects other-user client", func(t *testing.T) {
		runner, instance := "01234567-89ab-4cde-8fab-0123456789ab", "01234567-89ab-4cde-8fab-0123456789ac"
		transportRoot := filepath.Join(root, "runner-private")
		listener, err := ListenPeer(ctx, transportRoot, runner, instance)
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		address, err := PeerAddress(transportRoot, runner, instance)
		if err != nil {
			t.Fatal(err)
		}
		attempt, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			connection, err := listener.Accept(attempt)
			if connection != nil {
				connection.Close()
				done <- nil
				return
			}
			done <- err
		}()
		cmd := child("client", address)
		err = cmd.Run()
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 6 {
				t.Fatal("unexpected foreign client failure", err)
			}
		}
		cancel()
		if err := <-done; err == nil {
			t.Fatal("new runner channel admitted foreign client", err)
		}
	})
}
