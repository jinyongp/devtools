"""Installed operation contracts; owned fixtures, no user data or raw PID signals."""
import hashlib
import json
import os
from pathlib import Path
import statistics
import subprocess
import sys
import time
import uuid


def check_operations(binary, root, source, environment, documents):
    root = Path(root)
    root.mkdir(mode=0o700)
    env = {k: v for k, v in environment.items() if k not in ('DEVTOOLS_TASK_CONTEXT', 'DEVTOOLS_ISOLATED_ROOT')}
    rows, owned = [], set()

    def argv(words):
        return [str(binary), '--format', 'json', '--isolated-root', str(source), *map(str, words)]

    def api(*words, expected=0, body=None):
        p = subprocess.run(argv(words), cwd=root, env=env, input=None if body is None else json.dumps(body),
                           capture_output=True, text=True, encoding='utf-8', timeout=30)
        assert p.returncode == expected, ('operation_contract', words[:2], p.returncode, expected)
        doc = json.loads(p.stdout if expected == 0 else p.stderr)
        if expected:
            assert not p.stdout and not doc['ok']
            documents.append(doc)
        rows.append({'operation': list(words[:2]), 'exit_code': p.returncode, 'expected_exit_code': expected})
        return doc['data'] if expected == 0 else doc['error']

    def mutation(*words):
        return api(*words, '--request-id', str(uuid.uuid4()))

    def wait_file(path):
        end = time.monotonic() + 10
        while not path.exists():
            assert time.monotonic() < end, ('missing_owned_marker', path.name)
            time.sleep(.01)

    server = root / 'server.py'
    server.write_text('''import pathlib,signal,sys,time
marker=pathlib.Path(sys.argv[1])
def stop(*args):
 pathlib.Path(str(marker)+'.term').touch()
 if len(sys.argv)<3:raise SystemExit(0)
for name in ('SIGTERM','SIGINT','SIGBREAK'):
 if hasattr(signal,name):signal.signal(getattr(signal,name),stop)
with marker.open('a') as stream:stream.write('started\\n')
pathlib.Path(str(marker)+'.ready').touch()
while True:time.sleep(.05)
''', encoding='utf-8')
    bad = root / ('bad.exe' if os.name == 'nt' else 'bad')
    bad.write_bytes(b'not an OS executable image\n')
    bad.chmod(0o700)

    def project(name, invalid=False, hold=False):
        directory = root / name
        directory.mkdir(mode=0o700, exist_ok=True)
        marker = directory / 'starts'
        command = [str(bad)] if invalid else [sys.executable, str(server), str(marker)] + (['hold'] if hold else [])
        (directory / 'devtools.toml').write_text('profile=' + json.dumps('op-' + name) + '\n[commands.web]\ninject=false\nexec=' + json.dumps(command) + '\n', encoding='utf-8')
        return directory, marker

    def start(directory):
        item = mutation('process', 'start', 'web', '--dir', str(directory))['item']
        owned.add(item['id'])
        return item['id']

    def stop(execution):
        mutation('process', 'stop', execution)
        owned.discard(execution)

    try:
        w = mutation('task', 'workstream', 'create', '--profile', 'op-order', '--title', 'Order')['item']['id']

        def plan(): return api('task', 'workstream', 'plan', 'show', w, '--profile', 'op-order')
        def edit(operations):
            before = plan()
            words = ['task', 'workstream', 'edit', w, '--profile', 'op-order', '--if-revision', str(before['revision']), '--stdin']
            body = {'reason': 'Installed forward order regression', 'operations': operations}
            api(*words, '--dry-run', body=body)
            assert plan() == before, 'Preview changed state'
            request = str(uuid.uuid4())
            first = api(*words, '--request-id', request, body=body)
            replay = api(*words, '--request-id', request, body=body)
            assert replay['replayed'] and replay['created_refs'] == first['created_refs']
            return first['created_refs']

        a = edit([{'op': 'task.add', 'ref': 'a', 'value': {'title': 'A'}}])['a']
        n = edit([{'op': 'task.move', 'id': '@n', 'before_id': a}, {'op': 'task.add', 'ref': 'n', 'value': {'title': 'N'}}])['n']
        saved = plan()
        assert saved['task_order'] == [n, a] and len(saved['tasks']) == 2
        assert api('task', 'workstream', 'plan', 'show', w, '--profile', 'op-order', '--at-revision', str(saved['revision']))['task_order'] == [n, a]
        z = edit([{'op': 'task.move', 'id': a, 'after_id': '@z'}, {'op': 'task.add', 'ref': 'z', 'value': {'title': 'Z'}}])['z']
        assert plan()['task_order'] == [n, z, a]

        directory, _ = project('direct', invalid=True)
        request = str(uuid.uuid4())
        words = ['process', 'start', 'web', '--dir', str(directory), '--request-id', request]
        failed = api(*words, expected=126)
        execution = failed['details']['target']['id']
        assert failed['code'] == 'execution_failed' and failed['details']['facts']['state'] == 'failed'
        project('direct')
        replay = api(*words, expected=126)
        assert replay['details']['target']['id'] == execution and replay['details']['facts']['replayed']
        old = start(directory)
        wait_file(directory / 'starts.ready')
        project('direct', invalid=True)
        words = ['process', 'restart', old, '--request-id', str(uuid.uuid4())]
        failed = api(*words, expected=126)
        assert failed['details']['facts']['outage'] and failed['details']['facts']['previous_id'] == old
        assert api('process', 'status', old)['item']['ended_at'] is not None
        project('direct')
        assert api(*words, expected=126)['details']['target']['id'] == failed['details']['target']['id']
        owned.discard(old)
        fresh = start(directory)
        stop(fresh)

        directory, marker = project('batch')
        old = start(directory)
        wait_file(Path(str(marker) + '.ready'))
        project('batch', invalid=True)
        words = ['project', 'restart', 'web', '--dir', str(directory), '--request-id', str(uuid.uuid4())]
        failed = api(*words, expected=3)
        assert failed['details']['retry'] == 'inspect_first'
        assert any(r['argv'][1:3] == ['project', 'status'] for r in failed['details']['remedies'])
        project('batch')
        assert api(*words, expected=3)['details']['retry'] == 'inspect_first'
        text = subprocess.run([str(binary), '--format', 'text', '--isolated-root', str(source), *words], cwd=root, env=env, capture_output=True, text=True, encoding='utf-8', timeout=30)
        assert text.returncode == 3 and 'Retry using the same request ID' not in text.stderr
        assert not api('project', 'status', '--dir', str(directory))['items']
        owned.discard(old)

        samples = []
        if os.name != 'nt':
            # Windows Job/CTRL_BREAK and portable lock marker tests run in the
            # existing native Go lanes. Do not pretend Unix TERM exists there.
            for i in range(3):
                da, ma = project('a' + str(i), hold=True)
                db, _ = project('b' + str(i))
                t = time.monotonic(); b = start(db); baseline = time.monotonic() - t; stop(b)
                a = start(da); wait_file(Path(str(ma) + '.ready'))
                child = subprocess.Popen(argv(['process', 'stop', a, '--request-id', str(uuid.uuid4())]), cwd=root, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding='utf-8')
                try:
                    wait_file(Path(str(ma) + '.term'))
                    t = time.monotonic(); b = start(db); elapsed = time.monotonic() - t
                    assert child.poll() is None, 'B waited for A shutdown'
                    out, err = child.communicate(timeout=12)
                    assert child.returncode == 0 and json.loads(out)['ok'] and not err
                    samples.append({'baseline_seconds': baseline, 'contended_seconds': elapsed, 'b_started_before_a_stop': True})
                    owned.discard(a)
                    stop(b)
                finally:
                    if child.poll() is None:
                        child.communicate(timeout=12)
        return {'result': 'pass', 'uid': os.getuid() if hasattr(os, 'getuid') else None,
                'binary_sha256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
                'checks': ['forward_order_preview_replay_history', 'terminal_start_restart_replay_outage', 'terminal_project_inspection_guide'],
                'calls': rows, 'lock_samples': samples,
                'lock_median_seconds': statistics.median(s['contended_seconds'] for s in samples) if samples else None,
                'windows_lock_evidence': 'native Go marker and Job tests' if os.name == 'nt' else None}
    finally:
        for execution in owned:
            api('process', 'stop', execution, '--request-id', str(uuid.uuid4()))
