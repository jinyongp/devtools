package vault

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
)

// Exercise parsing and identity checks through a real separate daemon and its
// platform transport, not just an in-memory frame reader. These requests must
// neither deliver a key nor extend the warm session's idle expiry.
func TestSessionActualIPCRejectsMalformedRequests(t *testing.T) {
	directory, protector, want := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := (&SessionManager{Directory: directory}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		retire, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		client.Retire(retire)
	}()
	if err := client.Unlock(ctx, protector.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	initial, err := client.Status(ctx)
	if err != nil || initial.IdleExpiresAt == nil {
		t.Fatal("expected warm session", err)
	}
	var previousChallenge string
	for _, name := range []string{"duplicate operation", "unknown field", "zero frame", "oversized frame", "wrong vault", "stale session", "protocol mismatch", "stale challenge"} {
		t.Run(name, func(t *testing.T) {
			connection, err := dialSession(ctx, client.Endpoint.Endpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var hello sessionHello
			if err := readSessionFrame(connection, &hello); err != nil {
				t.Fatal(err)
			}
			request := testSessionRequest(client.Endpoint, hello, "use")
			malformed := false
			switch name {
			case "duplicate operation", "unknown field":
				body, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				suffix := `,"operation":"lock"}`
				if name == "unknown field" {
					suffix = `,"unexpected":true}`
				}
				body = append(body[:len(body)-1], suffix...)
				var header [4]byte
				binary.BigEndian.PutUint32(header[:], uint32(len(body)))
				if err := writeAll(connection, header[:]); err != nil {
					t.Fatal(err)
				}
				if err := writeAll(connection, body); err != nil {
					t.Fatal(err)
				}
				malformed = true
			case "zero frame", "oversized frame":
				var header [4]byte
				if name == "oversized frame" {
					binary.BigEndian.PutUint32(header[:], 0x7fffffff)
				}
				if err := writeAll(connection, header[:]); err != nil {
					t.Fatal(err)
				}
				malformed = true
			case "wrong vault":
				request.VaultID = "1123456789abcdef0123456789abcdef"
			case "stale session":
				request.SessionID = "1123456789abcdef0123456789abcdef"
			case "protocol mismatch":
				request.Protocol++
			case "stale challenge":
				request.Challenge = previousChallenge
			}
			previousChallenge = hello.Challenge
			if !malformed {
				if err := writeSessionFrame(connection, request); err != nil {
					t.Fatal(err)
				}
			}
			var response sessionResponse
			err = readSessionFrame(connection, &response)
			defer clear(response.Key)
			if len(response.Key) != 0 {
				t.Fatal("invalid request received key material")
			}
			if malformed {
				var timeout net.Error
				if err == nil || (errors.As(err, &timeout) && timeout.Timeout()) {
					t.Fatal("malformed frame was not promptly rejected", err)
				}
			} else if err != nil || response.Error != ErrSessionProtocol.Error() {
				t.Fatal("identity mismatch not rejected", err)
			}
			current, err := client.Status(ctx)
			if err != nil || current.State != initial.State || current.IdleExpiresAt == nil || !current.IdleExpiresAt.Equal(*initial.IdleExpiresAt) {
				t.Fatal("rejected request or polling changed warm session", err)
			}
		})
	}
	key, err := client.Use(ctx)
	if err != nil {
		t.Fatal("valid client failed after rejected requests", err)
	}
	defer key.Destroy()
	if !sameMaterial(key, want) {
		t.Fatal("rejected frames changed the key")
	}
}

// Endpoint metadata is untrusted storage. A remote Windows pipe or a different
// local endpoint must fail before dialing, even if the rest of its IDs match.
func TestSessionEndpointRejectsForeignTransport(t *testing.T) {
	directory, _, _ := sessionFixture(t)
	vaultID, _ := NewID()
	sessionID, _ := NewID()
	endpoint := SessionEndpoint{Protocol: SessionProtocol, VaultID: vaultID, SessionID: sessionID, Endpoint: sessionEndpoint(directory, sessionID)}
	root, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	write := func(e SessionEndpoint) {
		t.Helper()
		body, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if err := root.WriteFile(context.Background(), "session.json", body, true); err != nil {
			t.Fatal(err)
		}
	}
	write(endpoint)
	if _, err := ReadSessionEndpoint(directory); err != nil {
		t.Fatal("valid local endpoint rejected", err)
	}
	for _, foreign := range []string{`\\localhost\pipe\devtools-vault-fixture`, `\\127.0.0.1\pipe\devtools-vault-fixture`, `\\remote.invalid\pipe\devtools-vault-fixture`, sessionEndpoint(directory+"-other", sessionID)} {
		endpoint.Endpoint = foreign
		write(endpoint)
		if _, err := ReadSessionEndpoint(directory); !errors.Is(err, ErrRecovery) {
			t.Fatal("foreign endpoint metadata accepted", err)
		}
	}
}
