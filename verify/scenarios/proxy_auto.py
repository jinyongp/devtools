"""Automatic preparation through installed CLI, real peers, HTTP and verified TLS."""
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import sys
import uuid

home = Path.home()
binary = Path(os.environ.get("DEVTOOLS_TEST_BINARY", home / ".local/bin/devtools"))
if "DEVTOOLS_TEST_BINARY" not in os.environ:
    subprocess.run(["sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version",
                    "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"]],
                   check=True, capture_output=True, timeout=30)
root, other, state = (home / name for name in ("auto-project", "auto-worktree", "auto-state"))
root.mkdir()
other.mkdir()
env = dict(os.environ)
env.pop("DEVTOOLS_TASK_CONTEXT", None)
env.pop("DEVTOOLS_ISOLATED_ROOT", None)


def command(*args, directory=root):
    return subprocess.run([str(binary), "--isolated-root", str(state), *args],
                          cwd=directory, env=env, text=True, capture_output=True, timeout=40)


def api(*args, error=None, directory=root):
    result = command(*args, directory=directory)
    assert (result.returncode != 0) == bool(error), (args, result.returncode, result.stdout, result.stderr)
    assert not (result.stdout and result.stderr), (args, "mixed JSON streams")
    envelope = json.loads(result.stderr if error else result.stdout)
    assert envelope["schema_version"] == 1
    if error:
        assert envelope["error"]["code"] == error, envelope
        return envelope["error"]
    return envelope["data"]


def mutate(*args, **kwargs):
    return api(*args, "--request-id", str(uuid.uuid4()), **kwargs)


chosen = set()
def free_port():
    while True:
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        if port not in chosen:
            chosen.add(port)
            return port


http_port, tls_port, web_port, db_port = [free_port() for _ in range(4)]
server = '''import http.server,json,os,socketserver
class Backend(http.server.HTTPServer):
 def server_bind(self):
  socketserver.TCPServer.server_bind(self)
  self.server_name,self.server_port=self.server_address[:2]
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  body=json.dumps({'pid':os.getpid(),'port':os.environ['PORT'],'peer':os.environ.get('PEER')}).encode()
  self.send_response(200);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
 def log_message(self,*args):pass
Backend(('127.0.0.1',int(os.environ['PORT'])),Handler).serve_forever()
'''
config = f'''profile='proxy-auto'
[ports.web]
port={web_port}
range=[20000,65000]
[ports.db]
port={db_port}
range=[20000,65000]
[proxies.front]
host='${{instance.alias}}.auto.localhost'
port='web'
[proxies.alternate]
host='alt.${{instance.alias}}.auto.localhost'
port='web'
[commands.probe]
exec={json.dumps([sys.executable, '-c', 'import json,os,sys;print(json.dumps([os.environ["PORT"],sys.argv[1:]]))'])}
serve=['web']
[commands.probe.bind]
PORT={{port='web'}}
[commands.web]
exec={json.dumps([sys.executable, 'server.py'])}
serve=['web']
[commands.web.bind]
PORT={{port='web'}}
PEER={{port='db',format='url'}}
[commands.web.ready]
exec={json.dumps([sys.executable, '-c', 'import os,urllib.request;urllib.request.urlopen(os.environ["PEER"],timeout=1).read()'])}
timeout='8s'
[commands.db]
exec={json.dumps([sys.executable, 'server.py'])}
serve=['db']
[commands.db.bind]
PORT={{port='db'}}
[commands.worker]
exec={json.dumps([sys.executable, '-c', 'import time;time.sleep(300)'])}
[commands.worker.bind]
PEER={{port='web',format='url'}}
[commands.worker.ready]
exec={json.dumps([sys.executable, '-c', 'import os,urllib.request;urllib.request.urlopen(os.environ["PEER"],timeout=1).read()'])}
timeout='8s'
[commands.delayed]
exec={json.dumps([sys.executable, '-c', 'import time;time.sleep(300)'])}
[commands.delayed.bind]
PEER={{port='web',format='url'}}
[commands.delayed.ready]
exec={json.dumps([sys.executable, '-c', 'from pathlib import Path;import sys;sys.exit(0 if Path("ready.flag").exists() else 1)'])}
'''
for directory in (root, other):
    (directory / "devtools.toml").write_text(config)
    (directory / "server.py").write_text(server)


def request(host, tls=False):
    port = tls_port if tls else http_port
    sock = socket.create_connection(("127.0.0.1", port), timeout=4)
    if tls:
        sock = ssl.create_default_context(cafile=str(home / "auto-ca.pem")).wrap_socket(sock, server_hostname=host)
    sock.sendall(f"GET / HTTP/1.1\r\nHost: {host}:{port}\r\nConnection: close\r\n\r\n".encode())
    response = http.client.HTTPResponse(sock)
    response.begin()
    result = response.status, response.read()
    response.close()
    sock.close()
    return result


