"""Installed storage preview, conflict protection, archival and restoration."""
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time
import uuid

root=Path.home()/"project";root.mkdir()
env=dict(os.environ,PATH=str(Path.home()/".local/bin")+":"+os.environ["PATH"])
def execute(*args,input=None,expected=0):
    p=subprocess.run(args,cwd=root,env=env,input=input,text=True,capture_output=True,timeout=25)
    assert p.returncode==expected,(args,p.stdout,p.stderr)
    return json.loads(p.stdout if expected==0 else p.stderr)["data" if expected==0 else "error"]
execute("sh","/opt/devtools-install.sh","install","--version","0.0.0-test.1","--source","/opt/releases")
def api(*args,**kwargs):return execute("devtools",*args,**kwargs)
api("init","--profile","cleanup-fixture")
paths=api("project","inspect")["paths"]
data=Path(paths["data"]);cache=Path(paths["cache"])
query=cache/"task-queries"/(str(uuid.uuid4())+".json");query.parent.mkdir(mode=0o700,parents=True)
query.write_text(json.dumps({"expires":"2000-01-01T00:00:00Z","items":[]}));query.chmod(0o600)
plan=api("cleanup","preview")
item=next(i for i in plan["items"] if i["kind"]=="expired_query")
query.write_text(query.read_text()+" ")
assert api("cleanup","apply",plan["id"],"--item",item["id"],"--request-id",str(uuid.uuid4()),expected=3)["code"]=="revision_conflict"
plan=api("cleanup","preview");item=next(i for i in plan["items"] if i["kind"]=="expired_query");rid=str(uuid.uuid4())
api("cleanup","apply",plan["id"],"--item",item["id"],"--request-id",rid)
assert not query.exists()
assert api("cleanup","apply",plan["id"],"--item",item["id"],"--request-id",rid)["replayed"]
api("cleanup","restore",item["id"])
assert query.exists()
assert api("cleanup","purge",item["id"],expected=3)["code"]=="retention_active"
created=api("task","add","--title","Completed fixture","--request-id",str(uuid.uuid4()))
tid=created["item"]["id"]
api("task","cancel",tid,"--reason","Fixture complete","--if-revision",str(created["revision"]),"--request-id",str(uuid.uuid4()))
journal=data/"tasks"/("cleanup-fixture".encode().hex()+".json")
body=json.loads(journal.read_text())
for event in body["events"]:event["occurred_at"]="2000-01-01T00:00:00Z"
journal.write_text(json.dumps(body))
plan=api("cleanup","preview","--profile","cleanup-fixture")
item=next(i for i in plan["items"] if i["kind"]=="completed_tasks")
api("cleanup","apply",plan["id"],"--item",item["id"],"--request-id",str(uuid.uuid4()))
assert api("task","list")["items"]==[]
api("cleanup","restore",item["id"])
assert api("task","show",tid)["item"]["state"]=="canceled"
ghost=root/"gone";ghost.mkdir()
(ghost/"devtools.toml").write_text('profile="gone"\n[ports.web]\nport=25100\nrange=[25100,25199]\n')
allocated=api("port","allocate","web","--dir",str(ghost))
shutil.rmtree(ghost)
plan=api("cleanup","preview","--profile","gone")
item=next(i for i in plan["items"] if i["kind"]=="missing_instance")
with socket.socket() as s:
    s.bind(("127.0.0.1",allocated["port"]))
    assert api("cleanup","apply",plan["id"],"--item",item["id"],"--request-id",str(uuid.uuid4()),expected=3)["code"]=="revision_conflict"
plan=api("cleanup","preview","--profile","gone");item=next(i for i in plan["items"] if i["kind"]=="missing_instance")
api("cleanup","apply",plan["id"],"--item",item["id"],"--request-id",str(uuid.uuid4()))
assert api("instance","list","--profile","gone")["items"]==[]
api("cleanup","restore",item["id"])
assert len(api("instance","list","--profile","gone")["items"])==1
identity=root/"identity";recipient=root/"recipient";backups=root/"backups"
api("backup","keygen","--identity-file",str(identity),"--recipient-file",str(recipient))
api("backup","configure","--directory",str(backups),"--recipient-file",str(recipient))
for n in range(4):
    path=api("backup","create")["path"];age=time.time()-(40-n)*86400;os.utime(path,(age,age))
plan=api("cleanup","preview")
assert len([i for i in plan["items"] if i["kind"]=="old_backup"])==1
print("Installed cleanup stale previews, task archive/restore, occupied port protection, missing instances and backup retention verified")
