"""Installed named services: lifetime, identity, retries, ports, values and logs."""
import concurrent.futures
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import urllib.request
import uuid

root=Path.home()/"project"
root.mkdir()
env=dict(os.environ, PATH=str(Path.home()/".local/bin")+":"+os.environ["PATH"])
def execute(*args,input=None,expected=0):
    p=subprocess.run(args,cwd=root,env=env,input=input,text=True,capture_output=True,timeout=25)
    assert p.returncode==expected,(args,p.stdout,p.stderr)
    return json.loads(p.stdout if expected==0 else p.stderr)
execute("sh",os.environ["DEVTOOLS_TEST_INSTALLER"],"install","--version","0.0.0-test.1","--source",os.environ["DEVTOOLS_TEST_RELEASES"])
def api(*args,**kwargs): return execute("devtools",*args,**kwargs)["data" if kwargs.get("expected",0)==0 else "error"]
def mutate(action,*args,**kwargs): return api("process",action,*args,"--request-id",str(uuid.uuid4()),**kwargs)
config='''profile="services"
[ports.web]
port=24100
range=[24100,24199]
[commands.web]
exec=["python3","server.py"]
serve=["web"]
inject=true
[commands.web.bind]
PORT={port="web"}
[commands.fail]
exec=["sh","-c","exit 7"]
[commands.log]
exec=["python3","-c","print('x'*2000000)"]
[ports.tree]
port=24200
range=[24200,24299]
[commands.tree]
exec=["python3","tree.py"]
serve=["tree"]
[commands.tree.bind]
PORT={port="tree"}
[commands.stubborn]
exec=["python3","tree.py","hold"]
serve=["tree"]
[commands.stubborn.bind]
PORT={port="tree"}
'''
(root/"devtools.toml").write_text(config)
(root/"server.py").write_text('''import http.server,os,socketserver
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200);self.end_headers();self.wfile.write(os.environ.get('MESSAGE','empty').encode())
 def log_message(self,*args):pass
socketserver.TCPServer.allow_reuse_address=True
socketserver.TCPServer(('127.0.0.1',int(os.environ['PORT'])),Handler).serve_forever()
''')
(root/"tree.py").write_text('''import os,signal,subprocess,sys,time
child=subprocess.Popen([sys.executable,"-c","import os,signal,socket,time;signal.signal(signal.SIGTERM,signal.SIG_IGN);s=socket.socket();s.bind(('127.0.0.1',int(os.environ['PORT'])));s.listen();print('ready',flush=True);time.sleep(60)"],stdout=subprocess.PIPE)
assert child.stdout.readline().strip()==b'ready'
if len(sys.argv)>1:
 signal.signal(signal.SIGTERM,signal.SIG_IGN)
 while True:time.sleep(1)
''')
api("var","set","MESSAGE","--value","first")
api("env","create","local")
first_id=str(uuid.uuid4())
first=api("process","start","web","--request-id",first_id)
id=first["item"]["id"]
assert first["item"]["state"]=="running",first
assert api("process","start","web","--request-id",first_id)["replayed"]
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    results=list(pool.map(lambda _:mutate("start","web"),range(4)))
assert all(r["item"]["id"]==id and not r["changed"] for r in results)
assert mutate("start","web","--env","local",expected=3)["code"]=="process_conflict"
port=api("port","show","web")["port"]
def read(expected):
    for _ in range(60):
        try:
            if urllib.request.urlopen(f"http://127.0.0.1:{port}",timeout=.2).read().decode()==expected:return
        except OSError:pass
        time.sleep(.05)
    raise AssertionError("Server response mismatch")
read("first")
api("dashboard")
api("dashboard","stop")
read("first")
assert api("process","logs",id,expected=3)["code"]=="logs_disabled"
api("var","set","MESSAGE","--value","second")
second=mutate("restart",id)["item"]
assert second["previous_id"]==id and second["id"]!=id
read("second")
assert not mutate("stop",id)["changed"]
read("second")
assert mutate("stop",second["id"])["item"]["state"]=="stopped"
with socket.socket() as s:
    s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
    s.bind(("127.0.0.1",port))
def finished(id):
    for _ in range(100):
        r=api("process","status",id)
        if r["ended_at"]:return r
        time.sleep(.025)
    raise AssertionError("Process did not finish")
failed=finished(mutate("start","fail")["item"]["id"])
assert failed["exit_code"]==7 and failed["reason"]=="nonzero_exit"
logged=finished(mutate("start","log","--capture-logs")["item"]["id"])
assert len(api("process","logs",logged["id"])["content"].encode())<=1<<20
tree=finished(mutate("start","tree")["item"]["id"])
assert tree["exit_code"]==0,tree
tree_port=api("port","show","tree")["port"]
def free_tree():
    for _ in range(80):
        with socket.socket() as s:
            try:s.bind(("127.0.0.1",tree_port));return
            except OSError:time.sleep(.025)
    raise AssertionError("Descendant still holds port")
free_tree()
stubborn=mutate("start","stubborn")["item"]
time.sleep(.2)
assert mutate("stop",stubborn["id"])["item"]["exit_code"]==137
free_tree()
print("Installed process lifetime, idempotency, concurrent starts, env conflict, restart, ports and bounded logs verified")
