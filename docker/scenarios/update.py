"""Self-update an installed release and preserve it on failed updates."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

home = Path.home()
binary = home / '.local/bin/devtools'

def execute(*args, expected=0):
    result = subprocess.run(args, text=True, capture_output=True, timeout=30)
    assert result.returncode == expected, (result.stdout, result.stderr)
    return json.loads(result.stdout if expected == 0 else result.stderr)

execute('sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install', '--version',
        '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES'])
execute(str(binary), 'var', 'set', 'KEEP', '--value', 'preserved', '--profile', 'update')
execute(str(binary), 'update', '--version', '0.0.0-test.2', '--source',
        os.environ['DEVTOOLS_TEST_RELEASES'])
assert execute(str(binary), 'version')['data']['version'] == '0.0.0-test.2'
assert execute(str(binary), 'var', 'get', 'KEEP', '--profile', 'update')['data']['value'] == 'preserved'
before = hashlib.sha256(binary.read_bytes()).digest()
with tempfile.TemporaryDirectory() as empty:
    error = execute(str(binary), 'update', '--version', '0.0.0-test.1', '--source', empty, expected=1)
assert error['error']['code'] == 'update_failed'
assert hashlib.sha256(binary.read_bytes()).digest() == before
print('Installed self-update and failure preservation passed')
