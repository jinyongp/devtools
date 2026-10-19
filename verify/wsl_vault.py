"""Import, terminate and remove only a uniquely owned disposable CI WSL2 distro."""
import argparse
import hashlib
import json
import os
import shutil
from pathlib import Path
import subprocess
import tempfile
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    if os.name != 'nt' or os.environ.get('DEVTOOLS_DISPOSABLE_HOST') != '1':
        parser.error('Requires an explicitly disposable Windows host')
    binary = args.binary.resolve()
    scenario = Path(__file__).with_name('wsl_vault_scenario.py').resolve()
    name = 'devtools-vault-test-' + uuid.uuid4().hex
    report = {'result': 'fail', 'distribution': name,
              'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'phases': []}
    imported = False
    import_attempted = False

    def wsl(*words, timeout=120):
        result = subprocess.run(['wsl.exe', *words], capture_output=True, timeout=timeout)
        if result.returncode:
            report['failed_operation'] = words[0]
            report['operation_exit'] = result.returncode
            # WSL output contains no fixture values: don't forward arbitrary diagnostics.
            raise RuntimeError('WSL operation failed: ' + words[0])
        return result.stdout.replace(b'\x00', b'').decode('utf-8-sig').strip()

    def run(*words):
        return wsl('--distribution', name, '--user', 'root', '--exec', *words)

    temporary = tempfile.mkdtemp(prefix='devtools-wsl-import-')
    report['recovery_root'] = temporary
    try:
        # Missing virtualization/kernel support is a failed prerequisite, never a
        # WSL1 fallback or a passing skipped test. No global shutdown is used.
        report['wsl_version'] = wsl('--version')
        existing = wsl('--list', '--quiet').splitlines()
        if name in existing:
            raise RuntimeError('Refusing to reuse an existing distribution')
        image = Path(temporary) / 'ubuntu.rootfs.tar.gz'
        # Canonical's published 24.04 initial WSL image; fixed digest from
        # https://cloud-images.ubuntu.com/wsl/releases/24.04/current/SHA256SUMS
        url = 'https://cloud-images.ubuntu.com/wsl/releases/24.04/current/ubuntu-noble-wsl-amd64-24.04lts.rootfs.tar.gz'
        expected = '2a790896740b14d637dbdc583cce1ba081ac53b9e9cdb46dc09a2f73abbd9934'
        digest = hashlib.sha256()
        with urllib.request.urlopen(url, timeout=120) as source, image.open('wb') as output:
            while block := source.read(1024 * 1024):
                digest.update(block)
                output.write(block)
        if digest.hexdigest() != expected:
            raise RuntimeError('Canonical WSL rootfs checksum mismatch')
        report['rootfs_sha256'] = expected
        import_attempted = True
        imported = True  # Retain recovery files if a partial import cannot be inspected.
        wsl('--import', name, str(Path(temporary) / 'instance'), str(image), '--version', '2')
        run('/usr/sbin/useradd', '--create-home', '--shell', '/bin/bash', 'dvtfixture')
        for source, destination in [(binary, '/usr/local/bin/devtools'), (scenario, '/home/dvtfixture/scenario.py')]:
            linux_source = run('/usr/bin/wslpath', '-u', str(source))
            run('/usr/bin/install', '-m', '755', linux_source, destination)
        for phase in ('prepare', 'after-manual-restart', 'after-unlocked-restart'):
            if phase != 'prepare':
                wsl('--terminate', name)
            output = wsl('--distribution', name, '--user', 'dvtfixture', '--exec',
                         '/usr/bin/env', 'HOME=/home/dvtfixture',
                         '/usr/bin/python3', '-B', '/home/dvtfixture/scenario.py', phase)
            result = json.loads(output)
            if result.get('result') != 'pass' or result.get('phase') != phase:
                raise RuntimeError('WSL restart phase did not pass')
            if result.get('binary_identity', {}).get('commit') != args.commit:
                raise RuntimeError('Executed WSL candidate identity mismatch')
            report['phases'].append(result)
        report['checks_passed'] = True
    finally:
        try:
            # A failed import can still have registered this unique new name.
            if import_attempted:
                if name in wsl('--list', '--quiet').splitlines():
                    wsl('--terminate', name)
                    wsl('--unregister', name)
                    report['owned_distribution_removed'] = True
                imported = False
        finally:
            if not imported:
                shutil.rmtree(temporary)
            if report.get('checks_passed') and report.get('owned_distribution_removed'):
                report['result'] = 'pass'
            args.evidence.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))


if __name__ == '__main__':
    main()
