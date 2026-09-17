"""Installed local proxy: routes, daemon lifetime, reservation and live changes."""
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import urllib.error
import urllib.request
import uuid

home = Path.home()
env = dict(os.environ, PATH=str(home / ".local/bin") + ":" + os.environ["PATH"])
main = home / "main"
feature = home / "feature"
main.mkdir()
feature.mkdir()

def execute(*args, cwd=main, expected=0):
    result = subprocess.run(args, cwd=cwd, env=env, text=True, capture_output=True, timeout=20)
    assert result.returncode == expected, (args, result.returncode, result.stdout, result.stderr)
    return json.loads(result.stdout if expected == 0 else result.stderr)

def api(*args, **kwargs):
    expected = kwargs.get("expected", 0)
    return execute("devtools", *args, **kwargs)["data" if expected == 0 else "error"]

def mutate(action, *args, **kwargs):
    return api("proxy", action, *args, "--request-id", str(uuid.uuid4()), **kwargs)

def config(port):
    return f'''profile="shop"
[ports.web]
port={port}
strict=true
[proxies.app]
host="${{instance.alias}}.${{profile}}.localhost"
port="web"
'''

def read(port, host, path="/"):
    request = urllib.request.Request(f"http://127.0.0.1:{port}{path}", headers={"Host": host})
    return urllib.request.urlopen(request, timeout=2).read().decode()

def status(port, host, path="/"):
    try:
        read(port, host, path)
        return 200
    except urllib.error.HTTPError as error:
        return error.code

execute("sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"])
(main / "devtools.toml").write_text(config(24300))
(feature / "devtools.toml").write_text(config(24301))
server = home / "server.py"
server.write_text('''import socket,sys
port,label=int(sys.argv[1]),sys.argv[2]
s=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);s.bind(('127.0.0.1',port));s.listen()
while True:
 c,_=s.accept();data=b''
 while b'\\r\\n\\r\\n' not in data:data+=c.recv(4096)
 if b'GET /socket ' in data and b'Upgrade: websocket' in data:
  c.sendall(b'HTTP/1.1 101 Switching Protocols\\r\\nConnection: Upgrade\\r\\nUpgrade: websocket\\r\\n\\r\\n'+label.encode())
 else:
  body=label.encode();c.sendall(b'HTTP/1.1 200 OK\\r\\nContent-Length: '+str(len(body)).encode()+b'\\r\\nConnection: close\\r\\n\\r\\n'+body)
 c.close()
''')
for directory, alias in ((main, "main"), (feature, "feature")):
    api("port", "allocate", "web", cwd=directory)
    api("instance", "name", alias, cwd=directory)
servers = [subprocess.Popen(["python3", str(server), "24300", "main"], env=env), subprocess.Popen(["python3", str(server), "24301", "feature"], env=env)]
running = False
try:
    time.sleep(.1)
    started = mutate("start", "--port", "24400")
    running = True
    assert started["changed"] and not started["replayed"] and started["item"]["running"] and started["item"]["port"] == 24400
    assert read(24400, "main.shop.localhost") == "main"
    assert read(24400, "feature.shop.localhost") == "feature"
    with socket.create_connection(("127.0.0.1", 24400), timeout=2) as connection:
        connection.sendall(b"GET /socket HTTP/1.1\r\nHost: main.shop.localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
        assert b"101 Switching Protocols" in connection.recv(4096)

    conflict = config(24301).replace('host="${instance.alias}.${profile}.localhost"', 'host="main.shop.localhost"')
    (feature / "devtools.toml").write_text(conflict)
    assert status(24400, "main.shop.localhost") == 503
    routes = [route for route in api("proxy", "list")["items"] if route["host"] == "main.shop.localhost"]
    assert len(routes) == 2 and all(route["status"] == "host_conflict" for route in routes), routes
    (feature / "devtools.toml").write_text(config(24301))
    assert read(24400, "feature.shop.localhost") == "feature"

    servers[0].terminate();servers[0].wait(timeout=5)
    assert status(24400, "main.shop.localhost") == 502
    assert read(24400, "feature.shop.localhost") == "feature"
    api("port", "release", "web")
    (main / "devtools.toml").write_text(config(24302))
    api("port", "allocate", "web")
    servers[0] = subprocess.Popen(["python3", str(server), "24302", "replacement"], env=env)
    time.sleep(.1)
    assert read(24400, "main.shop.localhost") == "replacement"
    assert api("proxy", "status")["item"]["started_at"] == started["item"]["started_at"]

    mutate("stop")
    running = False
    reserved = home / "reserved"
    reserved.mkdir()
    (reserved / "devtools.toml").write_text('profile="reserved"\n[ports.listener]\nport=24400\nstrict=true\n')
    assert api("port", "allocate", "listener", cwd=reserved, expected=3)["code"] == "port_in_use"
    assert mutate("start")["item"]["port"] == 24400
    running = True
    assert read(24400, "feature.shop.localhost") == "feature"
finally:
    if running:
        try: mutate("stop")
        except Exception: pass
    for process in servers:
        if process.poll() is None:
            process.terminate()
        process.wait(timeout=5)

print("Installed proxy routes, conflict and backend failures, WebSocket, dynamic assignments, restart and persistent reservation verified")
