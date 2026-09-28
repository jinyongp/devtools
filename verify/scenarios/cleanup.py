"""Installed storage preview, conflict protection, archival and restoration."""
import json
import hashlib
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
execute("sh",os.environ["DEVTOOLS_TEST_INSTALLER"],"install","--version","0.0.0-test.1","--source",os.environ["DEVTOOLS_TEST_RELEASES"])
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
applied=api("cleanup","apply",plan["id"],"--item",item["id"],"--request-id",rid)
assert applied["changed"] and not applied["replayed"] and len(applied["items"])==1
assert not query.exists()
assert api("cleanup","apply",plan["id"],"--item",item["id"],"--request-id",rid)["replayed"]
restored=api("cleanup","restore",item["id"])
assert restored["changed"] and restored["item"]["id"]==item["id"]
assert not api("cleanup","restore",item["id"])["changed"]
assert query.exists()
assert api("cleanup","purge",item["id"],expected=3)["code"]=="retention_active"
created=api("task","add","--title","Completed fixture","--request-id",str(uuid.uuid4()))
tid=created["item"]["id"]
api("task","cancel",tid,"--reason","Fixture complete","--if-revision",str(created["revision"]),"--request-id",str(uuid.uuid4()))
journal=data/"tasks"/("p1-"+hashlib.sha256(b"cleanup-fixture").hexdigest()+".json")
body=json.loads(journal.read_text())
for event in body["events"]:event["occurred_at"]="2000-01-01T00:00:00Z"
journal.write_text(json.dumps(body))
plan=api("cleanup","preview","--profile","cleanup-fixture")
item=next(i for i in plan["items"] if i["kind"]=="completed_tasks")
api("cleanup","apply",plan["id"],"--item",item["id"],"--request-id",str(uuid.uuid4()))
assert api("task","list")["items"]==[]
api("cleanup","restore",item["id"])
assert api("task","show",tid)["item"]["state"]=="canceled"

process_id=str(uuid.uuid4());process_dir=data/"processes"/process_id;process_dir.mkdir(mode=0o700,parents=True)
old="2000-01-01T00:00:00Z"
record={"ready_configured":False,"id":process_id,"profile":"cleanup-fixture","instance_id":"","directory":str(root),"command":"legacy","env":"","capture_logs":True,"created_at":old,"started_at":old,"ended_at":old,"exit_code":0,"reason":"exit","state":"stopped"}
record_path=process_dir/"record.json";record_path.write_text(json.dumps(record));record_path.chmod(0o600)
log_path=process_dir/"output.log";log_path.write_text("partial-resume-log");log_path.chmod(0o600)
plan=api("cleanup","preview","--profile","cleanup-fixture")
process_item=next(i for i in plan["items"] if i["kind"]=="completed_process" and i["source"]==str(record_path))
log_item=next(i for i in plan["items"] if i["kind"]=="expired_log" and i["source"]==str(log_path))
blocked=data/"archives"/log_item["id"];blocked.mkdir(mode=0o700,parents=True);(blocked/"payload").mkdir(mode=0o700)
request_id=str(uuid.uuid4())
failure=api("cleanup","apply",plan["id"],"--item",process_item["id"],"--item",log_item["id"],"--request-id",request_id,expected=1)
assert failure["code"]=="storage_error"
assert not record_path.exists() and log_path.exists() and (process_dir/"record.archive.json").exists()
(cache/"cleanup"/(plan["id"]+".json")).unlink()
shutil.rmtree(blocked/"payload")
resumed=api("cleanup","apply",plan["id"],"--item",process_item["id"],"--item",log_item["id"],"--request-id",request_id)
assert resumed["replayed"] and len(resumed["items"])==2 and not log_path.exists()
assert all(i["id"]!=process_id for i in api("process","list","--profile","cleanup-fixture")["items"])

ghost=root/"gone";ghost.mkdir()
(ghost/"devtools.toml").write_text('profile="gone"\n[ports.web]\nport=25100\nrange=[25100,25199]\n')
allocated=api("port","allocate","web","--dir",str(ghost))["item"]
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
    path=api("backup","create")["item"]["path"];age=time.time()-(40-n)*86400;os.utime(path,(age,age))
plan=api("cleanup","preview")
assert len([i for i in plan["items"] if i["kind"]=="old_backup"])==1
print("Installed cleanup stale previews, task archive/restore, occupied port protection, missing instances and backup retention verified")
