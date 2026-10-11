"""Packaged network journeys; no system hosts/trust/capability or provider writes."""
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import time
import uuid

home = Path.home()
binary = home / ".local/bin/devtools"
subprocess.run(["sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version",
                "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"]],
               check=True, capture_output=True, timeout=30)
project = home / "network-project"
project.mkdir()
isolated = home / "network-state"
env = dict(os.environ)
env.pop("DEVTOOLS_TASK_CONTEXT", None)
env.pop("DEVTOOLS_ISOLATED_ROOT", None)

# Validate observable data against the packaged CLI's published network schemas.
catalog = json.loads(subprocess.check_output([str(binary),"schema","--all"],env=env,text=True))["data"]
schemas = {c["name"]:c.get("output_schema") for c in catalog["commands"]}
def conforms(schema, value):
    if "oneOf" in schema:
        return sum(conforms(s,value) for s in schema["oneOf"]) == 1
    if "const" in schema and value != schema["const"]: return False
    if "enum" in schema and value not in schema["enum"]: return False
    types = schema.get("type", [])
    if isinstance(types,str): types=[types]
    matches = {"object":isinstance(value,dict),"array":isinstance(value,list),"string":isinstance(value,str),
               "integer":type(value) is int,"boolean":type(value) is bool,"null":value is None,"number":type(value) in (int,float)}
    if types and not any(matches[t] for t in types): return False
    if isinstance(value,dict):
        if any(k not in value for k in schema.get("required",[])): return False
        properties = schema.get("properties",{})
        for key,entry in value.items():
            child = properties.get(key,schema.get("additionalProperties",True))
            if child is False or isinstance(child,dict) and not conforms(child,entry): return False
    if isinstance(value,list) and "items" in schema:
        if any(not conforms(schema["items"],entry) for entry in value): return False
    if type(value) in (int,float):
        if "minimum" in schema and value < schema["minimum"]: return False
        if "maximum" in schema and value > schema["maximum"]: return False
    return True

def api(*args, cwd=project, error=None):
    p = subprocess.run([str(binary), "--isolated-root", str(isolated), *args],
                       cwd=cwd, env=env, text=True, capture_output=True, timeout=45)
    assert (p.returncode != 0) == bool(error), (args, p.returncode, p.stdout, p.stderr)
    assert not (p.stdout and p.stderr), (args, "mixed JSON streams")
    envelope = json.loads(p.stderr if error else p.stdout)
    assert envelope["schema_version"] == 1
    if error:
        assert envelope["error"]["code"] == error, envelope
        return envelope["error"]
    name = next((n for n in sorted(schemas,key=len,reverse=True) if list(args[:len(n.split())]) == n.split()),None)
    if name and (name.startswith(("proxy ","expose ")) or name == "command env") and schemas[name]:
        assert conforms(schemas[name],envelope["data"]), (name,"output schema mismatch",envelope["data"])
    return envelope["data"]

def mutate(*args, **kwargs):
    return api(*args, "--request-id", str(uuid.uuid4()), **kwargs)

chosen = set()
def free_port():
    while True:
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        if port not in chosen:
            chosen.add(port)
            return port

def tree_digest(path):
    if not path.exists():
        return None
    return sorted((str(p.relative_to(path)), hashlib.sha256(p.read_bytes()).hexdigest())
                  for p in path.rglob("*") if p.is_file())

