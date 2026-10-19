"""Exercise an actual pre-vault CLI against a newly protected disposable root."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import tarfile

from vault_test_support import retire_vaults

BASELINE = '89100eb98888895a450b2ebd403e4e1c9b5f52d2'
SECRET = 'synthetic-actual-legacy-migration-token'
PASSWORD = 'synthetic-actual-legacy-vault-password'


def build_baseline(directory):
    repository = Path(__file__).resolve().parent.parent
    directory.mkdir()
    archive = directory / 'source.tar'
    with archive.open('wb') as output:
        result = subprocess.run(['git', 'archive', BASELINE], cwd=repository,
                                stdout=output, stderr=subprocess.PIPE, timeout=30)
    if result.returncode:
        raise RuntimeError('Fetch the pinned baseline commit before building the compatibility fixture')
    source = directory / 'source'
    source.mkdir()
    with tarfile.open(archive) as package:
        package.extractall(source, filter='data')
    binary = directory / ('legacy.exe' if os.name == 'nt' else 'legacy')
    result = subprocess.run(['go', 'build', '-buildvcs=false', '-trimpath', '-ldflags', '-X main.commit=' + BASELINE,
                             '-o', str(binary), './cmd/devtools'], cwd=source,
                            capture_output=True, timeout=180)
    if result.returncode:
        raise RuntimeError('Pinned legacy source did not build on the native host')
    return binary


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--current', required=True, type=Path)
    parser.add_argument('--legacy', type=Path, help='Use an already-built exact baseline; otherwise build the pinned git source')
    parser.add_argument('--evidence', required=True, type=Path)
    args = parser.parse_args()
    current = args.current.resolve(strict=True)
    legacy = args.legacy.resolve(strict=True) if args.legacy is not None else None
    with tempfile.TemporaryDirectory(prefix='devtools-actual-legacy-') as scratch:
        root = Path(scratch).resolve()
        if legacy is None:
            legacy = build_baseline(root / 'baseline-build')
        legacy_digest = hashlib.sha256(legacy.read_bytes()).hexdigest()
        env = {k: v for k, v in os.environ.items() if not k.startswith('DEVTOOLS_')}
        env['DEVTOOLS_ISOLATED_ROOT'] = str(root)
        def api(binary, *argv, input=None, denied=False):
            result = subprocess.run([str(binary), *argv], cwd=root, env=env,
                                    input=input.encode() if input is not None else None,
                                    capture_output=True, timeout=30)
            if SECRET.encode() in result.stdout + result.stderr or PASSWORD.encode() in result.stdout + result.stderr:
                raise RuntimeError('Legacy fixture value reached public response')
            if denied:
                if not result.returncode or result.stdout:
                    raise RuntimeError('Actual old CLI accepted protected-root access')
                error = json.loads(result.stderr)['error']['code']
                if error not in ('storage_error', 'vault_recovery_required', 'migration_required'):
                    raise RuntimeError('Unexpected old CLI denial classification')
                return error
            if result.returncode or result.stderr:
                raise RuntimeError('Compatibility fixture CLI failed: ' + '/'.join(argv[:2]))
            return json.loads(result.stdout)['data']
        old_version = api(legacy, 'version')
        new_version = api(current, 'version')
        if old_version['commit'] != BASELINE:
            raise RuntimeError('Actual baseline source identity mismatch')
        directory = None
        try:
            api(legacy, 'init', '--profile', 'compatibility')
            api(legacy, 'sec', 'set', 'TOKEN', '--profile', 'compatibility', '--stdin', input=SECRET)
            api(legacy, 'var', 'set', 'URL', '--profile', 'compatibility', '--value', 'https://example.invalid')
            # A user-owned input file remains outside the protected data tree.
            # Migration must not silently delete or claim to protect this copy.
            source = root / 'original.env'
            source.write_text('TOKEN=' + SECRET + '\n'); source.chmod(0o600)
            data = Path(api(current, 'project', 'inspect', '--profile', 'compatibility')['paths']['data'])
            directory = data / 'vault'
            api(current, 'vault', 'init', '--backend', 'passphrase', '--passphrase-stdin', input=PASSWORD + '\n')
            data = Path(api(current, 'project', 'inspect', '--profile', 'compatibility')['paths']['data'])
            directory = data / 'vault'
            def disk_state():
                return {str(p.relative_to(data)): hashlib.sha256(p.read_bytes()).hexdigest()
                        for p in data.rglob('*.json') if p.is_file()}
            before = disk_state()
            denials = []
            for profile, operation in [('compatibility', ['var', 'get', 'URL']),
                                       ('compatibility', ['var', 'set', 'URL', '--value', 'changed']),
                                       ('new-profile', ['sec', 'set', 'TOKEN', '--stdin'])]:
                denials.append(api(legacy, *operation, '--profile', profile,
                                   input=SECRET if '--stdin' in operation else None, denied=True))
            if disk_state() != before:
                raise RuntimeError('Old CLI changed protected data or created a plaintext profile')
            if source.read_text() != 'TOKEN=' + SECRET + '\n':
                raise RuntimeError('Migration changed original user input')
            result = subprocess.run([str(current), 'run', '--profile', 'compatibility', '--', sys.executable,
                                     '-c', 'import os,sys;sys.stdout.write(os.environ["TOKEN"])'],
                                    cwd=root, env=env, capture_output=True, timeout=30)
            if result.returncode or result.stdout != b'[REDACTED]' or result.stderr:
                raise RuntimeError('Actual migrated secret did not execute safely')
            # Windows byte-range leases can forbid even read-only scans while
            # the owning daemon is alive. Drain this fixture's worker first;
            # inspect all files, including launch/receipt state from the run.
            retire_vaults(directory)
            for path in data.rglob('*'):
                if not path.is_file():
                    continue
                try:
                    content = path.read_bytes()
                except OSError as error:
                    raise RuntimeError('Cannot scan owned fixture file: ' + str(path.relative_to(data))) from error
                if SECRET.encode() in content:
                    raise RuntimeError('Plaintext canary remains in migrated data')

        finally:
            if directory is not None:
                retire_vaults(directory)
    evidence = {'result': 'pass', 'baseline_commit': BASELINE, 'candidate': new_version,
                'baseline_sha256': legacy_digest,
                'candidate_sha256': hashlib.sha256(current.read_bytes()).hexdigest(),
                'denials': denials, 'protected_json_unchanged': True, 'migrated_data_canary_scan': 'pass',
                'original_input_preserved_and_still_sensitive': True, 'owned_root_removed': True}
    args.evidence.write_text(json.dumps(evidence, indent=2) + '\n')
    print(json.dumps({'result': 'pass', 'actual_baseline': BASELINE, 'owned_root_removed': True}))


if __name__ == '__main__':
    main()
