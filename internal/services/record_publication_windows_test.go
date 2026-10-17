package services

import (
	"context"
	"fmt"
	"github.com/jinyongp/devtools/internal/tasks"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Process status/list can overlap the supervisor's atomic readiness publication.
// Readers must see one complete snapshot, never a transient private-storage error.
func TestWindowsConcurrentRecordPublication(t *testing.T) {
	data := privateTempDir(t)
	store := Store{Data: data}
	id := tasks.ID()
	record := Record{ID: id, Profile: "publication-fixture", Directory: data, Command: "web", State: "running", CreatedAt: time.Now().UTC()}
	path := filepath.Join(data, "processes", id, "record.json")
	if err := writePrivate(path, record); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan struct{})
	failures := make(chan error, 3)
	var group sync.WaitGroup
	for reader := 0; reader < 2; reader++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				select {
				case <-done:
					return
				case <-ctx.Done():
					failures <- ctx.Err()
					return
				default:
				}
				records, failure := store.StoredRecords()
				if failure != nil {
					failures <- fmt.Errorf("read: %s %v", failure.Code, failure.Details)
					return
				}
				if len(records) != 1 || records[0].ID != id || records[0].State != "running" {
					failures <- fmt.Errorf("incomplete snapshot")
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	for iteration := 0; iteration < 200; iteration++ {
		record.Reason = fmt.Sprint(iteration)
		if err := writePrivate(path, record); err != nil {
			failures <- fmt.Errorf("publication: %w", err)
			break
		}
		if ctx.Err() != nil {
			failures <- ctx.Err()
			break
		}
	}
	close(done)
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
