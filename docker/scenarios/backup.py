"""Verify encrypted recovery using only the installed executable."""
import json
import os
from pathlib import Path
import subprocess
import uuid

home = Path.home()
env = dict(os.environ, PATH=str(home / '.local/bin') + ':' + os.environ['PATH'])
def call(args, input=None, error=None):
    r = subprocess.run(args, env=env, input=input, text=True, capture_output=True, timeout=20)
    assert 'BACKUP_CANARY' not in r.stdout + r.stderr
    data = json.loads(r.stdout if r.returncode == 0 else r.stderr)
    if error:
        assert not data['ok'] and data['error']['code'] == error, data
        return data['error']
    assert r.returncode == 0, r.stderr
    return data['data']
def api(*args, **kwargs): return call(['devtools', *args], **kwargs)
call(['sh',os.environ['DEVTOOLS_TEST_INSTALLER'],'install','--version','0.0.0-test.1','--source',os.environ['DEVTOOLS_TEST_RELEASES']])
identity, recipient = str(home / 'identity'), str(home / 'recipient')
api('backup','keygen','--identity-file',identity,'--recipient-file',recipient)
api('backup','configure','--directory',str(home / 'backups'),'--recipient-file',recipient)
api('sec','set','TOKEN','--profile','fixture','--stdin',input='BACKUP_CANARY')
api('var','set','MODE','--profile','fixture','--value','development')
task = api('task','add','--profile','fixture','--title','Recover me','--request-id',str(uuid.uuid4()))
api('task','claim',task['item']['id'],'--profile','fixture','--request-id',str(uuid.uuid4()))
archive = api('backup','create')['path']
assert b'BACKUP_CANARY' not in Path(archive).read_bytes()
assert api('backup','inspect','--file',archive,'--identity-file',identity)['profiles'][0]['profile'] == 'fixture'
args = ['backup','restore','--file',archive,'--identity-file',identity,'--profile','fixture','--as','recovered']
plan = api(*args)
request = str(uuid.uuid4())
result = api(*args,'--apply',plan['digest'],'--request-id',request)
assert result['applied']
assert api(*args,'--apply',plan['digest'],'--request-id',request) == result
assert api('var','get','MODE','--profile','recovered')['value'] == 'development'
assert api('task','claim',task['item']['id'],'--profile','recovered','--request-id',str(uuid.uuid4()))['context_valid']
replace = [*args,'--replace']
plan = api(*replace)
api('var','set','MODE','--profile','recovered','--value','changed')
api(*replace,'--apply',plan['digest'],'--request-id',str(uuid.uuid4()),error='revision_conflict')
plan = api(*replace)
result = api(*replace,'--apply',plan['digest'],'--request-id',str(uuid.uuid4()))
assert result['safety_backup']
api('backup','inspect','--file',result['safety_backup'],'--identity-file',identity)
damaged = home / 'damaged.age'
data = bytearray(Path(archive).read_bytes()); data[-1] ^= 1; damaged.write_bytes(data); damaged.chmod(0o600)
api('backup','inspect','--file',str(damaged),'--identity-file',identity,error='invalid_backup')
print('Installed encrypted backup, restore, claim recovery, conflict, retry and safety backup verified')