normal = home / ".local/share/devtools"
normal_before = tree_digest(normal)
http_port, tls_port, preview_http, preview_tls = [free_port() for _ in range(4)]
web_port, api_port = free_port(), free_port()
(project / "server.py").write_text('''import http.server,json,os,socketserver
class Backend(http.server.HTTPServer):
 def server_bind(self):
  socketserver.TCPServer.server_bind(self)
  self.server_name,self.server_port=self.server_address[:2]
class H(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  body=json.dumps({'pid':os.getpid(),'env':{k:os.environ.get(k) for k in ('PORT','API_URL','PUBLIC_URL','DEVTOOLS_ISOLATED_ROOT')},'headers':dict(self.headers)}).encode()
  self.send_response(200);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
 def log_message(self,*a):pass
Backend(('127.0.0.1',int(os.environ['PORT'])),H).serve_forever()
''')
(project / "ready.py").write_text('''import json,os,urllib.request,pathlib
pid=json.load(urllib.request.urlopen(os.environ['API_URL'],timeout=1))['pid']
old=pathlib.Path('old-api-pid')
assert not old.exists() or int(old.read_text())!=pid
''')
(project / "devtools.toml").write_text(f'''profile='network-fixture'
env_files=['missing.env','.env']
[ports.web]
port={web_port}
strict=true
[ports.api]
port="${{NETWORK_FIXTURE_PORT:-{api_port}}}"
strict=true
[proxies.web]
host='${{instance.alias}}.network.localhost'
port='web'
[proxies.api]
host='api.network.localhost'
port='api'
[commands.web]
exec=['python3','server.py']
serve=['web']
inject=false
[commands.web.bind]
PORT={{port='web'}}
API_URL={{port='api',format='url'}}
PUBLIC_URL={{proxy='web'}}
[commands.web.ready]
exec=['python3','ready.py']
timeout='8s'
[commands.api]
exec=['python3','server.py']
serve=['api']
inject=false
[commands.api.bind]
PORT={{port='api'}}
''')
(project / ".env").write_text("UNREFERENCED_NETWORK_SECRET=NETWORK_PRIVATE_CANARY\n")

def request(port, host, tls=True, path="/", headers=None):
    s = socket.create_connection(("127.0.0.1", port), timeout=4)
    if tls:
        context = ssl.create_default_context(cafile=str(home / "public-ca.pem"))
        s = context.wrap_socket(s, server_hostname=host)
    authority = host + (f":{port}" if port not in (80, 443) else "")
    extra = "".join(f"{k}: {v}\r\n" for k, v in (headers or {}).items())
    s.sendall(f"GET {path} HTTP/1.1\r\nHost: {authority}\r\nConnection: close\r\n{extra}\r\n".encode())
    response = http.client.HTTPResponse(s)
    response.begin()
    result = response.status, dict(response.getheaders()), response.read()
    response.close()
    s.close()
    return result

