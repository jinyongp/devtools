"""Exchange synthetic encrypted profiles using already-built native CLI images."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import uuid

from vault_test_support import retire_vaults

PASSPHRASE = 'synthetic-cross-platform-fixture-passphrase\n'
SECRET = 'synthetic-cross-platform-secret\n'
VALUE = '한글 / café / C:\\Users\\fixture / /home/fixture'
ORIGINS = {'linux', 'macos', 'windows-amd64', 'windows-arm64'}


def cli(binary, root, *args, input=None, error=None):
    env = dict(os.environ, DEVTOOLS_ISOLATED_ROOT=str(root))
    env.pop('DEVTOOLS_TASK_CONTEXT', None)
    # Binary stdin keeps fixture secret bytes identical on Windows and Unix.
    result = subprocess.run([str(binary), *args], cwd=root, env=env,
                            input=input.encode('utf-8') if input is not None else None,
                            capture_output=True, timeout=60)
    stdout, stderr = result.stdout.decode('utf-8'), result.stderr.decode('utf-8')
    assert (result.returncode != 0) == bool(error), (args[:3], stderr)
    envelope = json.loads(stderr if error else stdout)
    assert envelope['ok'] == (not error) and envelope['schema_version'] == 1
    if error:
        assert envelope['error']['code'] == error, envelope['error']
        return envelope['error']
    assert SECRET.strip() not in stdout, 'secret disclosed in response'
    return envelope['data']


def export_fixture(binary, root, output, origin, commit):
    output.mkdir(parents=True, exist_ok=False)
    def api(*args, **kwargs):
        return cli(binary, root, *args, '--profile', 'portable', **kwargs)
    def mutate(*args, body):
        return api('task', *args, '--stdin', '--request-id', str(uuid.uuid4()), input=json.dumps(body))
    api('init')
    api('env', 'create', 'local')
    api('var', 'set', 'MESSAGE', '--value', VALUE)
    api('var', 'set', 'MESSAGE', '--env', 'local', '--value', 'environment override')
    cli(binary, root, 'vault', 'init', '--backend', 'passphrase', '--passphrase-stdin', input='synthetic-local-vault-unlock\n')
    api('sec', 'set', 'TOKEN', '--stdin', input=SECRET)
    for name, keys in [('frontend', ['MESSAGE']), ('backend', ['MESSAGE', 'TOKEN']),
                       ('deploy', ['TOKEN'])]:
        revision = api('bundle', 'list')['revision']
        members = [arg for key in keys for arg in ('--key', key)]
        assert api('bundle', 'set', name, '--revision', revision,
                   '--request-id', str(uuid.uuid4()), *members)['changed']
    source_bundles = api('bundle', 'list')['items']
    assert len(source_bundles) == 3 and all(b['value_ids'] for b in source_bundles)
    source_vault = cli(binary, root, 'vault', 'status')['item']['vault_id']
    def input_identity(*options):
        current = api('values', 'inspect', *options)['values']['current']
        assert current is not None and current['integrity_verified'] is True
        return dict(identity=current['identity'], slots=current['slots'])
    source_inputs = dict(common=input_identity(), local=input_identity('--env', 'local'))
    for name in ('frontend', 'backend', 'deploy'):
        source_inputs[name] = input_identity('--bundle', name, '--env', 'local')
    task = mutate('add', body={'title': 'Cross-platform task 한글'})['item']['id']
    workstream = mutate('workstream', 'create', body={'title': 'Portable workstream'})['item']['id']
    validation = mutate('validation', 'add', body={'task_id': task, 'title': 'Portable validation',
                                                 'method': 'Read after native import'})['item']['id']
    expected = api('task', 'workstream', 'export', workstream)
    archive = output / 'portable.age'
    api('profile', 'export', '--passphrase-stdin', '--output', str(archive), input=PASSPHRASE)
    encrypted = archive.read_bytes()
    assert SECRET.strip().encode() not in encrypted and PASSPHRASE.strip().encode() not in encrypted
    shutil.copyfile(binary, output / 'cli.bin')
    manifest = dict(commit=commit, origin=origin, task=task, workstream=workstream,
                    validation=validation, expected_workstream=expected,
                    source_vault_id=source_vault, source_inputs=source_inputs,
                    source_bundles=source_bundles,
                    archive_sha256=hashlib.sha256(encrypted).hexdigest(),
                    binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest())
    (output / 'manifest.json').write_text(json.dumps(manifest, ensure_ascii=False), encoding='utf-8')


def private_archive_copy(path, body):
    """Actions artifacts discard private mode/ACL metadata; restore it on our copy."""
    path.write_bytes(body)
    if os.name != 'nt':
        path.chmod(0o600)
        return
    script = r"""
