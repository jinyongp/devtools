"""Installed one-archive transfer, metadata privacy, recovery and retry contracts."""
import concurrent.futures
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import uuid

home = Path.home()
binary = home / '.local/bin/devtools'
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install',
                '--version', '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               check=True, capture_output=True, timeout=30)
canary = 'PROFILE_SCENARIO_PRIVATE_VALUE'
transfer_passphrase = 'PROFILE_TRANSFER_PASSPHRASE_CANARY'
passphrase_input = transfer_passphrase + '\n'


def environment(name):
    root = home / name
    root.mkdir(mode=0o700)
    return dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root / 'config'),
                XDG_DATA_HOME=str(root / 'data'), XDG_CACHE_HOME=str(root / 'cache'))


source, target = environment('source'), environment('target')


def api(env, *args, input=None, error=None):
    result = subprocess.run([str(binary), *args], cwd=env['HOME'], env=env,
                            input=input, text=True, capture_output=True, timeout=30)
    assert canary not in result.stdout + result.stderr, 'profile response disclosed a value'
    assert transfer_passphrase not in result.stdout + result.stderr, 'profile response disclosed the transfer passphrase'
    assert 'AGE-SECRET-KEY-' not in result.stdout + result.stderr, 'identity disclosure'
    assert 'identity.txt' not in result.stdout + result.stderr, 'internal identity path disclosure'
    assert (result.returncode != 0) == bool(error), (args, result.returncode, result.stderr)
    assert not (result.stdout and result.stderr), 'mixed response streams'
    envelope = json.loads(result.stderr if error else result.stdout)
    assert envelope['schema_version'] == 1 and envelope['ok'] == (not error)
    if error:
        assert envelope['error']['code'] == error, envelope['error']
        return envelope['error']
    return envelope['data']


def mutate(command, target_id=None, body=None, guarded=False):
    args = ['task', *command.split()]
    if target_id:
        args.append(target_id)
    args += ['--profile', 'portable', '--request-id', str(uuid.uuid4())]
    if guarded:
        revision = api(source, 'task', 'list', '--profile', 'portable')['revision']
        args += ['--if-revision', str(revision)]
    if body is not None:
        args += ['--stdin']
    return api(source, *args, input=json.dumps(body) if body is not None else None)


assert api(source, 'version')['protocol_version'] == 5
assert api(source, 'profile', 'list')['items'] == []
assert api(source, 'backup', 'status')['item']['configured'] is False
assert api(target, 'backup', 'status')['item']['configured'] is False
api(source, 'init', '--profile', 'portable')
api(source, 'env', 'create', 'local', '--profile', 'portable')
api(source, 'var', 'set', 'MODE', '--profile', 'portable', '--value', 'development')
api(source, 'var', 'set', 'MODE', '--profile', 'portable', '--env', 'local', '--value', 'override')
api(source, 'sec', 'set', 'TOKEN', '--profile', 'portable', '--stdin', input=canary)
task = mutate('add', body={'title': 'Portable task'})['item']['id']
workstream = mutate('workstream create', body={'title': 'Portable workstream'})['item']['id']
mutate('workstream spec set', workstream, {
    'body': 'Keep the complete definition during transfer.',
    'requirements': [{'key': 'R', 'text': 'Transfer workstream state'}],
    'acceptance': [{'key': 'A', 'text': 'State is unchanged', 'requirement_keys': ['R']}],
}, guarded=True)
member = mutate('add', body={
    'title': 'Transfer member', 'workstream_id': workstream, 'acceptance_keys': ['A'],
})['item']['id']
validation = mutate('validation add', body={
    'title': 'Transfer validation', 'method': 'Compare restored state', 'task_id': member,
})['item']['id']
mutate('workstream plan set', workstream, {
    'body': 'Export, copy one archive, import.', 'task_ids': [member], 'validation_ids': [validation],
}, guarded=True)
mutate('workstream activate', workstream, guarded=True)
expected_workstream = api(source, 'task', 'workstream', 'export', workstream, '--profile', 'portable')
summary = api(source, 'profile', 'list')['items']
assert len(summary) == 1 and summary[0]['profile'] == 'portable'
assert summary[0]['values'] and summary[0]['tasks'] and summary[0]['env_count'] == 1
detail = api(source, 'profile', 'inspect', 'portable')['item']
assert detail['envs'] == ['local'] and detail['secrets'][0]['key'] == 'TOKEN'
api(target, 'profile', 'inspect', 'portable', error='profile_not_found')

