package cli

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestTaskInputCancellationDoesNotRequirePipeClose(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan *protocol.Error, 1)
	go func() {
		_, inputErr := taskInput(ctx, IO{In: reader}, Request{Options: map[string]string{"stdin": "true"}})
		result <- inputErr
	}()

	cancel()
	select {
	case inputErr := <-result:
		if inputErr == nil || inputErr.Code != "canceled" || inputErr.ExitCode != 130 {
			t.Fatalf("unexpected cancellation result: %+v", inputErr)
		}
	case <-time.After(time.Second):
		t.Fatal("task input did not return after cancellation")
	}

	late := []byte("{\"title\":\"late\"}")
	if _, err := writer.Write(late); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	remaining, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(remaining) != string(late) {
		t.Fatalf("canceled read consumed caller input: %q", remaining)
	}
}
