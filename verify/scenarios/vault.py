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


def invoke(*args, input=None, error=None):
    result = subprocess.run([binary, *args], cwd=project, env=env, input=input,
                            text=True, capture_output=True, timeout=20)
    assert secret not in result.stdout + result.stderr
    assert passphrase not in result.stdout + result.stderr
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
selected_env = dict(env)
selected_env.pop('TOKEN', None)
result = subprocess.run([binary, 'run', '--bundle', 'frontend', '--', 'sh', '-c',
                         'test -z "${TOKEN+x}" && printf %s "$PUBLIC"'], cwd=project,
                        env=selected_env, capture_output=True, timeout=20)
assert result.returncode == 0 and result.stdout == b'public value' and not result.stderr
result = subprocess.run([binary, 'run', '--bundle', 'backend', '--', 'sh', '-c', 'printf %s "$TOKEN"'],
                        cwd=project, env=selected_env, capture_output=True, timeout=20)
assert result.returncode == 0 and result.stdout == b'[REDACTED]' and not result.stderr

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
        return
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
    managed_api('process','restart',managed,'--bundle','missing','--request-id',str(uuid.uuid4()),error='bundle_not_found')
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
invoke('run', '--bundle', 'missing', '--', 'true', error='bundle_not_found')
invoke('vault', 'lock')
locked = invoke('vault', 'status')['item']
assert locked['state'] == 'locked_manual'
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
print('Installed vault setup, reused key session, locked metadata, JSON/schema/completion and protected output verified')
