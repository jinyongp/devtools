"""Installed project lifecycle over real supervisors, ports and readiness probes."""
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
root = home / 'project'
other = home / 'other-worktree'
root.mkdir()
other.mkdir()


def api(*args, directory=root, error=None):
    result = subprocess.run([str(binary), *args], cwd=directory, text=True,
                            capture_output=True, timeout=35)
    assert (result.returncode != 0) == bool(error), (args, result.returncode, result.stderr)
    assert not (result.stdout and result.stderr), 'mixed JSON streams'
    envelope = json.loads(result.stderr if error else result.stdout)
    assert envelope['schema_version'] == 1
    if error:
        assert envelope['error']['code'] == error, envelope['error']
        return envelope['error']
    return envelope['data']


config = '''profile="project-fixture"
[ports.web]
port=25100
range=[25100,25199]
[ports.api]
port=25200
range=[25200,25299]
[ports.idle]
port=25300
range=[25300,25399]
[commands.web]
exec=["python3","server.py"]
serve=["web"]
[commands.web.bind]
PORT={port="web"}
[commands.web.ready]
exec=["python3","probe.py"]
[commands.api]
exec=["python3","server.py"]
serve=["api"]
[commands.api.bind]
PORT={port="api"}
[commands.api.ready]
exec=["python3","probe.py"]
[commands.serve-only]
exec=["python3","-c","import time; time.sleep(300)"]
serve=["idle"]
env="missing"
[commands.delayed]
exec=["python3","-c","import time; time.sleep(300)"]
[commands.delayed.ready]
exec=["python3","-c","from pathlib import Path; import sys; sys.exit(0 if Path('ready.flag').exists() else 1)"]
'''
for directory in (root, other):
    (directory / 'devtools.toml').write_text(config)
    (directory / 'server.py').write_text('''import os, socket, time
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', int(os.environ['PORT'])))
s.listen()
while True:
 c, _ = s.accept()
 c.close()
''')
    (directory / 'probe.py').write_text('''import os, socket
with socket.create_connection(('127.0.0.1', int(os.environ['PORT'])), timeout=.5):
 pass
''')

try:
    assert api('version')['protocol_version'] == 3
    assert api('project', 'status')['items'] == []

    # serve-only commands do not require an env that is never injected or bound.
    serve_only = api('project', 'up', 'serve-only', '--request-id', str(uuid.uuid4()))
    assert serve_only['changed'] and serve_only['items'][0]['status'] == 'running'
    api('project', 'down', 'serve-only', '--request-id', str(uuid.uuid4()))
    assert api('project', 'status')['items'] == []

    request = str(uuid.uuid4())
    args = ['project', 'up', 'web', 'api', '--timeout', '3s', '--request-id', request]
    first = api(*args)
    assert first['changed'] and not first['replayed']
    assert [item['status'] for item in first['items']] == ['ready', 'ready']
    ids = [item['item']['id'] for item in first['items']]
    assert api(*args) == {**first, 'replayed': True}
    assert [item['command'] for item in api('project', 'status', 'api')['items']] == ['api']

    # A new request for running servers must reuse them, including served ports.
    same = api('project', 'up', 'web', 'api', '--request-id', str(uuid.uuid4()))
    assert not same['changed'] and [item['item']['id'] for item in same['items']] == ids
    conflict = api('project', 'up', 'web', '--capture-logs', '--request-id', str(uuid.uuid4()),
                   error='project_operation_failed')
    assert conflict['details']['items'][0]['condition']['code'] == 'process_conflict'

    # Sharing a profile does not broaden status/down across worktree locations.
    other_started = api('project', 'up', 'web', '--request-id', str(uuid.uuid4()), directory=other)
    other_id = other_started['items'][0]['item']['id']
    down_id = str(uuid.uuid4())
    down = api('project', 'down', '--request-id', down_id)
    assert down['changed'] and len(down['items']) == 2
    assert api('project', 'status')['items'] == []
    assert [item['id'] for item in api('project', 'status', directory=other)['items']] == [other_id]
    new_web = api('project', 'up', 'web', '--request-id', str(uuid.uuid4()))
    assert api('project', 'down', '--request-id', down_id)['replayed']
    assert api('project', 'status')['items'][0]['id'] == new_web['items'][0]['item']['id']
    api('project', 'down', '--request-id', str(uuid.uuid4()))

    # Successful siblings remain running while an unfinished readiness wait retries.
    partial_args = ['project', 'up', 'web', 'delayed', '--timeout', '300ms', '--request-id', str(uuid.uuid4())]
    partial = api(*partial_args, error='project_operation_failed')['details']['items']
    assert partial[0]['status'] == 'ready' and partial[1]['status'] == 'pending'
    web_id, delayed_id = (item['item']['id'] for item in partial)
    (root / 'ready.flag').touch()
    resumed = api(*partial_args)
    assert resumed['replayed'] and [item['status'] for item in resumed['items']] == ['ready', 'ready']
    assert [item['item']['id'] for item in resumed['items']] == [web_id, delayed_id]
    api('project', 'down', '--request-id', str(uuid.uuid4()))
    noop_id = str(uuid.uuid4())
    noop = api('project', 'down', '--request-id', noop_id)
    assert not noop['changed'] and not noop['replayed'] and noop['items'] == []
    assert api('project', 'down', '--request-id', noop_id)['replayed']
    print('Installed project multi-start, readiness, singleton reuse, worktree isolation, partial retry and down replay passed')
finally:
    # All processes here belong to this scenario's isolated HOME/data root.
    records = api('process', 'list', '--profile', 'project-fixture')['items']
    for record in records:
        if record['ended_at'] is None and record['state'] != 'interrupted':
            api('process', 'stop', record['id'], '--request-id', str(uuid.uuid4()))
