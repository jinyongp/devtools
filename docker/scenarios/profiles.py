"""Installed profile catalog, metadata privacy, transfer preview and retry contracts."""
import concurrent.futures
import json
import os
from pathlib import Path
import subprocess
import uuid

home = Path.home()
binary = home / '.local/bin/devtools'
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install',
                '--version', '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               check=True, capture_output=True, timeout=30)
canary = 'PROFILE_SCENARIO_PRIVATE_VALUE'


def environment(name):
    root = home / name
    root.mkdir(mode=0o700)
    return dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root / 'config'),
                XDG_DATA_HOME=str(root / 'data'), XDG_CACHE_HOME=str(root / 'cache'))


source, target = environment('source'), environment('target')


def api(env, *args, input=None, error=None):
    result = subprocess.run([str(binary), *args], env=env, input=input, text=True,
                            capture_output=True, timeout=30)
    assert canary not in result.stdout + result.stderr, 'profile response disclosed a value'
    assert 'AGE-SECRET-KEY-' not in result.stdout + result.stderr, 'identity disclosure'
    assert (result.returncode != 0) == bool(error), (args, result.returncode, result.stderr)
    assert not (result.stdout and result.stderr), 'mixed response streams'
    envelope = json.loads(result.stderr if error else result.stdout)
    assert envelope['schema_version'] == 1 and envelope['ok'] == (not error)
    if error:
        assert envelope['error']['code'] == error, envelope['error']
        return envelope['error']
    return envelope['data']


assert api(source, 'version')['protocol_version'] == 3
assert api(source, 'profile', 'list')['items'] == []
assert api(target, 'backup', 'status')['item']['configured'] is False
api(source, 'env', 'create', 'local', '--profile', 'portable')
api(source, 'var', 'set', 'MODE', '--profile', 'portable', '--value', 'development')
api(source, 'sec', 'set', 'TOKEN', '--profile', 'portable', '--stdin', input=canary)
task = api(source, 'task', 'add', '--profile', 'portable', '--title', 'Portable task',
           '--request-id', str(uuid.uuid4()))['item']['id']
summary = api(source, 'profile', 'list')['items']
assert len(summary) == 1 and summary[0]['profile'] == 'portable'
assert summary[0]['values'] and summary[0]['tasks'] and summary[0]['env_count'] == 1
detail = api(source, 'profile', 'inspect', 'portable')['item']
assert detail['envs'] == ['local'] and detail['secrets'][0]['key'] == 'TOKEN'
api(target, 'profile', 'inspect', 'portable', error='profile_not_found')

identity, recipient = home / 'identity', home / 'recipient'
api(target, 'backup', 'keygen', '--identity-file', str(identity), '--recipient-file', str(recipient))
api(target, 'backup', 'configure', '--directory', str(home / 'backups'), '--recipient-file', str(recipient))
public = api(target, 'backup', 'status')['item']['recipient']
assert public.startswith('age1')
archive = home / 'portable.age'
api(source, 'profile', 'export', '--profile', 'portable', '--output', str(archive), '--recipient', public)
assert canary.encode() not in archive.read_bytes()
api(source, 'profile', 'export', '--profile', 'portable', '--output', str(home / 'unused.age'),
    '--recipient', public, '--recipient-file', str(recipient), error='invalid_argument')
assert not (home / 'unused.age').exists()
base = ['profile', 'import', '--file', str(archive), '--identity-file', str(identity)]
preview = api(target, *base)
assert not preview['changed'] and not preview['replayed'] and not preview['target_exists']
assert preview['diff']['different'] and api(target, 'profile', 'list')['items'] == []
apply = [*base, '--apply', preview['digest'], '--request-id', str(uuid.uuid4())]
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
    results = list(pool.map(lambda _: api(target, *apply), range(2)))
assert sorted(item['replayed'] for item in results) == [False, True]
assert all(item['changed'] and item['diff'] == preview['diff'] for item in results)
assert api(target, *apply)['replayed']
api(target, *apply, '--as', 'changed-target', error='request_conflict')
assert api(target, 'task', 'show', task, '--profile', 'portable')['item']['id'] == task

# Whole-profile replacement is explicit, stale-guarded, and safety-backed.
preview = api(target, *base)
api(target, *base, '--apply', preview['digest'], '--request-id', str(uuid.uuid4()), error='profile_exists')
api(target, 'var', 'set', 'MODE', '--profile', 'portable', '--value', 'changed')
api(target, *base, '--replace', '--apply', preview['digest'], '--request-id', str(uuid.uuid4()), error='revision_conflict')
preview = api(target, *base)
assert not preview['diff']['different'], 'value-only difference must not be exposed'
replaced = api(target, *base, '--replace', '--apply', preview['digest'], '--request-id', str(uuid.uuid4()))
assert replaced['changed'] and replaced['safety_backup']
api(target, 'backup', 'inspect', '--file', replaced['safety_backup'], '--identity-file', str(identity))
assert api(target, 'var', 'get', 'MODE', '--profile', 'portable')['value'] == 'development'

copy_base = [*base, '--as', 'copy']
copy_preview = api(target, *copy_base)
api(target, *copy_base, '--apply', copy_preview['digest'], '--request-id', str(uuid.uuid4()))
api(target, 'sec', 'set', 'TOKEN', '--profile', 'copy', '--stdin', input=canary + '-different')
difference = api(target, 'profile', 'diff', 'portable', 'copy')
assert not difference['different'] and not difference['secrets']
api(target, 'env', 'create', 'staging', '--profile', 'copy')
difference = api(target, 'profile', 'diff', 'portable', 'copy')
assert difference['envs'] == [{'name': 'staging', 'action': 'added'}]
print('Installed profile discovery, metadata privacy, direct recipient, preview/apply, stale protection and concurrent replay passed')