$ErrorActionPreference = 'Stop'
$user = [Security.Principal.WindowsIdentity]::GetCurrent().User
$system = [Security.Principal.SecurityIdentifier]::new('S-1-5-18')
$acl = [Security.AccessControl.FileSecurity]::new()
$acl.SetOwner($user)
$acl.SetAccessRuleProtection($true, $false)
foreach ($sid in @($user, $system)) {
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($sid, 'FullControl', 'Allow'))
}
Set-Acl -LiteralPath $env:DEVTOOLS_EXCHANGE_ARCHIVE -AclObject $acl
"""
    subprocess.run(['pwsh', '-NoProfile', '-NonInteractive', '-Command', script],
                   env=dict(os.environ, DEVTOOLS_EXCHANGE_ARCHIVE=str(path)),
                   check=True, capture_output=True, text=True, encoding='utf-8', timeout=30)


def import_fixtures(root, fixtures, origin, commit):
    own = fixtures / ('profile-exchange-' + origin)
    identity = json.loads((own / 'manifest.json').read_text(encoding='utf-8'))
    image = own / 'cli.bin'
    assert identity['commit'] == commit and identity['origin'] == origin
    assert hashlib.sha256(image.read_bytes()).hexdigest() == identity['binary_sha256']
    binary = root / ('devtools.exe' if os.name == 'nt' else 'devtools')
    shutil.copyfile(image, binary)
    binary.chmod(0o700)
    passed = []
    for source in sorted(ORIGINS - {origin}):
        directory = fixtures / ('profile-exchange-' + source)
        manifest = json.loads((directory / 'manifest.json').read_text(encoding='utf-8'))
        assert manifest['commit'] == commit and manifest['origin'] == source
        encrypted = (directory / 'portable.age').read_bytes()
        assert hashlib.sha256(encrypted).hexdigest() == manifest['archive_sha256']
        target = root / source
        target.mkdir()
        archive = target / 'portable.age'
        private_archive_copy(archive, encrypted)
        def api(*args, **kwargs):
            return cli(binary, target, *args, '--profile', 'portable', **kwargs)
        cli(binary, target, 'vault', 'init', '--backend', 'passphrase', '--passphrase-stdin', input='synthetic-destination-vault-unlock\n')
        base = ['profile', 'import', '--file', str(archive), '--passphrase-stdin']
        preview = api(*base, input=PASSPHRASE)
        request = str(uuid.uuid4())
        apply = [*base, '--apply', preview['digest'], '--request-id', request]
        assert api(*apply, input=PASSPHRASE)['changed']
        assert api(*apply, input=PASSPHRASE)['replayed']
        local_vault = cli(binary, target, 'vault', 'status')['item']['vault_id']
        assert local_vault != manifest['source_vault_id'], 'source vault identity was transferred'
        selections = [('common', []), ('local', ['--env', 'local'])]
        selections += [(name, ['--bundle', name, '--env', 'local'])
                       for name in ('frontend', 'backend', 'deploy')]
        for env, options in selections:
            current = api('values', 'inspect', *options)['values']['current']
            assert current is not None and current['integrity_verified'] is True
            original = manifest['source_inputs'][env]
            assert current['identity']['profile_id'] != original['identity']['profile_id']
            assert current['identity']['storage_generation'] != original['identity']['storage_generation']
            assert current['slots'] == original['slots'], 'logical value/slot/version/source changed in transfer'
        assert api('bundle', 'list')['items'] == manifest['source_bundles'], 'bundle identity/membership changed in transfer'
        assert api('var', 'get', 'MESSAGE')['value'] == VALUE, 'path strings must remain literal'
        assert api('var', 'get', 'MESSAGE', '--env', 'local')['value'] == 'environment override'
        assert api('task', 'show', manifest['task'])['item']['title'] == 'Cross-platform task 한글'
        assert api('task', 'workstream', 'export', manifest['workstream']) == manifest['expected_workstream']
        assert api('task', 'validation', 'show', manifest['validation'])['item']['task_id'] == manifest['task']
        assert cli(binary, target, 'backup', 'status')['item']['configured'] is False
        # The CLI injects the restored secret into a child; no secret-reveal API is used.
        result = subprocess.run([str(binary), 'run', '--profile', 'portable', '--', sys.executable,
                                 '-c', 'import os,sys; assert os.environ["TOKEN"].encode("utf-8") == sys.stdin.buffer.read()'],
                                cwd=target, env=dict(os.environ, DEVTOOLS_ISOLATED_ROOT=str(target)),
                                input=SECRET.encode('utf-8'),
                                capture_output=True, timeout=60)
        assert result.returncode == 0, result.stderr
        passed.append(source + ' -> ' + origin)
    return dict(commit=commit, target=origin, passed=passed,
                fresh_target_vault_profile_generation=True, logical_slot_versions_preserved=True,
                nonempty_bundle_ids_members_and_selected_slots_preserved=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('mode', choices=['export', 'import'])
    parser.add_argument('--origin', required=True, choices=sorted(ORIGINS))
    parser.add_argument('--commit', required=True)
    parser.add_argument('--binary', type=Path)
    parser.add_argument('--directory', required=True, type=Path)
    parser.add_argument('--evidence', type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='devtools-profile-exchange-') as temporary:
        root = Path(temporary)
        try:
            if args.mode == 'export':
                export_fixture(args.binary.resolve(), root, args.directory.resolve(), args.origin, args.commit)
            else:
                result = import_fixtures(root, args.directory.resolve(), args.origin, args.commit)
                args.evidence.write_text(json.dumps(result, indent=2) + '\n', encoding='utf-8')
                print(json.dumps(result))
        finally:
            retire_vaults(root / 'data' / 'vault', *(p / 'data' / 'vault' for p in root.iterdir() if p.is_dir()))


if __name__ == '__main__':
    main()
