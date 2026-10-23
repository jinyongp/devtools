"""Use only the parent fixture's private bus and synthetic HOME."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

from native_rewrap import check_native_rewrap
from vault_test_support import retire_vaults


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--binary', required=True, type=Path)
    ap.add_argument('--unavailable', action='store_true')
    args = ap.parse_args()
    if os.environ.get('DEVTOOLS_DISPOSABLE_HOST') != '1' or not Path.home().name.startswith('devtools-native-secret-'):
        raise RuntimeError('Requires the wrapper-owned Secret Service bus')
    with tempfile.TemporaryDirectory(prefix='rewrap-', dir=Path.home()) as temporary:
        root = Path(temporary)
        runtime = root / 'runtime'
        runtime.mkdir(mode=0o700)
        env = {k: v for k, v in os.environ.items() if not k.startswith('DEVTOOLS_')}
        env.update(DEVTOOLS_DISPOSABLE_HOST='1', XDG_CONFIG_HOME=str(root/'config'), XDG_DATA_HOME=str(root/'data'),
                   XDG_CACHE_HOME=str(root/'cache'), XDG_RUNTIME_DIR=str(runtime))
        directory = root / 'data/devtools/vault'
        def api(*words):
            p = subprocess.run([str(args.binary), *map(str, words)], cwd=root, env=env,
                               capture_output=True, text=True, timeout=45)
            assert p.returncode == 0, 'Native fixture setup failed'
            return json.loads(p.stdout)['data']
        try:
            api('init', '--profile', 'native-rewrap')
            if args.unavailable:
                password = root / 'passphrase'
                password.write_text('synthetic-unavailable-native-rewrap\n')
                password.chmod(0o600)
                api('vault', 'init', '--backend', 'passphrase', '--passphrase-file', password)
                slot = directory / 'key-slot.json'
                before = slot.read_bytes()
                p = subprocess.run([str(args.binary), 'vault', 'rewrap', '--from-backend', 'passphrase', '--backend', 'native',
                                    '--current-passphrase-file', str(password), '--request-id', '00000000-0000-4000-8000-000000000455'],
                                   cwd=root, env=env, capture_output=True, text=True, timeout=45)
                assert p.returncode != 0 and json.loads(p.stderr)['error']['code'] == 'key_backend_unavailable'
                assert slot.read_bytes() == before and not (directory / 'rewrap-pending.json').exists()
                result = {'unavailable_refused': True, 'no_fallback': True, 'original_slot_preserved': True}
            else:
                api('vault', 'init')
                result = check_native_rewrap(args.binary, env, root, 'native-rewrap', directory)
        finally:
            retire_vaults(directory)
    name = 'unavailable' if args.unavailable else 'native'
    Path('vault-secret-service-rewrap-'+name+'.json').write_text(json.dumps(result, indent=2)+'\n')

if __name__ == '__main__':
    main()
