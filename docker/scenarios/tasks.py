"""Exercise installed task workflows and authenticated dashboard on loopback."""
import concurrent.futures
import fcntl
import http.cookiejar
import http.server as http_server
import json
import os
from pathlib import Path
import subprocess
import time
import threading
import urllib.error
import urllib.parse
import urllib.request
import uuid

home = Path.home()
env = dict(os.environ, PATH=str(home / ".local/bin") + ":" + os.environ["PATH"])
project = home / "project"
project.mkdir()

def execute(args, input=None, expected=0, cwd=project):
    result = subprocess.run(args, cwd=cwd, env=env, input=input, text=True,
                            capture_output=True, timeout=20)
    assert result.returncode == expected, (args, result.returncode, result.stderr)
    return json.loads(result.stdout if expected == 0 else result.stderr)["data" if expected == 0 else "error"]

execute(["sh", "/opt/devtools-install.sh", "install", "--version", "0.0.0-test.1", "--source", "/opt/releases"])
def api(*args, **kwargs):
    return execute(["devtools", *args], **kwargs)
def mutate(command, target=None, body=None, revision=None, **options):
    args = ["task", *command.split()]
    if target:
        args.append(target)
    args += ["--request-id", str(uuid.uuid4())]
    if revision is not None:
        args += ["--if-revision", str(revision)]
    for key, value in options.items():
        args += ["--" + key.replace("_", "-"), value]
    if body is not None:
        args += ["--stdin"]
    return api(*args, input=json.dumps(body) if body is not None else None)
def revision():
    return api("task", "list")["revision"]

api("init", "--profile", "fixture")
subprocess.run(["git", "init", "-q"], cwd=project, check=True, env=env)
subprocess.run(["git", "add", "devtools.toml"], cwd=project, check=True, env=env)
subprocess.run(["git", "-c", "user.name=Tester", "-c", "user.email=test@example.invalid", "commit", "-qm", "Initialize"], cwd=project, check=True, env=env)
worktree = home / "worktree"
subprocess.run(["git", "worktree", "add", "-qb", "parallel", str(worktree)], cwd=project, check=True, env=env)
w = mutate("workstream create", body={"title": "Installed workflow"})["item"]["id"]
mutate("workstream spec set", w, {"body": "Share progress across worktrees.", "requirements": [{"key": "R1", "text": "Recover execution"}], "acceptance": [{"key": "A1", "text": "Recovered execution completes", "requirement_keys": ["R1"]}]}, revision())
t = mutate("add", body={"title": "Implement recovery", "workstream_id": w, "acceptance_keys": ["A1"]})["item"]["id"]
v = mutate("validation add", body={"title": "Recovery check", "method": "Installed CLI scenario", "task_id": t})["item"]["id"]
mutate("workstream plan set", w, {"body": "Implement, validate, finish.", "task_ids": [t], "validation_ids": [v]}, revision())
assert api("task", "workstream", "check", w)["valid"]
mutate("workstream activate", w, revision=revision())
assert api("task", "next", cwd=worktree)["item"]["id"] == t

def compete(_):
    result = subprocess.run(["devtools", "task", "claim", t, "--request-id", str(uuid.uuid4())], cwd=worktree, env=env, text=True, capture_output=True, timeout=20)
    return result.returncode, json.loads(result.stdout or result.stderr)
with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
    results = list(pool.map(compete, range(6)))
assert sum(code == 0 for code, _ in results) == 1
claim = next(response["data"] for code, response in results if code == 0)
assert api("task", "current", "--dir", str(worktree))["items"][0]["id"] == claim["run"]["id"]
mutate("checkpoint", claim["run"]["id"], {"summary": "Implementation saved", "next_action": "Run verification"}, context=claim["context"])
takeover = mutate("takeover", t, expected_run=claim["run"]["id"])
err = api("task", "checkpoint", claim["run"]["id"], "--summary", "Stale session", "--context", claim["context"], "--request-id", str(uuid.uuid4()), expected=3)
assert err["code"] == "context_invalid"
basis = mutate("validation basis", v, {"code": []})["basis_id"]
mutate("validation record", v, {"basis_id": basis, "result": "pass", "summary": "Recovery verified", "evidence": [{"kind": "command", "reference": "installed CLI scenario", "description": "claim, checkpoint, and takeover passed"}]}, context=takeover["context"])
request_id = str(uuid.uuid4())
done_args = ["task", "done", t, "--summary", "Recovery works", "--context", takeover["context"], "--request-id", request_id]
first, repeated = api(*done_args), api(*done_args)
assert repeated["replayed"] and not repeated["context_valid"] and first["revision"] == repeated["revision"]
mutate("workstream close", w, revision=revision())
history = json.dumps(api("task", "workstream", "export", w))
assert claim["context"] not in history and takeover["context"] not in history
assert "Implementation saved" in history