try:
    assert api("version")["protocol_version"] == 17
    mutate("proxy", "configure", "--http-address", f"127.0.0.1:{http_port}", "--https-address", f"127.0.0.1:{tls_port}")
    api("instance", "name", "main")
    for flag in ("--no-proxy", "--proxy=false"):
        result = command("run", flag, "probe", "--proxy", "--help")
        assert result.returncode == 0 and json.loads(result.stdout)[1] == ["--proxy", "--help"], result.stderr
        assert not api("proxy", "status")["item"]["running"]
        assert not (state / "data/ca/material.json").exists()
    result = command("cmd", "run", "probe")
    assert result.returncode == 0, result.stderr
    assert api("proxy", "status")["item"]["running"]
    api("proxy", "ca", "export", "--output", str(home / "auto-ca.pem"))
    assert request("main.auto.localhost", True)[0] == 502  # Listener exists before backend.

    # Disabled routes do not expand missing network variables, even when all are disabled.
    for route in ("front", "alternate"):
        mutate("proxy", "down", route)
    missing = config.replace("${instance.alias}.auto.localhost", "${AUTO_MISSING_DOMAIN}")
    (root / "devtools.toml").write_text(missing)
    result = command("command", "run", "probe")
    assert result.returncode == 0, result.stderr
    (root / "devtools.toml").write_text(config)
    result = command("run", "--proxy", "probe")
    assert result.returncode == 0, result.stderr

    # Worker-first selection must launch the DB and web peers before waiting.
    batch = ["project", "up", "worker", "web", "db", "--timeout", "10s", "--request-id", str(uuid.uuid4())]
    started = api(*batch)
    assert [item["status"] for item in started["items"]] == ["ready", "ready", "running"]
    assert all(item["item"]["proxy_mode"] for item in started["items"])
    ids = [item["item"]["id"] for item in started["items"]]
    assert api(*batch)["replayed"]
    reused = mutate("project", "up", "worker", "web", "db", "--timeout", "10s")
    assert not reused["changed"] and [item["item"]["id"] for item in reused["items"]] == ids
    assert api(*batch, "--no-proxy", error="request_conflict")
    web = json.loads(request("main.auto.localhost", True)[1])
    assert web["peer"] == f"http://127.0.0.1:{api('port', 'show', 'db')['item']['port']}"
    assert request("alt.main.auto.localhost")[0] in (301, 302, 307, 308)
    assert json.loads(request("alt.main.auto.localhost", True)[1])["pid"] == web["pid"]

    # This checks partial readiness and replay, not a subsecond startup SLA.
    # A fresh Python/TLS probe needs scheduling time on native Intel macOS.
    # delayed cannot become ready until ready.flag is created below.
    partial_args = ["project", "up", "web", "delayed", "--timeout", "3s", "--request-id", str(uuid.uuid4())]
    partial = api(*partial_args, error="project_operation_failed")["details"]["facts"]["outcomes"]
    assert [item["state"] for item in partial] == ["ready", "pending"], partial
    (root / "ready.flag").touch()
    resumed = api(*partial_args)
    assert resumed["replayed"] and [item["item"]["id"] for item in resumed["items"]] == [item["id"] for item in partial]

    # A sibling worktree shares a profile, while ports, aliases and lifecycle stay scoped.
    api("instance", "name", "feature", directory=other)
    mutate("project", "up", "web", "db", "--timeout", "10s", directory=other)
    sibling = json.loads(request("feature.auto.localhost", True)[1])
    assert sibling["pid"] != web["pid"] and sibling["port"] != web["port"]
    mutate("proxy", "down", "front")
    restarted = mutate("project", "restart", "worker", "web", "db", "--timeout", "10s")
    assert all(item["item"]["id"] != old for item, old in zip(restarted["items"], ids))
    disabled = request("main.auto.localhost")
    assert disabled[0] == 503, disabled  # Known but explicitly inactive route.
    assert request("alt.main.auto.localhost", True)[0] == 200
    assert json.loads(request("feature.auto.localhost", True)[1])["pid"] == sibling["pid"]
    mutate("project", "down")
    assert request("alt.main.auto.localhost", True)[0] == 502  # Down is process-only.
    assert request("feature.auto.localhost", True)[0] == 200

    off = mutate("process", "start", "web", "--no-proxy")["item"]
    assert not off.get("proxy_mode", False)
    replacement = mutate("process", "restart", off["id"])["item"]
    assert not replacement.get("proxy_mode", False)
    mutate("process", "stop", replacement["id"])

    # Immutable old request projection: a completed pre-auto receipt replays without
    # resolving a newly broken declaration or creating a new parent intent journal.
    legacy_id = str(uuid.uuid4())
    projection = {"action": "start", "directory": str(root), "command": "probe", "request_id": legacy_id}
    digest = hashlib.sha256(json.dumps(projection, separators=(",", ":")).encode()).hexdigest()
    receipt = state / "data/processes/receipts" / (legacy_id + ".json")
    receipt.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    receipt.write_text(json.dumps({"fingerprint": digest, "done": True,
                                  "result": {"item": {"id": legacy_id}, "changed": False, "replayed": False}}))
    receipt.chmod(0o600)
    (root / "devtools.toml").write_text(missing)
    replay = api("process", "start", "probe", "--request-id", legacy_id)
    assert replay["replayed"] and not replay["item"].get("proxy_mode", False)
    assert not (state / "data/network-operations" / (legacy_id + ".json")).exists()
    (root / "devtools.toml").write_text(config)
finally:
    for directory in (root, other):
        mutate("project", "down", directory=directory)
    mutate("proxy", "stop", "--all")
print("Automatic proxy preparation: verified HTTP/TLS, disabled/force/off, mixed peers, worktrees, restart and old receipt replay")
