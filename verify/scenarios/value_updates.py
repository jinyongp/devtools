"""Installed opted value restart through actual proxy/launcher/key-session paths.

Own loopback listeners and an isolated root; no system hosts/trust/provider writes.
"""
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import sys
import time
import uuid

home = Path.home()
binary = home / '.local/bin/devtools'
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install', '--version',
                '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               capture_output=True, check=True, timeout=30)
project = home / 'value-update-project'
project.mkdir()
isolation = home / 'value-update-state'
secret = 'synthetic-installed-auto-secret'
password = 'synthetic-installed-auto-passphrase'
env = dict(os.environ)
for key in ['DEVTOOLS_TASK_CONTEXT', 'DEVTOOLS_ISOLATED_ROOT', 'TOKEN', 'REV']:
    env.pop(key, None)


def api(*args, input=None):
    result = subprocess.run([str(binary), '--isolated-root', str(isolation), *args],
                            cwd=project, env=env, input=input, text=True,
                            capture_output=True, timeout=40)
    assert secret not in result.stdout + result.stderr
    assert password not in result.stdout + result.stderr
    assert result.returncode == 0 and not result.stderr, (args[:2], result.returncode)
    envelope = json.loads(result.stdout)
    assert envelope['schema_version'] == 1 and envelope['ok'] is True
    return envelope['data']


def mutate(*args):
    return api(*args, '--request-id', str(uuid.uuid4()))


def free_port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]


ports = set()
while len(ports) < 3:
    ports.add(free_port())
backend, http_port, tls_port = ports
host = 'auto.values.localhost'
(project / 'server.py').write_text('''import http.server,json,os,socketserver
class Backend(http.server.HTTPServer):
 def server_bind(self):
  socketserver.TCPServer.server_bind(self)
  self.server_name,self.server_port=self.server_address[:2]
class H(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  body=json.dumps({'revision':os.getenv('REV'),'token_present':'TOKEN' in os.environ,'pid':os.getpid()}).encode()
  self.send_response(200);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
 def log_message(self,*args):pass
Backend(('127.0.0.1',int(os.environ['PORT'])),H).serve_forever()
''')
(project / 'ready.py').write_text('''import os,socket
with socket.create_connection(('127.0.0.1',int(os.environ['PORT'])),timeout=1):pass
''')
(project / 'devtools.toml').write_text(f'''profile='value-updates'
[ports.web]
port={backend}
strict=true
[proxies.web]
host='{host}'
port='web'
[commands.web]
exec={json.dumps([sys.executable, 'server.py'])}
serve=['web']
inject=true
values_update='restart'
[commands.web.bind]
PORT={{port='web'}}
[commands.web.ready]
exec={json.dumps([sys.executable, 'ready.py'])}
timeout='5s'
''')


def wait(check):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        value = check()
        if value:
            return value
        time.sleep(.05)
    raise AssertionError('Installed value update did not converge')


context = None


def read_proxy():
    connection = socket.create_connection(('127.0.0.1', tls_port), timeout=4)
    with context.wrap_socket(connection, server_hostname=host) as stream:
        stream.sendall(f'GET / HTTP/1.1\r\nHost: {host}:{tls_port}\r\nConnection: close\r\n\r\n'.encode())
        response = http.client.HTTPResponse(stream)
        response.begin()
        assert response.status == 200
        body = response.read()
        assert secret.encode() not in body
        return json.loads(body)


try:
    api('var', 'set', 'REV', '--value', 'one')
    api('vault', 'init', '--backend', 'passphrase', '--passphrase-stdin', input=password+'\n')
    api('sec', 'set', 'TOKEN', '--stdin', input=secret)
    mutate('proxy', 'configure', '--http-address', f'127.0.0.1:{http_port}',
           '--https-address', f'127.0.0.1:{tls_port}')
    up = api('project', 'up', 'web', '--proxy', '--request-id', str(uuid.uuid4()), '--timeout', '10s')
    first = up['items'][0]['item']
    assert first['proxy_mode'] is True
    certificate = home / 'value-update-ca.pem'
    api('proxy', 'ca', 'export', '--output', str(certificate))
    context = ssl.create_default_context(cafile=str(certificate))
    original = read_proxy()
    assert original['revision'] == 'one' and original['token_present'] is True
    api('var', 'set', 'REV', '--value', 'two')

    def applied():
        status = api('process', 'status', first['id'])['item']
        update = status.get('value_apply')
        if update and update['state'] == 'applied':
            assert update['reason'] == 'readiness_passed'
            return api('process', 'status', update['next_id'])['item']
        if update and update['state'] in ('failed', 'paused'):
            raise AssertionError('Automatic proxy update failed: '+update['reason'])
        return None

    current = wait(applied)
    assert current['proxy_mode'] is True and current['previous_id'] == first['id']
    assert current['state'] == 'running' and current['ended_at'] is None
    actual = read_proxy()
    assert actual['revision'] == 'two' and actual['token_present'] is True
    assert actual['pid'] != original['pid']  # Comparison only; never a kill target.
    api('vault', 'lock')
    wait(lambda: api('process', 'status', current['id'])['values']['state'] == 'verification_pending')
    assert read_proxy()['revision'] == 'two'
    api('vault', 'unlock', '--passphrase-stdin', input=password+'\n')
    wait(lambda: api('process', 'status', current['id'])['values']['state'] == 'current')
    records = api('process', 'list')['items']
    assert len(records) == 2 and sum(r['ended_at'] is None for r in records) == 1
    print('Installed automatic proxy restart, frozen selected input, readiness and passive lock verified')
finally:
    # Both controls are private to this fixture. Normal/system networking is not
    # touched, and no record PID is used to terminate a process.
    try:
        mutate('project', 'down')
    finally:
        mutate('proxy', 'stop')