# Use only a compiled installed app; no application sources are mounted.
cache = Path(api("project", "inspect")["paths"]["cache"]) / "dashboard"
cache.mkdir(mode=0o700, parents=True, exist_ok=True)
legacy_lock = (cache / "serve.lock").open("w")
os.chmod(legacy_lock.name, 0o600)
fcntl.flock(legacy_lock, fcntl.LOCK_EX)
legacy_id = "legacy-auth-fixture"
class Legacy(http_server.BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def do_POST(self):
        action = json.loads(self.rfile.read(int(self.headers["Content-Length"])))["action"]
        self.send_response(200)
        self.end_headers()
        self.wfile.write(json.dumps({"server_id": legacy_id}).encode())
        if action == "stop":
            def finish():
                legacy.shutdown()
                (cache / "server.json").unlink()
                fcntl.flock(legacy_lock, fcntl.LOCK_UN)
                legacy_lock.close()
            threading.Thread(target=finish, daemon=True).start()
legacy = http_server.ThreadingHTTPServer(("127.0.0.1", 0), Legacy)
(cache / "server.json").write_text(json.dumps({"id": legacy_id, "address": f"http://127.0.0.1:{legacy.server_port}", "token": "legacy-fixture"}))
os.chmod(cache / "server.json", 0o600)
threading.Thread(target=legacy.serve_forever, daemon=True).start()
dashboard = api("dashboard")
assert dashboard["server_id"] != legacy_id
legacy.server_close()
assert api("dashboard")["server_id"] == dashboard["server_id"]
url = urllib.parse.urlsplit(dashboard["url"])
origin = f"{url.scheme}://{url.netloc}"
bootstrap = urllib.parse.parse_qs(url.fragment)["token"][0]
cookie_jar = http.cookiejar.CookieJar()
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookie_jar))
session_token = ""
def http(path, body=None, expected=200, headers=None):
    request_headers = dict(headers or {})
    if session_token and path.startswith("/api/"):
        request_headers["Authorization"] = "Bearer " + session_token
    request = urllib.request.Request(origin + path, data=None if body is None else json.dumps(body).encode(), headers=request_headers)
    try:
        response = client.open(request, timeout=3)
    except urllib.error.HTTPError as error:
        assert error.code == expected, error
        return error.read()
    assert response.status == expected
    return response.read()
try:
    assert b"canvas" in http("/")
    assert b"v7.9.0" in http("/d3.min.js")
    http("/api/profiles", expected=401)
    session_token = json.loads(http("/session", {"token": bootstrap}, headers={"Origin": origin, "Content-Type": "application/json"}))["token"]
    assert len(cookie_jar) == 0
    cookie_only = urllib.request.Request(origin + "/api/profiles", headers={"Cookie": "devtools_session=" + session_token})
    try:
        client.open(cookie_only, timeout=3)
        raise AssertionError("Cookie-only request authenticated")
    except urllib.error.HTTPError as e:
        assert e.code == 401
    received = []
    class OtherService(http_server.BaseHTTPRequestHandler):
        def log_message(self, *args): pass
        def do_GET(self):
            received.append((self.headers.get("Cookie"), self.headers.get("Authorization")))
            self.send_response(200)
            self.end_headers()
    other = http_server.ThreadingHTTPServer(("127.0.0.1", 0), OtherService)
    thread = threading.Thread(target=other.handle_request, daemon=True)
    thread.start()
    client.open(f"http://127.0.0.1:{other.server_port}/", timeout=3).close()
    thread.join(timeout=3)
    other.server_close()
    assert received == [(None, None)]
    http("/session", {"token": bootstrap}, expected=401, headers={"Origin": origin})
    assert json.loads(http("/api/profiles"))["profiles"] == ["fixture"]
    graph = json.loads(http("/api/query?" + urllib.parse.urlencode({"profile": "fixture", "command": "workstream tree"})))
    assert graph["nodes"][0]["id"] == w
    http("/api/profiles", expected=403, headers={"Origin": "https://example.invalid"})
    assert api("dashboard", "status")["running"]
finally:
    assert api("dashboard", "stop")["stopped"]
for _ in range(40):
    if not api("dashboard", "status")["running"]:
        break
    time.sleep(.05)
else:
    raise AssertionError("Dashboard did not stop")
assert api("task", "show", t)["item"]["state"] == "done"
print("Installed task/workstream recovery, concurrency, worktree sharing, and dashboard verified")
