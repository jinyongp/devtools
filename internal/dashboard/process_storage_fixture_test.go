package dashboard

import (
	"context"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"testing"
)

func prepareProcessFixture(t *testing.T, root string) {
	t.Helper()
	report, condition := (storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{services.StorageUpgradeAdapter(), services.LogStorageUpgradeAdapter()}}).Fix(context.Background())
	if condition != nil || !report.Ready {
		t.Fatal(report, condition)
	}
}
