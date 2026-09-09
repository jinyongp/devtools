package dashboard

import (
	"github.com/jinyongp/devtools/internal/tasks"
	"testing"
)

func TestServerCompatibilityIncludesEmbeddedUI(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status tasks.Object
		want   bool
	}{
		{"current", tasks.Object{"auth_protocol": float64(authProtocol), "asset_version": assetVersion, "task_protocol": float64(tasks.ProjectionVersion)}, true},
		{"previous task protocol", tasks.Object{"auth_protocol": float64(authProtocol), "asset_version": assetVersion, "task_protocol": float64(tasks.ProjectionVersion - 1)}, false},
		{"previous UI", tasks.Object{"auth_protocol": float64(authProtocol), "asset_version": "old"}, false},
		{"legacy", tasks.Object{"auth_protocol": float64(authProtocol)}, false},
		{"previous protocol", tasks.Object{"auth_protocol": float64(authProtocol - 1), "asset_version": assetVersion}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if compatibleServer(tc.status) != tc.want {
				t.Fatal("incorrect server reuse")
			}
		})
	}
}
