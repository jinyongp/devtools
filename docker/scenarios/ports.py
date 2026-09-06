"""Exercise persistent assignments and real server/consumer processes after install."""
import concurrent.futures
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time

home = Path.home()
env = dict(os.environ, PATH=str(home / ".local/bin") + ":" + os.environ["PATH"])
root = home / "backend"
root.mkdir()

def execute(*args, cwd=root, expected=0, input=None):
    r = subprocess.run(args, cwd=cwd, env=env, text=True, input=input,
                       capture_output=True, timeout=20)
    assert r.returncode == expected, (args, r.returncode, r.stdout, r.stderr)
    assert "secret-canary" not in r.stdout + r.stderr
    return r

def api(*args, **kwargs):
    r = execute("devtools", *args, **kwargs)
    return json.loads(r.stdout or r.stderr)

execute("sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"])
config = '''profile="backend"
[ports.api]
port=23000
range=[23000,23099]
[commands.server]
exec=["python3","server.py","${bind.PORT}"]
serve=["api"]
[commands.server.bind]
PORT={port="api"}
[commands.read]
exec=["python3","-c","import os; print(os.environ['URL'])"]
[commands.read.bind]
PORT={port="api"}
URL={template="http://${var.HOST}:${bind.PORT}/api/v1"}
'''
(root / "devtools.toml").write_text(config)
(root / "server.py").write_text('''import socket,sys,time
s=socket.socket()
s.bind(('127.0.0.1',int(sys.argv[1])))
s.listen()
print('ready',flush=True)
while True: time.sleep(1)
''')
paths = api("project", "inspect")["data"]["paths"]
assert api("doctor", "server")["data"]["ready"]
assert not (Path(paths["data"]) / "ports").exists()
api("var", "set", "HOST", "--value", "127.0.0.1")
instance = api("instance", "name", "main")["data"]
first = api("port", "allocate", "api")["data"]
assert first["created"] and first["port"] == 23000
assert not api("port", "allocate", "api")["data"]["created"]

# Configuration changes preserve committed assignments.
(root / "devtools.toml").write_text(config.replace("port=23000", "port=23100\nstrict=true"))
assert api("port", "allocate", "api")["data"]["port"] == 23000
(root / "devtools.toml").write_text(config)

server = subprocess.Popen(["devtools", "run", "server"], cwd=root, env=env,
                          text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
try:
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", 23000), timeout=.1): break
        except OSError: time.sleep(.05)
    else: raise AssertionError("server did not listen")
    assert api("port", "check", "api")["data"]["occupancy"] == "in_use"
    assert api("run", "server", expected=3)["error"]["code"] == "port_run_active"
    assert api("port", "release", "api", expected=3)["error"]["code"] == "port_run_active"
    assert execute("devtools", "run", "read").stdout.strip() == "http://127.0.0.1:23000/api/v1"
    assert not api("doctor", "server")["data"]["ready"]
    assert api("doctor", "read")["data"]["ready"]

    frontend = home / "frontend"
    frontend.mkdir()
    (frontend / "devtools.toml").write_text('''profile="frontend"
[commands.read]
exec=["python3","-c","import os; print(os.environ['URL'])"]
[commands.read.bind]
P={profile="backend",instance="main",port="api"}
URL={template="http://${var.HOST}:${bind.P}/v1"}
''')
    api("var", "set", "HOST", "--value", "localhost", cwd=frontend)
    assert execute("devtools", "run", "read", cwd=frontend).stdout.strip() == "http://localhost:23000/v1"
    api("sec", "set", "URL", "--stdin", input="secret-canary", cwd=frontend)
    assert api("run", "read", cwd=frontend, expected=3)["error"]["code"] == "binding_conflict"
finally:
    server.terminate()
    server.communicate(timeout=8)

# An unrelated listener causes an error, preserving the registered address.
with socket.socket() as busy:
    busy.bind(("127.0.0.1", 23000))
    busy.listen()
    assert api("run", "server", expected=3)["error"]["code"] == "port_in_use"
    assert api("port", "show", "api")["data"]["port"] == 23000

# Concurrent locations sharing one profile receive distinct persisted ports.
locations = []
for n in range(5):
    d = home / f"copy-{n}"
    d.mkdir()
    (d / "devtools.toml").write_text(config)
    locations.append(d)
with concurrent.futures.ThreadPoolExecutor() as pool:
    allocations = list(pool.map(lambda d: api("port", "allocate", "api", cwd=d)["data"], locations))
assert len({a["port"] for a in allocations}) == len(locations)
assert all(a["port"] != 23000 for a in allocations)

# A real Git worktree uses its own root while sharing the profile.
execute("git", "init")
execute("git", "add", "devtools.toml", "server.py")
execute("git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
worktree = home / "backend-worktree"
execute("git", "worktree", "add", "--detach", str(worktree))
wt = api("port", "allocate", "api", cwd=worktree)["data"]
assert wt["instance_id"] != instance["instance_id"] and wt["port"] != 23000
execute("git", "worktree", "remove", str(worktree))
assert len(api("port", "prune")["data"]["removed"]) == 1

moved = home / "moved"
root.rename(moved)
changed = api("instance", "move", "main", "--profile", "backend", "--dir", str(moved), cwd=moved)["data"]
assert changed["instance_id"] == instance["instance_id"]
assert api("port", "show", "api", cwd=moved)["data"]["port"] == 23000
assert api("port", "release", "api", cwd=moved)["data"]["released"]
assert not api("port", "release", "api", cwd=moved)["data"]["released"]
assert api("instance", "remove", "main", cwd=moved)["data"]["removed"]
for d in locations: shutil.rmtree(d)
assert all(x["location_status"] == "missing" for x in api("instance", "list", "--profile", "backend", cwd=moved)["data"]["items"])
assert len(api("port", "prune", cwd=moved)["data"]["removed"]) == len(locations)
print("Installed port persistence, concurrency, real server bindings, cross-profile references, and cleanup verified")
