"""A repository/worktree workflow combining tasks, servers, bindings and recovery."""
import json
import os
from pathlib import Path
import runpy
import subprocess
import time
import urllib.parse
import urllib.request
import uuid

# Reuse the complete workstream specification, validation and takeover fixture.
fixture=runpy.run_path(str(Path(__file__).with_name("tasks.py")))
api=fixture["api"];project=fixture["project"];worktree=fixture["worktree"];env=fixture["env"]
config='''profile="fixture"
[ports.web]
port=26100
range=[26100,26199]
[commands.web]
exec=["python3","server.py"]
inject=true
serve=["web"]
[commands.web.bind]
PORT={port="web"}
'''
server='''import http.server,os,socketserver
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200);self.end_headers();self.wfile.write(os.environ['MESSAGE'].encode())
 def log_message(self,*args):pass
socketserver.TCPServer.allow_reuse_address=True
socketserver.TCPServer(('127.0.0.1',int(os.environ['PORT'])),Handler).serve_forever()
'''
for directory in (project,worktree):
    (directory/"devtools.toml").write_text(config)
    (directory/"server.py").write_text(server)
api("instance","name","main")
api("var","set","MESSAGE","--value","first")
def start(cwd):return api("process","start","web","--request-id",str(uuid.uuid4()),cwd=cwd)["item"]
main=start(project);parallel=start(worktree)
main_port=api("port","show","web")["port"]
other_port=api("port","show","web",cwd=worktree)["port"]
assert main_port!=other_port
def read(port,message):
    for _ in range(80):
        try:
            if urllib.request.urlopen(f"http://127.0.0.1:{port}/",timeout=.2).read().decode()==message:return
        except OSError:pass
        time.sleep(.05)
    raise AssertionError("Service response mismatch")
read(main_port,"first");read(other_port,"first")
frontend=Path.home()/"frontend";frontend.mkdir()
(frontend/"devtools.toml").write_text('''profile="frontend"
[commands.check]
exec=["python3","-c","import os,urllib.request;print(urllib.request.urlopen(os.environ['BACKEND_URL']).read().decode())"]
[commands.check.bind]
BACKEND_PORT={profile="fixture",instance="main",port="web"}
BACKEND_URL={template="http://127.0.0.1:${bind.BACKEND_PORT}/"}
''')
result=subprocess.run(["devtools","run","check"],cwd=frontend,env=env,text=True,capture_output=True,timeout=10)
assert result.returncode==0 and result.stdout.strip()=="first"
identity=project/"identity";recipient=project/"recipient";backups=project/"backups"
api("backup","keygen","--identity-file",str(identity),"--recipient-file",str(recipient))
api("backup","configure","--directory",str(backups),"--recipient-file",str(recipient))
url=urllib.parse.urlsplit(api("dashboard")["url"]);origin=f"{url.scheme}://{url.netloc}"
def http(path,body=None,token=""):
    headers={"Origin":origin,"Content-Type":"application/json"}
    if token:headers["Authorization"]="Bearer "+token
    req=urllib.request.Request(origin+path,data=None if body is None else json.dumps(body).encode(),headers=headers)
    return json.load(urllib.request.urlopen(req,timeout=15))
token=http("/session",{"token":urllib.parse.parse_qs(url.fragment)["token"][0]})["token"]
def action(domain,body,profile="fixture"):return http("/api/actions",{"domain":domain,"profile":profile,domain:body},token)["data"]
assert len(http("/api/processes?profile=fixture",token=token)["items"])==2
details=http("/api/project?"+urllib.parse.urlencode({"profile":"fixture","instance":main["instance_id"]}),token=token)
assert details["commands"][0]["name"]=="web"
backup_request={"action":"create","request_id":str(uuid.uuid4())}
saved=action("backup",backup_request)
assert action("backup",backup_request)["replayed"]
api("var","set","MESSAGE","--value","second")
restarted=action("process",{"action":"restart","id":main["id"],"request_id":str(uuid.uuid4())})["item"]
read(main_port,"second");read(other_port,"first")
restore={"action":"restore","file":saved["path"],"identity_file":str(identity),"source_profile":"fixture"}
preview=action("backup",restore,"recovered")
action("backup",dict(restore,digest=preview["digest"],request_id=str(uuid.uuid4())),"recovered")
assert api("var","get","MESSAGE","--profile","recovered")["value"]=="first"
assert api("task","workstream","show",fixture["w"],"--profile","recovered")["item"]["state"]=="done"
saved_path=Path(saved["path"]);held=saved_path.with_suffix(".held")
saved_path.rename(held)
assert action("backup",backup_request)["replayed"] and not saved_path.exists()
held.rename(saved_path)
action("process",{"action":"stop","id":parallel["id"],"request_id":str(uuid.uuid4())})
subprocess.run(["git","worktree","remove","--force",str(worktree)],cwd=project,env=env,check=True)
plan=http("/api/cleanup-preview?profile=fixture",token=token)
item=next(i for i in plan["items"] if i["kind"]=="missing_instance")
cleanup_request={"action":"apply","plan":plan["id"],"ids":[item["id"]],"request_id":str(uuid.uuid4())}
action("cleanup",cleanup_request)
assert action("cleanup",cleanup_request)["replayed"]
action("cleanup",{"action":"restore","id":item["id"]})
action("process",{"action":"stop","id":restarted["id"],"request_id":str(uuid.uuid4())})
api("dashboard","stop")
print("Integrated repository/worktree workflow verified: task recovery, services, cross-profile URL, dashboard backup/restore and cleanup")