# Source-first default: no destination preparation. The passphrase stays out of argv/env
# and only the encrypted archive crosses the device boundary.
outbox = Path(source['HOME']) / 'outbox'
outbox.mkdir(mode=0o700)
passphrase_file = Path(source['HOME']) / 'transfer-passphrase.txt'
passphrase_file.write_text(transfer_passphrase + '\n')
passphrase_file.chmod(0o600)
exported = api(source, 'profile', 'export', '--passphrase-file', str(passphrase_file), '--output', str(outbox))
source_archive = outbox / 'portable.age'
assert exported['item']['path'] == str(source_archive) and exported['changed']
archive_bytes = source_archive.read_bytes()
assert canary.encode() not in archive_bytes and transfer_passphrase.encode() not in archive_bytes
assert stat.S_IMODE(source_archive.stat().st_mode) == 0o600
collision = api(source, 'profile', 'export', '--passphrase-file', str(passphrase_file),
                '--output', str(outbox), error='output_exists')
assert collision['details']['path'] == str(source_archive)
assert source_archive.read_bytes() == archive_bytes

# Exactly one filesystem payload crosses the device boundary. Never copy a key or config.
inbox = Path(target['HOME']) / 'inbox'
inbox.mkdir(mode=0o700)
archive = inbox / source_archive.name
before_copy = {path.relative_to(Path(target['HOME'])) for path in Path(target['HOME']).rglob('*') if path.is_file()}
shutil.copy2(source_archive, archive)
after_copy = {path.relative_to(Path(target['HOME'])) for path in Path(target['HOME']).rglob('*') if path.is_file()}
assert after_copy - before_copy == {Path('inbox/portable.age')}
assert list(inbox.iterdir()) == [archive] and archive.read_bytes() == archive_bytes
assert not (Path(target['HOME']) / 'devtools.toml').exists()

base = ['profile', 'import', '--file', str(archive), '--passphrase-stdin']
preview = api(target, *base, input=passphrase_input)
assert not preview['changed'] and not preview['replayed'] and not preview['target_exists']
assert preview['diff']['different'] and api(target, 'profile', 'list')['items'] == []
apply = [*base, '--apply', preview['digest'], '--request-id', str(uuid.uuid4())]
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
    results = list(pool.map(lambda _: api(target, *apply, input=passphrase_input), range(2)))
assert sorted(item['replayed'] for item in results) == [False, True]
assert all(item['changed'] and item['diff'] == preview['diff'] for item in results)
assert api(target, *apply, input=passphrase_input)['replayed']
api(target, *apply, '--as', 'changed-target', input=passphrase_input, error='request_conflict')
assert api(target, 'task', 'show', task, '--profile', 'portable')['item']['id'] == task
assert api(target, 'task', 'workstream', 'export', workstream, '--profile', 'portable') == expected_workstream
assert api(target, 'var', 'get', 'MODE', '--profile', 'portable', '--env', 'local')['value'] == 'override'
# Compare a synthetic secret inside the child without printing either value.
secret_check = subprocess.run(
    [str(binary), 'command', 'run', '--profile', 'portable', '--', '/bin/sh', '-c',
     'test "$TOKEN" = "$EXPECTED_TRANSFER_TOKEN"'],
    env=dict(target, EXPECTED_TRANSFER_TOKEN=canary), cwd=target['HOME'],
    text=True, capture_output=True, timeout=30,
)
assert secret_check.returncode == 0 and secret_check.stdout == '' and secret_check.stderr == '', 'secret round trip failed'
assert not (Path(target['HOME']) / 'devtools.toml').exists()

