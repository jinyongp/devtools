//go:build automationbench

package values

import (
	"context"
	"testing"

	"github.com/jinyongp/devtools/internal/storageupgrade"
)

// Both arms time completed request lookup. Current storage must be explicitly
// upgraded during untimed setup; historical arms keep their direct legacy read.
func prepareAutomationLegacyLookup(b *testing.B, root string) {
	b.Helper()
	engine := storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter(root)}}
	report, err := engine.Fix(context.Background())
	if err != nil || !report.Ready || !report.Changed {
		b.Fatal("explicit legacy receipt fixture upgrade", report.Ready, report.Changed, err)
	}
}
