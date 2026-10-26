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
import urllib.error
import urllib.parse
import urllib.request

from vault_test_support import retire_vaults
from cli_json import json_argv, json_environment

BASELINE = '89100eb98888895a450b2ebd403e4e1c9b5f52d2'
SECRET = 'synthetic-actual-legacy-migration-token'
PASSWORD = 'synthetic-actual-legacy-vault-password'


def dashboard_request(origin, path, credential=None, body=None):
    headers = {'Origin': origin}
    if credential is not None:
        headers['Authorization'] = 'Bearer ' + credential
    encoded = None
    if body is not None:
        headers['Content-Type'] = 'application/json'
        encoded = json.dumps(body).encode()
    request = urllib.request.Request(origin + path, data=encoded, headers=headers)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        response = opener.open(request, timeout=10)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        content = response.read(2 * 1024 * 1024 + 1)
        status = response.code
    if len(content) > 2 * 1024 * 1024 or SECRET.encode() in content or PASSWORD.encode() in content:
        raise RuntimeError('Unsafe response from owned dashboard fixture')
    if status == 401:
        # The authentication boundary deliberately uses a plain HTTP denial.
        return status, {}
    return status, json.loads(content)


def dashboard_session(started):
    url = urllib.parse.urlsplit(started['url'])
    if url.scheme != 'http' or url.hostname not in ('127.0.0.1', 'localhost', '::1'):
        raise RuntimeError('Dashboard fixture must use a loopback address')
    origin = urllib.parse.urlunsplit((url.scheme, url.netloc, '', '', ''))
    token = urllib.parse.parse_qs(url.fragment)['token'][0]
    status, result = dashboard_request(origin, '/session', body={'token': token})
    if status != 200 or not result.get('ok'):
        raise RuntimeError('Owned dashboard session creation failed')
    return origin, result['token']


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
        env['DEVTOOLS_FORMAT'] = 'json'
        def api(binary, *argv, input=None, denied=False):
            result = subprocess.run(json_argv(binary,argv,environment=env,cwd=root), cwd=root, env=env,
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
        dashboard_started = False
        try:
            api(legacy, 'init', '--profile', 'compatibility')
            api(legacy, 'sec', 'set', 'TOKEN', '--profile', 'compatibility', '--stdin', input=SECRET + '-initial')
            api(legacy, 'var', 'set', 'URL', '--profile', 'compatibility', '--value', 'https://example.invalid')
            # A user-owned input file remains outside the protected data tree.
            # Migration must not silently delete or claim to protect this copy.
            source = root / 'original.env'
            source.write_text('TOKEN=' + SECRET + '\n'); source.chmod(0o600)
            data = Path(api(current, 'project', 'inspect', '--profile', 'compatibility')['paths']['data'])
            directory = data / 'vault'
            old_dashboard = api(legacy, 'dashboard')
            dashboard_started = True
            old_origin, old_session = dashboard_session(old_dashboard['item'])
            status, old_view = dashboard_request(old_origin, '/api/values?profile=compatibility', old_session)
            if status != 200 or len(old_view.get('items', [])) != 2:
                raise RuntimeError('Actual old dashboard positive control failed')
            old_change = {'domain': 'values', 'profile': 'compatibility', 'change': {
                'action': 'secret.set', 'key': 'TOKEN', 'env': '', 'value': SECRET,
                'revision': old_view['revision'],
                'request_id': '00000000-0000-4000-8000-000000008002'}}
            status, old_changed = dashboard_request(old_origin, '/api/actions', old_session, old_change)
            if status != 200 or old_changed.get('data', {}).get('changed') is not True:
                raise RuntimeError('Actual legacy managed receipt positive control failed')
            legacy_mac = (data / '.maintenance' / 'values-key').read_bytes()
            if len(legacy_mac) != 32:
                raise RuntimeError('Actual legacy management MAC key missing')
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
            dashboard_denials = []
            for path, body in [('/api/values?profile=compatibility', None),
                               ('/api/actions', {'domain': 'values', 'profile': 'compatibility', 'change': {
                                   'action': 'variable.set', 'key': 'URL', 'env': '', 'value': 'changed',
                                   'revision': old_view['revision'],
                                   'request_id': '00000000-0000-4000-8000-000000008001'}})]:
                status, result = dashboard_request(old_origin, path, old_session, body)
                code = result.get('error', {}).get('code')
                if status < 400 or code != 'storage_error':
                    raise RuntimeError('Actual old dashboard accepted protected-root access')
                dashboard_denials.append(code)
            if disk_state() != before:
                raise RuntimeError('Old CLI changed protected data or created a plaintext profile')
            new_dashboard = api(current, 'dashboard')['item']
            if new_dashboard['server_id'] == old_dashboard['item']['server_id']:
                raise RuntimeError('Current CLI reused an incompatible old dashboard')
            new_origin, new_session = dashboard_session(new_dashboard)
            status, _ = dashboard_request(new_origin, '/api/values?profile=compatibility', old_session)
            if status != 401:
                raise RuntimeError('Old dashboard session crossed server replacement')
            status, _ = dashboard_request(new_origin, '/api/values?profile=compatibility', new_session)
            if status != 200:
                raise RuntimeError('New dashboard cannot inspect migrated metadata')
            # A migrated legacy receipt must retain its original result without
            # overwriting a later secret. Neither secret is placed in argv.
            expected_secret = SECRET + '-newer'
            api(current, 'sec', 'set', 'TOKEN', '--profile', 'compatibility', '--stdin', input=expected_secret)
            status, replay = dashboard_request(new_origin, '/api/actions', new_session, old_change)
            if status != 200 or replay.get('data', {}).get('replayed') is not True or replay['data'].get('changed') is not True:
                raise RuntimeError('Actual legacy receipt did not replay after migration and later mutation')
            api(current, 'dashboard', 'stop')
            dashboard_started = False
            if source.read_text() != 'TOKEN=' + SECRET + '\n':
                raise RuntimeError('Migration changed original user input')
            result = subprocess.run([str(current), 'run', '--profile', 'compatibility', '--', sys.executable,
                                     '-c', 'import os,sys;expected=sys.stdin.read();sys.exit(9) if os.environ["TOKEN"] != expected else None;sys.stdout.write(os.environ["TOKEN"])'],
                                    cwd=root, env=env, input=expected_secret.encode(), capture_output=True, timeout=30)
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
                if legacy_mac in content:
                    raise RuntimeError('Plaintext legacy MAC remains in migrated data')

        finally:
            try:
                if dashboard_started:
                    api(current, 'dashboard', 'stop')
            finally:
                if directory is not None:
                    retire_vaults(directory)
    evidence = {'result': 'pass', 'baseline_commit': BASELINE, 'candidate': new_version,
                'baseline_sha256': legacy_digest,
                'candidate_sha256': hashlib.sha256(current.read_bytes()).hexdigest(),
                'denials': denials, 'dashboard_denials': dashboard_denials,
                'old_dashboard_positive_control': True, 'incompatible_dashboard_replaced': True,
                'old_session_rejected_after_replacement': True,
                'actual_legacy_receipt_replay_without_rollback': True,
                'legacy_mac_plaintext_scan': 'pass',
                'protected_json_unchanged': True, 'migrated_data_canary_scan': 'pass',
                'original_input_preserved_and_still_sensitive': True, 'owned_root_removed': True}
    args.evidence.write_text(json.dumps(evidence, indent=2) + '\n')
    print(json.dumps({'result': 'pass', 'actual_baseline': BASELINE, 'owned_root_removed': True}))


if __name__ == '__main__':
    main()
