package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
	"io"
	"os"
	"testing"
	"time"
)

func TestWindowsTaskPipeCancelAfterPartialInputPreservesFutureBytes(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	prefix := []byte(`{"title":`)
	if _, err = writer.Write(prefix); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *protocol.Error, 1)
	go func() {
		_, failure := taskInput(ctx, IO{In: reader}, Request{Options: map[string]string{"stdin": "true"}})
		done <- failure
	}()
	deadline := time.Now().Add(time.Second)
	for {
		available, err := taskPipeAvailable(reader)
		if err != nil {
			t.Fatal(err)
		}
		if available == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("partial input was not consumed")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case failure := <-done:
		if failure == nil || failure.Code != "canceled" || failure.ExitCode != 130 {
			t.Fatal(failure)
		}
	case <-time.After(time.Second):
		t.Fatal("pipe cancellation blocked")
	}
	if _, err = writer.Write([]byte("future")); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "future" {
		t.Fatal("canceled reader consumed future input", string(body), err)
	}
}
func TestWindowsTaskPipeCollectsChunksUntilEOF(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	go func() {
		writer.Write([]byte(`{"title":"`))
		time.Sleep(20 * time.Millisecond)
		writer.Write([]byte("한글\"}"))
		writer.Close()
	}()
	body, failure := taskInput(context.Background(), IO{In: reader}, Request{Options: map[string]string{"stdin": "true"}})
	if failure != nil || body != `{"title":"한글"}` {
		t.Fatal(body, failure)
	}
}
