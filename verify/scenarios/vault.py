"""Installed public-to-protected values, reusable sessions and passive metadata."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

home = Path.home()
project = home / 'project'
project.mkdir()
env = dict(os.environ, PATH=str(home / '.local/bin') + os.pathsep + os.environ['PATH'])
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install', '--version',
                '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               cwd=project, env=env, capture_output=True, check=True, timeout=30)
binary = str(home / '.local/bin/devtools')
secret = 'synthetic-installed-vault-secret'
passphrase = 'synthetic-installed-vault-passphrase'
replacement_passphrase = 'synthetic-installed-vault-replacement'


def invoke(*args, input=None, error=None):
    result = subprocess.run([binary, *args], cwd=project, env=env, input=input,
                            text=True, capture_output=True, timeout=20)
    assert secret not in result.stdout + result.stderr
    assert passphrase not in result.stdout + result.stderr
    assert replacement_passphrase not in result.stdout + result.stderr
    assert not (result.stdout and result.stderr), 'mixed response streams'
    envelope = json.loads(result.stderr if error else result.stdout)
    assert envelope['schema_version'] == 1 and envelope['ok'] == (error is None)
    if error:
        assert result.returncode != 0 and envelope['error']['code'] == error
        return envelope['error']
    assert result.returncode == 0, (args[:2], result.returncode)
    return envelope['data']


assert invoke('vault', 'status')['item']['state'] == 'uninitialized'
invoke('init', '--profile', 'vault-fixture')
invoke('var', 'set', 'PUBLIC', '--value', 'public value')
public = invoke('values', 'inspect')['values']['current']
assert public is not None and public['integrity_verified'] is False
assert [slot['name'] for slot in public['slots']] == ['PUBLIC']
invoke('sec', 'set', 'TOKEN', '--stdin', input=secret, error='key_missing')
# No implicit terminal input or wait in a noninteractive invocation.
invoke('vault', 'init', '--backend', 'passphrase', error='passphrase_required')
invoke('vault', 'init', '--backend', 'passphrase', '--passphrase-stdin', input=passphrase + '\n')
invoke('sec', 'set', 'TOKEN', '--stdin', input=secret)
initial = invoke('vault', 'status')['item']
assert initial['state'] == 'unlocked'
assert invoke('var', 'get', 'PUBLIC')['integrity_verified'] is False
assert invoke('sec', 'list')['items'][0]['key'] == 'TOKEN'
# Register shared membership once, then select the reusable bundle on execution.
assignments = home / 'bundle.env'
assignments.write_text('PUBLIC=public value\nTOKEN=' + secret + '\n')
assignments.chmod(0o600)
invoke('import', '--file', str(assignments), '--var', 'PUBLIC', '--bundle', 'backend')
bundles = invoke('bundle', 'list')
invoke('bundle', 'set', 'frontend', '--key', 'PUBLIC', '--revision', bundles['revision'],
       '--request-id', '00000000-0000-4000-8000-000000000996')
invoke('bundle', 'set', 'deploy', '--key', 'TOKEN',
       '--revision', invoke('bundle', 'list')['revision'], '--request-id', str(uuid.uuid4()))
selected_env = dict(env)
selected_env.pop('TOKEN', None)
result = subprocess.run([binary, 'run', '--bundle', 'frontend', '--', 'sh', '-c',
                         'test -z "${TOKEN+x}" && printf %s "$PUBLIC"'], cwd=project,
                        env=selected_env, capture_output=True, timeout=20)
assert result.returncode == 0 and result.stdout == b'public value' and not result.stderr
result = subprocess.run([binary, 'run', '--bundle', 'backend', '--', 'sh', '-c', 'printf %s "$TOKEN"'],
                        cwd=project, env=selected_env, capture_output=True, timeout=20)
assert result.returncode == 0 and result.stdout == b'[REDACTED]' and not result.stderr
deploy = invoke('values', 'inspect', '--bundle', 'deploy')['values']['current']
assert deploy['integrity_verified'] is True
assert [slot['name'] for slot in deploy['slots']] == ['TOKEN']

# The managed path persists an explicit selection across restart, including
# when the command's configured default changes in between executions.
original_config = (project / 'devtools.toml').read_text()
observation = project / 'bundle-observed.json'
server = project / 'bundle-server.py'
server.write_text("import json,os,time\nfrom pathlib import Path\np=Path(" + repr(str(observation)) + ")\n"
                  "t=p.with_suffix('.tmp')\nt.write_text(json.dumps({'public':os.getenv('PUBLIC'),'token_present':'TOKEN' in os.environ}))\n"
                  "os.replace(t,p)\ntime.sleep(120)\n")
config = ('profile="vault-fixture"\nbundle="none"\n[commands.bundle-server]\ninject=true\nbundle="backend"\nexec='
          + json.dumps([sys.executable, str(server)]) + '\n[commands.bundle-server.ready]\nexec='
          + json.dumps([sys.executable, '-c', 'from pathlib import Path;raise SystemExit(not Path(' + repr(str(observation)) + ').exists())']) + '\ntimeout="5s"\n')
(project / 'devtools.toml').write_text(config)
managed = None
managed_env = dict(env)
managed_env.pop('TOKEN', None)
managed_env.pop('PUBLIC', None)
def managed_api(*args, error=None):
    result = subprocess.run([binary,*args],cwd=project,env=managed_env,capture_output=True,text=True,timeout=20)
    assert secret not in result.stdout + result.stderr
    if error:
        assert result.returncode and json.loads(result.stderr)['error']['code']==error
        return json.loads(result.stderr)['error']
    assert not result.returncode and not result.stderr
    return json.loads(result.stdout)['data']
def observed():
    deadline=time.monotonic()+5
    while time.monotonic()<deadline:
        try:return json.loads(observation.read_text())
        except (FileNotFoundError,json.JSONDecodeError):time.sleep(.02)
    raise AssertionError('Managed bundle execution did not publish its public observation')
try:
    first=managed_api('process','start','bundle-server','--bundle','frontend','--request-id',str(uuid.uuid4()))
    managed=first['item']['id']
    assert first['item']['selection']=='frontend' and first['item']['bundle_override']=='frontend'
    assert observed()=={'public':'public value','token_present':False}
    # Actual installed metadata APIs distinguish configured selection from the
    # frozen delivered override, without returning values or extending idle.
    before_queries = invoke('vault', 'status')['item']
    inspected = managed_api('values', 'inspect', '--bundle', 'frontend')
    usages = managed_api('values', 'uses')
    configured = managed_api('command', 'inspect', 'bundle-server')
    assert configured['item']['values_update'] == 'notify'
    assert configured['item']['selection'] == 'backend'
    assert inspected['values']['current']['slots'] == first['item']['input_version']['slots']
    declared = [item for item in usages['items'] if item['kind'] == 'declared']
    delivered = [item for item in usages['items'] if item['kind'] == 'delivered']
    assert len(declared) == len(delivered) == 1
    assert declared[0]['selection'] == 'backend'
    assert delivered[0]['selection'] == 'frontend' and delivered[0]['execution_id'] == managed
    assert delivered[0]['values']['state'] == 'current'
    metadata = json.dumps([inspected, usages, configured])
    for forbidden in [secret, passphrase, 'public value', 'session_id', 'challenge', 'ciphertext', 'launch_id']:
        assert forbidden not in metadata, 'metadata exposed value or authority'
    for command, fields in [('values inspect', {'profile','env','selection','values'}),
                            ('values uses', {'profile','items'}),
                            ('bundle list', {'profile','revision','integrity_verified','items'})]:
        schema = managed_api('schema', *command.split())
        assert schema['output_mode'] == 'json'
        assert fields <= set(schema['output_schema']['required'])
    assert invoke('vault', 'status')['item'] == before_queries
    # Default notify preserves the actual app. Unselected secret changes do not
    # change its effective input; a selected public change does report pending.
    invoke('sec', 'set', 'TOKEN', '--stdin', input=secret+'-unselected')
    assert managed_api('process', 'status', managed)['values']['state'] == 'current'
    invoke('var', 'set', 'PUBLIC', '--value', 'selected changed')
    deadline = time.monotonic() + 10
    while True:
        status = managed_api('process', 'status', managed)
        if status['item'].get('value_watch', {}).get('state') == 'pending':
            break
        assert time.monotonic() < deadline, 'notify watcher did not converge'
        time.sleep(.05)
    assert status['values']['state'] == 'pending' and status['item']['state'] == 'running'
    assert not status['item'].get('value_apply')
    assert len(managed_api('process', 'list')['items']) == 1
    assert observed() == {'public':'public value','token_present':False}
    invoke('var', 'set', 'PUBLIC', '--value', 'public value')
    invoke('sec', 'set', 'TOKEN', '--stdin', input=secret)
    (project / 'devtools.toml').write_text(config.replace('bundle="backend"','bundle="none"'))
    observation.unlink()
    restarted=managed_api('process','restart',managed,'--request-id',str(uuid.uuid4()))
    managed=restarted['item']['id']
    assert restarted['item']['selection']=='frontend' and observed()=={'public':'public value','token_present':False}
    observation.unlink()
    batch=managed_api('project','restart','bundle-server','--timeout','5s','--request-id',str(uuid.uuid4()))
    managed=batch['items'][0]['item']['id']
    assert batch['items'][0]['item']['selection']=='frontend' and observed()=={'public':'public value','token_present':False}
    observation.unlink()
    empty_batch=managed_api('project','restart','bundle-server','--bundle','none','--timeout','5s','--request-id',str(uuid.uuid4()))
    empty={'item':empty_batch['items'][0]['item']}
    managed=empty['item']['id']
    assert empty['item']['selection']=='none' and observed()=={'public':None,'token_present':False}
    blocked = managed_api('process','restart',managed,'--bundle','missing','--request-id',str(uuid.uuid4()),error='requirements_failed')
    assert any(cause['code'] == 'bundle_not_found' for cause in blocked['details']['causes']), blocked
    assert managed_api('process','status',managed)['item']['state']=='running'
finally:
    if managed is not None:
        managed_api('process','stop',managed,'--request-id',str(uuid.uuid4()))
    (project / 'devtools.toml').write_text(original_config)

invoke('schema', 'vault', 'unlock')
invoke('schema', 'sec', 'set')
for unavailable in [('sec', 'get', 'TOKEN')]:
    result = subprocess.run([binary, *unavailable], cwd=project, env=env,
                            text=True, capture_output=True, timeout=10)
    assert result.returncode == 2 and secret not in result.stdout + result.stderr
# Bundle selection is a separate option from env; no per-command key list is needed.
result = subprocess.run([binary, 'run', '--bundle', 'none', '--', 'true'], cwd=project,
                        env=env, text=True, capture_output=True, timeout=20)
assert result.returncode == 0 and not result.stdout and not result.stderr
blocked = invoke('run', '--bundle', 'missing', '--', 'true', error='requirements_failed')
assert any(cause['code'] == 'bundle_not_found' for cause in blocked['details']['causes']), blocked
invoke('vault', 'lock')
locked = invoke('vault', 'status')['item']
assert locked['state'] == 'locked_manual'
for selection in ['frontend', 'none']:
    inspection = invoke('values', 'inspect', '--bundle', selection)['values']
    assert inspection['state'] == 'verification_pending' and inspection['current'] is None
assert invoke('var', 'get', 'PUBLIC')['value'] == 'public value'
invoke('sec', 'list')
invoke('diagnostics')
# The dynamic completion protocol is value-free and remains usable when locked.
completed = subprocess.run([binary, '__complete'], cwd=project,
                           env=env, input='sec\0unset\0TO\0', text=True, capture_output=True, timeout=10)
assert completed.returncode == 0 and secret not in completed.stdout + completed.stderr
assert invoke('vault', 'status')['item'] == locked
invoke('var', 'set', 'PUBLIC', '--value', 'changed', error='vault_locked')
invoke('vault', 'unlock', error='passphrase_required')
invoke('vault', 'unlock', '--passphrase-stdin', input='wrong\n', error='integrity_failed')
invoke('vault', 'unlock', '--passphrase-stdin', input=passphrase + '\n')
invoke('vault', 'recover')
# Real installed child output uses the same session without another passphrase.
result = subprocess.run([binary, 'run', '--', 'sh', '-c', 'printf %s "$TOKEN"'],
                        cwd=project, env=env, capture_output=True, timeout=20)
assert result.returncode == 0 and result.stdout == b'[REDACTED]' and not result.stderr
replacement_file = home / 'replacement-vault-passphrase'
replacement_file.write_text(replacement_passphrase + '\n')
replacement_file.chmod(0o600)
rewrap_id = str(uuid.uuid4())
changed = invoke('vault', 'rewrap', '--from-backend', 'passphrase', '--backend', 'passphrase',
                 '--request-id', rewrap_id, '--current-passphrase-stdin',
                 '--new-passphrase-file', str(replacement_file), input=passphrase + '\n')
assert changed['changed'] and not changed['replayed'] and changed['item']['state'] == 'locked'
invoke('vault', 'unlock', '--passphrase-stdin', input=passphrase + '\n', error='integrity_failed')
invoke('vault', 'unlock', '--passphrase-file', str(replacement_file))
replayed = invoke('vault', 'rewrap', '--from-backend', 'passphrase', '--backend', 'passphrase',
                  '--request-id', rewrap_id, '--new-passphrase-file', str(replacement_file))
assert replayed['changed'] and replayed['replayed'] and replayed['item']['vault_id'] == initial['vault_id']
result = subprocess.run([binary, 'run', '--', 'sh', '-c', 'printf %s "$TOKEN"'],
                        cwd=project, env=env, capture_output=True, timeout=20)
assert result.returncode == 0 and result.stdout == b'[REDACTED]' and not result.stderr
print('Installed vault setup, sessions, locked metadata, protected output and rewrap/replay verified')
