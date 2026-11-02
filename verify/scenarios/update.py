"""Self-update an installed release and preserve it on failed updates."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

home = Path.home()
binary = home / '.local/bin/devtools'
alias = home / '.local/bin/dvt'

def execute(*args, expected=0, input=None):
    result = subprocess.run(args, input=input, text=True, capture_output=True, timeout=40)
    assert result.returncode == expected, (result.stdout, result.stderr)
    return json.loads(result.stdout if expected == 0 else result.stderr)

installed = subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install', '--version',
                           '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
                          env=dict(os.environ, PATH=os.defpath), text=True,
                          capture_output=True, timeout=30)
assert installed.returncode == 0 and json.loads(installed.stdout)['data']['alias']['status'] == 'created'
execute(str(binary), 'var', 'set', 'KEEP', '--value', 'preserved', '--profile', 'update')
updated = execute(str(alias), 'update', '--version', '0.0.0-test.2', '--source',
                  os.environ['DEVTOOLS_TEST_RELEASES'])
assert updated['data']['changed'] is True
assert updated['data']['binary_changed'] is True and updated['data']['storage']['ready'] is True
assert updated['data']['alias'] == {'name': 'dvt', 'status': 'unchanged'}
assert execute(str(binary), 'version')['data']['version'] == '0.0.0-test.2'
assert execute(str(binary), 'var', 'get', 'KEEP', '--profile', 'update')['data']['value'] == 'preserved'
before = hashlib.sha256(binary.read_bytes()).digest()
with tempfile.TemporaryDirectory() as empty:
    error = execute(str(binary), 'update', '--version', '0.0.0-test.1', '--source', empty, expected=1)
assert error['error']['code'] == 'update_failed'
assert hashlib.sha256(binary.read_bytes()).digest() == before

# Same-image updates resume explicit physical conversion through the new binary.
paths = execute(str(binary), 'project', 'inspect', '--profile', 'update')['data']['paths']
profiles = Path(paths['data']) / 'profiles'
legacy = profiles / ('LegacyUpdate'.encode().hex() + '.json')
legacy.write_text(json.dumps({'version':1,'profile':'LegacyUpdate','envs':{},'keys':{}}))
legacy.chmod(0o600)
resumed = execute(str(binary), 'update', '--version', '0.0.0-test.2', '--source', os.environ['DEVTOOLS_TEST_RELEASES'])['data']
assert resumed['binary_changed'] is False and resumed['storage']['ready'] is True
assert resumed['storage']['report']['changed'] is True
assert hashlib.sha256(binary.read_bytes()).digest() == before
execute(str(binary), 'var', 'list', '--profile', 'LegacyUpdate')

# Future state is preserved and reported separately from the installed image.
future = profiles / ('FutureUpdate'.encode().hex() + '.json')
future_body = json.dumps({'version':999,'profile':'FutureUpdate'})
future.write_text(future_body)
future.chmod(0o600)
incomplete = execute(str(binary), 'update', '--version', '0.0.0-test.2', '--source', os.environ['DEVTOOLS_TEST_RELEASES'], expected=3)
report = incomplete['error']['details']['report']
assert incomplete['error']['code'].startswith('storage_upgrade_')
assert report['binary_changed'] is False and report['storage']['ready'] is False
assert future.read_text() == future_body and hashlib.sha256(binary.read_bytes()).digest() == before
future.unlink()
assert execute(str(binary), 'doctor', '--storage', '--fix')['data']['ready'] is True

# A locked session cannot silently authorize even an address-only conversion.
passphrase = 'synthetic-update-vault-passphrase'
execute(str(binary), 'vault', 'init', '--backend', 'passphrase', '--passphrase-stdin', input=passphrase+'\n')
execute(str(binary), 'secret', 'set', 'TOKEN', '--profile', 'update', '--stdin', input='synthetic-update-secret')
key = 'p1-' + hashlib.sha256(b'update').hexdigest() + '.json'
canonical = profiles / key
protected = canonical.read_bytes()
old_address = profiles / (b'update'.hex()+'.json')
old_address.write_bytes(protected)
canonical.unlink()
(profiles / '.identity' / key).unlink()
execute(str(binary), 'vault', 'lock')
session_before = execute(str(binary), 'vault', 'status')['data']
pending = execute(str(binary), 'update', '--version', '0.0.0-test.2', '--source', os.environ['DEVTOOLS_TEST_RELEASES'], expected=3)['error']['details']['report']
assert pending['binary_changed'] is False and pending['storage']['status'] == 'pending'
assert old_address.read_bytes() == protected and not canonical.exists()
assert execute(str(binary), 'vault', 'status')['data'] == session_before
execute(str(binary), 'vault', 'unlock', '--passphrase-stdin', input=passphrase+'\n')
idle = execute(str(binary), 'vault', 'status')['data']['item']['idle_expires_at']
resumed = execute(str(binary), 'update', '--version', '0.0.0-test.2', '--source', os.environ['DEVTOOLS_TEST_RELEASES'])['data']
assert resumed['binary_changed'] is False and resumed['storage']['ready'] is True
assert canonical.read_bytes() == protected
assert execute(str(binary), 'vault', 'status')['data']['item']['idle_expires_at'] == idle
assert hashlib.sha256(binary.read_bytes()).digest() == before
print('Installed self-update and failure preservation passed')
