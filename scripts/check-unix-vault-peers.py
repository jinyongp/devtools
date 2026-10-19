"""Require real different-UID IPC checks on a disposable Linux/macOS runner."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def main():
    if sys.platform not in ('linux', 'darwin') or os.environ.get('DEVTOOLS_DISPOSABLE_HOST') != '1':
        raise RuntimeError('Requires an explicitly disposable native Unix host')
    log = Path('vault-peers-' + sys.platform + '.jsonl')
    with tempfile.TemporaryDirectory(prefix='devtools-peer-check-') as temporary:
        binary = Path(temporary).resolve() / 'vault.test'
        subprocess.run(['go', 'test', '-c', '-o', str(binary), './internal/vault'], check=True, timeout=120)
        with log.open('w') as output, log.with_suffix('.stderr').open('w') as diagnostics:
            result = subprocess.run([
                'go', 'tool', 'test2json', '-p', 'github.com/jinyongp/devtools/internal/vault',
                'sudo', '-n', 'env', 'DEVTOOLS_DISPOSABLE_HOST=1', 'DEVTOOLS_TEST_NATIVE_PEERS=1',
                str(binary), '-test.run', '^TestNativeUnixPeersRejectOtherUID$',
                '-test.v=test2json', '-test.count=1', '-test.timeout=45s',
            ], stdout=output, stderr=diagnostics, timeout=60)
        if result.returncode:
            raise RuntimeError('Actual different-UID IPC test failed; inspect native evidence')
    events = [json.loads(line) for line in log.read_text().splitlines()]
    passed = {event.get('Test') for event in events if event.get('Action') == 'pass'}
    required = {'TestNativeUnixPeersRejectOtherUID',
                'TestNativeUnixPeersRejectOtherUID/client_rejects_other-user_server',
                'TestNativeUnixPeersRejectOtherUID/server_rejects_other-user_client'}
    if not required <= passed or any(event.get('Action') == 'skip' for event in events):
        raise RuntimeError('Required native IPC checks did not actually pass')
    print(json.dumps({'native_different_uid': 'pass', 'both_peer_directions': 'pass', 'platform': sys.platform}))


if __name__ == '__main__':
    main()
