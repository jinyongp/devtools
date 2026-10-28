"""Installed request recovery journeys through real CLI, supervisors and HTTP."""
import hashlib
from http.client import HTTPConnection, HTTPException
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import time
import uuid
from urllib.error import HTTPError
from urllib.parse import parse_qs, urlsplit
from urllib.request import ProxyHandler, Request, build_opener


def check_request_recovery(binary, root, environment, documents):
    root = Path(root).resolve()
    root.mkdir(mode=0o700)
    state = root / 'state'
    env = {k: v for k, v in environment.items() if k not in ('DEVTOOLS_TASK_CONTEXT', 'DEVTOOLS_ISOLATED_ROOT')}
    env['DEVTOOLS_FORMAT'] = 'json'
    owned, rows, checks, identities, cleanup = set(), [], [], [], {}
    dashboard = False

    def uid(): return str(uuid.uuid4())

    def api(*words, expected=0, selected=state, private=False):
        p = subprocess.run([str(binary), '--format', 'json', '--isolated-root', str(selected), *map(str, words)],
                           cwd=root, env=env, text=True, encoding='utf-8', capture_output=True, timeout=30)
        assert p.returncode == expected, ('request_recovery', list(words[:2]), p.returncode, expected)
        doc = json.loads(p.stdout if expected == 0 else p.stderr)
        assert doc['ok'] == (expected == 0) and not (p.stdout and p.stderr)
        if expected: documents.append(doc)
        if not private: rows.append({'operation': list(words[:3]), 'exit_code': p.returncode})
        return doc['data'] if expected == 0 else doc['error']

    def mutation(*words): return api(*words, '--request-id', uid())

    server = root / 'server.py'
    server.write_text('''import pathlib,signal,sys,time
def stop(*args):raise SystemExit(0)
for name in ('SIGTERM','SIGINT','SIGBREAK'):
 if hasattr(signal,name):signal.signal(getattr(signal,name),stop)
with pathlib.Path(sys.argv[1]).open('a') as stream:stream.write('started\\n')
while True:time.sleep(.05)
''', encoding='utf-8')
    bad = root / ('bad.exe' if os.name == 'nt' else 'bad')
    bad.write_bytes(b'not an OS executable image\n')
    bad.chmod(0o700)

    def config(directory, failing=False, proxy=False, ready=False):
        args = [str(bad)] if failing else [sys.executable, str(server), str(directory / 'starts')]
        body = 'profile="request-recovery"\n'
        if proxy: body += '[ports.web]\nrange=[20000,65000]\n[proxies.front]\nhost="recovery.localhost"\nport="web"\n'
        body += '[commands.web]\ninject=false\nexec=' + json.dumps(args) + '\n'
        if proxy: body += 'serve=["web"]\n'
        if ready:
            body += '[commands.web.ready]\nexec=' + json.dumps([sys.executable, '-c', 'import pathlib,sys;sys.exit(0 if pathlib.Path("gate").exists() else 1)']) + '\ntimeout="1s"\n'
        (directory / 'devtools.toml').write_text(body, encoding='utf-8')
        return body

    def project(name, **options):
        directory = root / name
        directory.mkdir(mode=0o700)
        config(directory, **options)
        return directory

    def start(directory):
        item = mutation('process', 'start', 'web', '--dir', directory)['item']
        owned.add(item['id'])
        return item['id']

    def receipt_hashes():
        return {str(p.relative_to(state)): hashlib.sha256(p.read_bytes()).hexdigest() for p in state.rglob('receipts/*.json')}

    def wait_starts(directory, count):
        deadline = time.monotonic() + 10
        marker = directory / 'starts'
        while not marker.exists() or len(marker.read_text(encoding='utf-8').splitlines()) < count:
            assert time.monotonic() < deadline, 'Owned child did not publish its start marker'
            time.sleep(.01)
        assert len(marker.read_text(encoding='utf-8').splitlines()) == count, 'Duplicate child start'

    try:
        for group, action in (('process', 'start'), ('project', 'up')):
            directory = project(group)
            request = uid()
            args = [group, action, 'web', '--dir', directory, '--request-id', request]
            first = api(*args)
            execution = first['item']['id'] if group == 'process' else first['items'][0]['item']['id']
            owned.add(execution)
            before = receipt_hashes()
            path = directory / 'devtools.toml'
            for variant in ('removed', 'invalid', 'missing'):
                if variant == 'removed': path.write_text('profile="request-recovery"\n', encoding='utf-8')
                elif variant == 'invalid': path.write_text('[broken\n', encoding='utf-8')
                else: path.unlink()
                replay = api(*args)
                assert replay['replayed'] and replay['changed'] == first['changed']
                assert api('process', 'status', execution)['item']['state'] == 'running'
            saved = api(group, 'request', 'show', request)['item']
            assert saved['state'] == 'completed' and saved['result'] == first
            assert receipt_hashes() == before, 'Completed recovery rewrote receipts'
            assert api(*args, '--capture-logs=true', expected=3)['code'] == 'request_conflict'
            wait_starts(directory, 1)
            identities.append({'kind': 'completed', 'group': group, 'request_id': request, 'execution_id': execution, 'receipt_hashes_unchanged': True, 'expected_starts': 1, 'observed_starts': 1})
            # A new request can reuse an execution, so request UUID != execution ID.
            if group == 'process':
                config(directory)
                reused = uid()
                assert api('process', 'start', 'web', '--dir', directory, '--request-id', reused)['item']['id'] == execution
                assert api('process', 'request', 'show', reused)['item']['result']['item']['id'] == execution
        checks.append('completed_results_survive_config_loss_and_preserve_identity')

        fresh = root / 'uninitialized'
        for group in ('process', 'project'):
            assert api(group, 'request', 'show', uid(), expected=3, selected=fresh)['code'] == 'request_not_found'
        assert not fresh.exists(), 'Passive query initialized a store'
        checks.append('missing_query_does_not_initialize_storage')

        directory = project('ready', ready=True)
        request = uid()
        args = ['project', 'up', 'web', '--dir', directory, '--timeout', '1s', '--request-id', request]
        first = api(*args, expected=3)
        outcome = first['details']['facts']['outcomes'][0]
        assert outcome['code'] == 'readiness_timeout'
        owned.add(outcome['id'])
        (directory / 'devtools.toml').unlink()
        (directory / 'gate').touch()
        result = api(*args)
        assert result['replayed'] and result['items'][0]['status'] == 'ready' and result['items'][0]['item']['id'] == outcome['id']
        wait_starts(directory, 1)
        identities.append({'kind': 'readiness', 'request_id': request, 'execution_id': outcome['id'], 'config_present': False, 'expected_starts': 1, 'observed_starts': 1})
        checks.append('frozen_readiness_resumes_without_current_config')

        for capture in (False, True):
            directory = project('failure-' + str(capture))
            old = start(directory)
            config(directory, failing=True)
            request = uid()
            args = ['process', 'restart', old, '--capture-logs=' + str(capture).lower(), '--request-id', request]
            first = api(*args, expected=126)
            details = first['details']
            new = details['target']['id']
            assert new != old and details['target']['name'] == new and details['facts']['previous_id'] == old and details['facts']['outage']
            assert details['remedies'][0]['argv'][1:4] == ['process', 'status', new]
            assert len(details['remedies']) == (2 if capture else 1)
            for remedy in details['remedies']:
                argv = remedy['argv']
                assert argv[0] == 'devtools' and '--isolated-root' in argv
                p = subprocess.run([str(binary), *argv[1:]], cwd=root, env=env, capture_output=True, text=True, encoding='utf-8', timeout=30)
                assert p.returncode == 0, 'Published failed-launch remedy did not execute'
            replay = api(*args, expected=126)
            assert replay['details']['target']['id'] == new and replay['details']['facts']['replayed']
            assert api('process', 'request', 'show', request)['item']['state'] == 'completed'
            text = subprocess.run([str(binary), '--isolated-root', str(state), '--format', 'text', *map(str, args)], cwd=root, env=env, capture_output=True, text=True, encoding='utf-8', timeout=30)
            assert text.returncode == 126
            shown = ' '.join(text.stderr.split())
            assert all(x in shown for x in (old, new, 'Previous execution', 'New execution', 'Recorded service gap', 'Saved result replayed'))
            assert ('process logs' in shown) == capture
            identities.append({'kind': 'failure_guidance', 'request_id': request, 'previous_id': old, 'execution_id': new, 'capture': capture, 'executed_remedies': len(details['remedies'])})
        checks.append('failed_launch_remedies_execute_and_capture_is_conditional')

        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        mutation('proxy', 'configure', '--http-address', '127.0.0.1:' + str(port), '--disable-https')
        directory = project('proxy', failing=True, proxy=True)
        for action in ('up', 'restart'):
            if action == 'restart':
                config(directory, proxy=True)
                start(directory)
                config(directory, failing=True, proxy=True)
            request = uid()
            args = ['project', action, 'web', '--dir', directory, '--request-id', request]
            first = api(*args, expected=3)
            failed_id = first['details']['facts']['outcomes'][0]['id']
            assert first['details']['retry'] == 'inspect_first'
            before = receipt_hashes()
            replay = api(*args, expected=3)
            assert replay['details']['facts']['outcomes'][0]['id'] == failed_id and replay['details']['retry'] == 'inspect_first'
            saved = api('project', 'request', 'show', request)['item']
            assert saved['state'] == 'completed' and saved['result']['items'][0]['item']['id'] == failed_id
            assert before == receipt_hashes(), 'Proxy terminal replay created another child'
            identities.append({'kind': 'proxy_terminal', 'action': action, 'request_id': request, 'execution_id': failed_id, 'replayed_execution_id': replay['details']['facts']['outcomes'][0]['id'], 'receipt_hashes_unchanged': True})
        config(directory, proxy=True)
        old = start(directory)
        request = uid()
        replacement = api('process', 'restart', old, '--request-id', request)['item']['id']
        owned.add(replacement)
        (directory / 'devtools.toml').unlink()
        assert api('process', 'restart', old, '--request-id', request)['replayed']
        checks.append('proxy_terminal_replay_keeps_child_and_direct_restart_needs_no_config')

        # Credentials stay in memory and never enter evidence/documents.
        dashboard = True
        link = api('dashboard', 'start', '--profile', 'request-recovery', private=True)['item']['url']
        parsed = urlsplit(link)
        origin = parsed.scheme + '://' + parsed.netloc
        boot = parse_qs(parsed.fragment)['token'][0]

        opener = build_opener(ProxyHandler({}))

        def http(path, body, credential=None):
            headers = {'Content-Type': 'application/json', 'Origin': origin}
            if credential: headers['Authorization'] = 'Bearer ' + credential
            req = Request(origin + path, data=json.dumps(body).encode(), headers=headers)
            try:
                with opener.open(req, timeout=15) as response: return response.status, json.load(response)
            except HTTPError as error: return error.code, json.load(error)

        code, session = http('/session', {'token': boot})
        assert code == 200
        credential = session['token']
        directory = project('dashboard')
        request = uid()
        body = {'domain': 'process', 'profile': 'request-recovery', 'process': {'action': 'start', 'directory': str(directory), 'command': 'web', 'request_id': request}}
        code, original = http('/api/actions', body, credential)
        assert code == 200 and original['ok']
        owned.add(original['data']['item']['id'])
        (directory / 'devtools.toml').unlink()
        code, replay = http('/api/actions', body, credential)
        assert code == 200 and replay['data']['replayed'] and replay['data']['item']['id'] == original['data']['item']['id']
        body['profile'] = 'other'
        assert http('/api/actions', body, credential)[0] != 200
        checks.append('live_dashboard_authenticated_replay_preserves_profile_boundary')
        return {'result': 'pass', 'binary_sha256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(), 'checks': checks, 'identities': identities, 'rows': rows, 'cleanup': cleanup}
    finally:
        if dashboard: api('dashboard', 'stop', private=True)
        for execution in owned: api('process', 'stop', execution, '--request-id', uid())
        if (state / 'data').exists(): mutation('proxy', 'stop', '--all')
        # Listener stop retains the controller. Retire only this fixture's
        # authenticated daemon so native Windows can release its mapped image.
        control_path = state / 'data' / 'proxy' / 'control.json'
        if control_path.is_file():
            control = json.loads(control_path.read_text(encoding='utf-8'))
            address, token = urlsplit(control.get('address', '')), control.get('token', '')
            assert address.scheme == 'http' and address.hostname == '127.0.0.1' and address.port and not address.username and len(token) == 64, 'Invalid owned controller metadata'
            connection = HTTPConnection('127.0.0.1', address.port, timeout=5)
            try:
                connection.request('POST', '/stop', headers={'Authorization': 'Bearer ' + token})
                response = connection.getresponse()
                assert response.status == 200, 'Owned controller rejected shutdown'
                response.read()
            except (OSError, HTTPException):
                pass  # An orderly shutdown may close its response.
            finally:
                connection.close()
            deadline = time.monotonic() + 5
            while True:
                try:
                    with socket.create_connection(('127.0.0.1', address.port), timeout=1): pass
                except ConnectionRefusedError:
                    break
                assert time.monotonic() < deadline, 'Owned controller survived fixture cleanup'
                time.sleep(.05)
        cleanup['proxy_controller_stopped'] = True