external = None
try:
    assert api("proxy", "ca", "status")["item"]["present"] is False
    assert not isolated.exists(), "read-only CA status initialized root"
    mutate("proxy", "configure", "--http-address", f"127.0.0.1:{http_port}",
           "--https-address", f"127.0.0.1:{tls_port}")
    mutate("proxy", "configure", "--listener", "preview", "--http-address", f"127.0.0.1:{preview_http}",
           "--https-address", f"127.0.0.1:{preview_tls}")
    api("instance", "name", "main")
    operation = str(uuid.uuid4())
    up = api("project", "up", "web", "api", "--proxy", "--request-id", operation, "--timeout", "12s")
    assert all(i["status"] in ("ready", "running") and i["item"]["proxy_mode"] for i in up["items"]), up
    assert api("project", "up", "web", "api", "--proxy", "--request-id", operation, "--timeout", "12s")["replayed"]
    public = api("command", "env", "web")
    assert public["env"]["PUBLIC_URL"] == f"https://main.network.localhost:{tls_port}"
    assert public["env"]["API_URL"] == f"http://127.0.0.1:{api_port}"
    assert "NETWORK_PRIVATE_CANARY" not in json.dumps(public)
    api("proxy", "ca", "export", "--output", str(home / "public-ca.pem"))
    code, _, body = request(tls_port, "main.network.localhost", headers={"X-Forwarded-Proto":"spoof"})
    assert code == 200
    observed = json.loads(body)
    assert observed["env"]["DEVTOOLS_ISOLATED_ROOT"] == str(isolated)
    assert observed["headers"]["X-Forwarded-Proto"] == "https"
    code, headers, _ = request(http_port, "main.network.localhost", tls=False, path="/path?q=1")
    assert code in (301, 302, 307, 308) and headers["Location"] == f"https://main.network.localhost:{tls_port}/path?q=1"
    with __import__('urllib.request',fromlist=['urlopen']).urlopen(public["env"]["API_URL"], timeout=2) as r:
        (project / "old-api-pid").write_text(str(json.load(r)["pid"]))
    restarted = mutate("project", "restart", "web", "api", "--timeout", "12s")
    assert restarted["items"][0]["status"] == "ready"
    assert all(i["item"]["previous_id"] == old["item"]["id"] for i, old in zip(restarted["items"], up["items"]))

    # Already busy external backend is referenced without taking its process claim.
    port = free_port()
    external = subprocess.Popen(["python3", "server.py"], cwd=project, env=dict(env, PORT=str(port)), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(60):
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=.1): break
        except OSError: time.sleep(.05)
    mutate("proxy", "add", "external", str(port), "--global", "--host", "external.localhost", "--listener", "preview", cwd=home)
    mutate("proxy", "up", "external", "--global", cwd=home)
    assert request(preview_tls, "external.localhost")[0] == 200
    fingerprint = api("proxy", "ca", "status")["item"]["fingerprint"]
    mutate("proxy", "stop", "--listener", "default")
    assert api("proxy", "status", "--listener", "preview")["item"]["running"]
    assert request(preview_tls, "external.localhost")[0] == 200
    plan = api("proxy", "teardown", "--listener", "default")["item"]
    mutate("proxy", "teardown", "--listener", "default", "--apply", plan["digest"])
    assert api("proxy", "ca", "status")["item"]["fingerprint"] == fingerprint
    assert request(preview_tls, "external.localhost")[0] == 200
    assert external.poll() is None
    assert api("proxy", "env", "external", "--global", cwd=home)["env"]["PORT"] == str(port)
    p = subprocess.run([str(binary),"--isolated-root",str(isolated),"proxy","run","external","--global","--","true"],cwd=home,env=env,capture_output=True,text=True)
    assert p.returncode != 0, "foreground runner accepted busy external backend"
    external.terminate();external.wait(timeout=5);external=None
    p = subprocess.run([str(binary),"--isolated-root",str(isolated),"proxy","run","external","--global","--","python3","-c","import os,sys; print(os.environ['PORT'],os.environ['DEVTOOLS_EXTERNAL_ROUTE_URL']);sys.exit(7)"],cwd=home,env=env,capture_output=True,text=True,timeout=10)
    assert p.returncode == 7 and p.stdout.strip() == f"{port} https://external.localhost:{preview_tls}", (p.returncode,p.stdout,p.stderr)
    mutate("proxy","down","external","--global",cwd=home)
    mutate("proxy","stop","--listener","preview")
    p=subprocess.run([str(binary),"--isolated-root",str(isolated),"proxy","run","external","--global","--","true"],cwd=home,env=env,capture_output=True,text=True,timeout=15)
    assert p.returncode==0,(p.returncode,p.stdout,p.stderr)
    assert api("proxy","status","--listener","preview")["item"]["running"]
    assert api("proxy","env","external","--global",cwd=home)["item"]["status"]=="ready"
    mutate("proxy", "add", "custom", str(free_port()), "--global", "--host", "custom.network.test", cwd=home)
    api("proxy", "up", "custom", "--global", "--request-id", str(uuid.uuid4()), cwd=home, error="isolated_operation_not_allowed")
    assert tree_digest(normal) == normal_before, "explicit root changed normal user data"
    for group in (("schema","proxy","configure"),("schema","proxy","teardown"),("schema","expose","start"),("schema","command","env")):
        assert api(*group)["output_mode"] == "json"
    print(json.dumps({"result":"pass","fresh_batch":True,"peer_dependent_restart":True,"verified_https":True,"selected_teardown":True,"busy_backend":True,"projectless_foreground":True,"isolated":True}))
finally:
    if external:
        external.terminate();external.wait(timeout=5)
    mutate("project", "down")
    plan = api("proxy", "teardown", "--all")["item"]
    mutate("proxy", "teardown", "--all", "--apply", plan["digest"])