# Whole-profile replacement remains explicit, stale-guarded and safety-backed.
preview = api(target, *base, input=passphrase_input)
api(target, *base, '--apply', preview['digest'], '--request-id', str(uuid.uuid4()), input=passphrase_input, error='profile_exists')
api(target, 'var', 'set', 'MODE', '--profile', 'portable', '--value', 'changed')
api(target, *base, '--replace', '--apply', preview['digest'], '--request-id', str(uuid.uuid4()), input=passphrase_input, error='revision_conflict')
preview = api(target, *base, input=passphrase_input)
assert not preview['diff']['different'], 'value-only difference must not be exposed'
replace = [*base, '--replace', '--apply', preview['digest'], '--request-id', str(uuid.uuid4())]
replaced = api(target, *replace, input=passphrase_input)
assert replaced['changed'] and replaced['safety_backup']
safety = Path(replaced['safety_backup'])
assert safety.is_relative_to(Path(target['HOME'])) and safety.name.startswith('before-import-')
assert stat.S_IMODE(safety.stat().st_mode) == 0o600
assert api(target, *replace, input=passphrase_input)['safety_backup'] == str(safety), 'retry created a different recovery archive'
assert api(target, 'var', 'get', 'MODE', '--profile', 'portable')['value'] == 'development'
recovery = ['profile', 'import', '--file', str(safety), '--as', 'recovered', '--passphrase-stdin']
recovery_preview = api(target, *recovery, input=passphrase_input)
api(target, *recovery, '--apply', recovery_preview['digest'], '--request-id', str(uuid.uuid4()), input=passphrase_input)
assert api(target, 'var', 'get', 'MODE', '--profile', 'recovered')['value'] == 'changed'

copy_base = [*base, '--as', 'copy']
copy_preview = api(target, *copy_base, input=passphrase_input)
api(target, *copy_base, '--apply', copy_preview['digest'], '--request-id', str(uuid.uuid4()), input=passphrase_input)
api(target, 'sec', 'set', 'TOKEN', '--profile', 'copy', '--stdin', input=canary + '-different')
difference = api(target, 'profile', 'diff', 'portable', 'copy')
assert not difference['different'] and not difference['secrets']
api(target, 'env', 'create', 'staging', '--profile', 'copy')
difference = api(target, 'profile', 'diff', 'portable', 'copy')
assert difference['envs'] == [{'name': 'staging', 'action': 'added'}]
# Advanced recipient compatibility remains available and is no longer the default flow.
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
    prepared = list(pool.map(lambda _: api(target, 'profile', 'transfer', 'prepare'), range(2)))
assert sorted(item['changed'] for item in prepared) == [False, True]
public = prepared[0]['item']['recipient']
assert public.startswith('age1') and prepared[1]['item']['recipient'] == public
recipient_archive = outbox / 'recipient-portable.age'
api(source, 'profile', 'export', '--profile', 'portable', '--recipient', public, '--output', str(recipient_archive))
recipient_preview = api(target, 'profile', 'import', '--file', str(recipient_archive), '--as', 'recipient-copy')
api(target, 'profile', 'import', '--file', str(recipient_archive), '--as', 'recipient-copy',
    '--apply', recipient_preview['digest'], '--request-id', str(uuid.uuid4()))
assert api(target, 'var', 'get', 'MODE', '--profile', 'recipient-copy')['value'] == 'development'
api(source, 'profile', 'export', '--profile', 'portable', '--recipient', public,
    '--passphrase-file', str(passphrase_file), '--output', str(outbox / 'invalid.age'), error='invalid_argument')

for env in (source, target):
    assert api(env, 'backup', 'status')['item']['configured'] is False
    assert not list(Path(env['HOME']).rglob('backup.json'))
print('Installed source-first one-archive passphrase transfer, recovery, advanced recipient compatibility, privacy and concurrent replay passed')
