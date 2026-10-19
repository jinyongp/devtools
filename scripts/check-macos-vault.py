"""Run opt-in Keychain tests in a disposable Actions keychain, then restore it."""
import json
import os
from pathlib import Path
import platform
import shlex
import subprocess
import sys
import tempfile


def security(*args):
    return subprocess.check_output(['/usr/bin/security', *args], text=True, timeout=20).strip()


def main():
    if sys.platform != 'darwin' or os.environ.get('DEVTOOLS_DISPOSABLE_HOST') != '1':
        raise RuntimeError('Requires an explicitly disposable macOS host')
    original_default = shlex.split(security('default-keychain', '-d', 'user'))
    original_search = shlex.split(security('list-keychains', '-d', 'user'))
    if len(original_default) != 1:
        raise RuntimeError('Cannot preserve original keychain selection')
    with tempfile.TemporaryDirectory(prefix='devtools-native-keychain-') as temporary:
        keychain = str(Path(temporary) / 'synthetic.keychain-db')
        created = False
        try:
            # Empty password belongs only to this disposable synthetic keychain.
            security('create-keychain', '-p', '', keychain)
            created = True
            security('set-keychain-settings', keychain)
            security('unlock-keychain', '-p', '', keychain)
            security('list-keychains', '-d', 'user', '-s', keychain)
            security('default-keychain', '-d', 'user', '-s', keychain)
            env = dict(os.environ, DEVTOOLS_TEST_NATIVE_KEYCHAIN='1', DEVTOOLS_TEST_KEYCHAIN=keychain)
            with open('vault-keychain-results.jsonl', 'w') as output:
                result = subprocess.run(['go', 'test', '-json', '-count=1', '-timeout', '3m',
                                         './internal/vault', '-run', '^TestNativeKeychain'],
                                        env=env, stdout=output, stderr=subprocess.STDOUT, timeout=210)
            if result.returncode:
                raise RuntimeError('Native Keychain tests failed; inspect value-free JSON test evidence')
        finally:
            try:
                security('default-keychain', '-d', 'user', '-s', original_default[0])
                security('list-keychains', '-d', 'user', '-s', *original_search)
            finally:
                if created:
                    security('delete-keychain', keychain)
    print(json.dumps({'platform': platform.platform(), 'architecture': platform.machine(),
                      'keychain_tests': 'pass', 'synthetic_keychain_removed': True}))


if __name__ == '__main__':
    main()
