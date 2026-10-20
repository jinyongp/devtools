package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/vault"
)

func TestVaultInitUnavailableSecretServiceOffersExplicitPassphrase(t *testing.T) {
	app := testApp(t)
	// Pin the bus to an absent socket owned by this fixture. Never contact or
	// activate the user's real Secret Service during this regression test.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(privateTempDir(t), "absent-bus"))
	code, out, diagnostic := invoke(t, app, "", "vault", "init")
	if code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"key_backend_unavailable"`) || !strings.Contains(diagnostic, "Secret Service") || !strings.Contains(diagnostic, `"--backend","passphrase"`) {
		t.Fatalf("missing actionable native setup error: exit=%d stdout=%s stderr=%s", code, out, diagnostic)
	}
	code, out, diagnostic = invoke(t, app, "", "vault", "status")
	if code != 0 || vaultStatusResult(t, out).State != vault.Uninitialized {
		t.Fatalf("failed native setup changed vault state: exit=%d stdout=%s stderr=%s", code, out, diagnostic)
	}
}
