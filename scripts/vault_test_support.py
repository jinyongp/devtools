"""Build a value-free control helper for owned native product fixture cleanup."""
import atexit
import os
from pathlib import Path
import subprocess
import tempfile

_control = None
_scratch = None


def retire_vaults(*directories):
    global _control, _scratch
    # Preserve the caller's spelling: session endpoints bind to the exact data
    # root, and Windows resolve() expands 8.3 paths used by tempfile/Go.
    # Unix product roots are canonical, including macOS /var -> /private/var.
    existing = [Path(os.path.abspath(path)) if os.name == 'nt' else Path(path).resolve()
                for path in directories if (Path(path) / 'session.json').exists()]
    if not existing:
        return
    if _control is None:
        # Keep the helper outside the roots it will retire/remove on Windows.
        _scratch = tempfile.TemporaryDirectory(prefix='devtools-vault-control-')
        atexit.register(_scratch.cleanup)
        _control = Path(_scratch.name) / ('vault-fixture.exe' if os.name == 'nt' else 'vault-fixture')
        subprocess.run(['go', 'build', '-o', str(_control), './scripts/vault-fixture'],
                       cwd=Path(__file__).resolve().parent.parent, check=True,
                       capture_output=True, timeout=120)
    subprocess.run([str(_control), *map(str, existing)], check=True, timeout=15 * len(existing))
