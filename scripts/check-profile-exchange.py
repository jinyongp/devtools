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

PASSPHRASE = 'synthetic-cross-platform-fixture-passphrase\n'
SECRET = 'synthetic-cross-platform-secret\n'
VALUE = '한글 / café / C:\\Users\\fixture / /home/fixture'
ORIGINS = {'linux', 'macos', 'windows-amd64', 'windows-arm64'}


def cli(binary, root, *args, input=None, error=None):
    env = dict(os.environ, DEVTOOLS_ISOLATED_ROOT=str(root))
    env.pop('DEVTOOLS_TASK_CONTEXT', None)
    result = subprocess.run([str(binary), *args], cwd=root, env=env, input=input,
                            capture_output=True, text=True, encoding="utf-8", timeout=60)
    assert (result.returncode != 0) == bool(error), (args[:3], result.stderr)
    envelope = json.loads(result.stderr if error else result.stdout)
    assert envelope['ok'] == (not error) and envelope['schema_version'] == 1
    if error:
        assert envelope['error']['code'] == error, envelope['error']
        return envelope['error']
    assert SECRET.strip() not in result.stdout, 'secret disclosed in response'
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
    api('sec', 'set', 'TOKEN', '--stdin', input=SECRET)
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
                    archive_sha256=hashlib.sha256(encrypted).hexdigest(),
                    binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest())
    (output / 'manifest.json').write_text(json.dumps(manifest, ensure_ascii=False), encoding='utf-8')


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
        archive = directory / 'portable.age'
        assert hashlib.sha256(archive.read_bytes()).hexdigest() == manifest['archive_sha256']
        target = root / source
        target.mkdir()
        def api(*args, **kwargs):
            return cli(binary, target, *args, '--profile', 'portable', **kwargs)
        base = ['profile', 'import', '--file', str(archive), '--passphrase-stdin']
        preview = api(*base, input=PASSPHRASE)
        request = str(uuid.uuid4())
        apply = [*base, '--apply', preview['digest'], '--request-id', request]
        assert api(*apply, input=PASSPHRASE)['changed']
        assert api(*apply, input=PASSPHRASE)['replayed']
        assert api('var', 'get', 'MESSAGE')['value'] == VALUE, 'path strings must remain literal'
        assert api('var', 'get', 'MESSAGE', '--env', 'local')['value'] == 'environment override'
        assert api('task', 'show', manifest['task'])['item']['title'] == 'Cross-platform task 한글'
        assert api('task', 'workstream', 'export', manifest['workstream']) == manifest['expected_workstream']
        assert api('task', 'validation', 'show', manifest['validation'])['item']['task_id'] == manifest['task']
        assert cli(binary, target, 'backup', 'status')['item']['configured'] is False
        # The CLI injects the restored secret into a child; no secret-reveal API is used.
        result = subprocess.run([str(binary), 'run', '--profile', 'portable', '--', sys.executable,
                                 '-c', 'import os; assert os.environ["TOKEN"] == ' + repr(SECRET)],
                                cwd=target, env=dict(os.environ, DEVTOOLS_ISOLATED_ROOT=str(target)),
                                capture_output=True, text=True, encoding="utf-8", timeout=60)
        assert result.returncode == 0, result.stderr
        passed.append(source + ' -> ' + origin)
    return dict(commit=commit, target=origin, passed=passed)


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
        if args.mode == 'export':
            export_fixture(args.binary.resolve(), root, args.directory.resolve(), args.origin, args.commit)
        else:
            result = import_fixtures(root, args.directory.resolve(), args.origin, args.commit)
            args.evidence.write_text(json.dumps(result, indent=2) + '\n', encoding='utf-8')
            print(json.dumps(result))


if __name__ == '__main__':
    main()
