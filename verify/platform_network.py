"""Opt-in real hosts, OS trust and low-port smoke on a disposable macOS/Linux host."""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import pty
import select
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import uuid

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary",type=Path,required=True)
    parser.add_argument("--evidence",type=Path,required=True)
    args=parser.parse_args()
    if os.environ.get("DEVTOOLS_DISPOSABLE_HOST")!="1" or os.geteuid()==0:
        parser.error("Requires DEVTOOLS_DISPOSABLE_HOST=1 and a non-root user on a disposable host with interactive sudo available.")
    source=args.binary.resolve()
    report={"platform":sys.platform,"binary_sha256":hashlib.sha256(source.read_bytes()).hexdigest(),"result":"fail","commands":[]}
    temporary=tempfile.mkdtemp(prefix="devtools-system-network-")
    # Retain keys and recovery state if any OS cleanup cannot be confirmed.
    if temporary:
        root=Path(temporary);root.chmod(0o700)
        report["recovery_root"]=str(root)
        binary=root/"bin"/"devtools";binary.parent.mkdir();shutil.copy2(source,binary)
        project=root/"project";project.mkdir()
        env=dict(os.environ,HOME=str(root),XDG_CONFIG_HOME=str(root/"config"),XDG_DATA_HOME=str(root/"data"),XDG_CACHE_HOME=str(root/"cache"))
        env.pop("DEVTOOLS_ISOLATED_ROOT",None);env.pop("DEVTOOLS_TASK_CONTEXT",None)
        hostname="network-"+uuid.uuid4().hex[:12]+".test"
        with socket.socket() as s:s.bind(("127.0.0.1",0));backend=s.getsockname()[1]
        (project/"server.py").write_text("import http.server,os\nhttp.server.HTTPServer(('127.0.0.1',int(os.environ['PORT'])),http.server.SimpleHTTPRequestHandler).serve_forever()\n")
        (project/"devtools.toml").write_text(f'''profile='platform-network'
[ports.web]
port={backend}
strict=true
[proxies.web]
host='{hostname}'
port='web'
[commands.web]
exec=['python3','server.py']
inject=false
serve=['web']
[commands.web.bind]
PORT={{port='web'}}
''')
        def api(*words,terminal=False,error=None):
            argv=[str(binary),*words]
            if terminal:
                pid,fd=pty.fork()
                if pid==0:
                    os.chdir(project);os.execve(str(binary),argv,env)
                content=bytearray();deadline=time.monotonic()+45
                try:
                    while True:
                        if time.monotonic()>deadline:
                            os.kill(pid,15);raise TimeoutError("Interactive network operation exceeded 45 seconds")
                        ready,_,_=select.select([fd],[],[],.2)
                        if ready:
                            try:part=os.read(fd,65536)
                            except OSError:break
                            if not part:break
                            content.extend(part)
                    _,status=os.waitpid(pid,0);code=os.waitstatus_to_exitcode(status)
                finally:os.close(fd)
                text=content.decode(errors="replace")
            else:
                p=subprocess.run(argv,cwd=project,env=env,capture_output=True,text=True,timeout=45)
                code=p.returncode;text=p.stderr if code else p.stdout
            report["commands"].append({"argv":list(words),"exit":code})
            # Interactive previews/sudo output precede the single JSON envelope.
            start=text.find('{"schema_version"')
            if start<0:raise RuntimeError("Network command did not return a JSON envelope")
            result=json.JSONDecoder().raw_decode(text[start:])[0]
            if error:
                assert code!=0 and result['error']['code']==error,result
                return result['error']
            assert code==0,result
            return result['data']
        def mutate(*words,**kwargs):return api(*words,"--request-id",str(uuid.uuid4()),**kwargs)
        try:
            mutate("proxy","configure","--http-address","127.0.0.1:80","--https-address","127.0.0.1:443")
            mutate("proxy","setup",terminal=True)
            # DNS preparation fails before a child starts when there is no terminal.
            mutate("project","up","web","--proxy",error="permission_required")
            assert not api("process","list","--profile","platform-network")["items"]
            mutate("project","up","web","--proxy",terminal=True)
            assert "127.0.0.1" in socket.gethostbyname_ex(hostname)[2]
            ca_path=root/"public.pem"
            api("proxy","ca","export","--output",str(ca_path))
            mutate("proxy","trust",terminal=True)
            assert api("proxy","ca","status")["trusted"]
            # A public-CA client verifies transport independently of Python's OS-store integration.
            c=http.client.HTTPSConnection(hostname,443,context=ssl.create_default_context(cafile=str(ca_path)),timeout=5)
            c.request("GET","/");r=c.getresponse();r.read();assert r.status==200;c.close()
            report.update(hosts_automatic=True,os_trust_confirmed=True,verified_https_443=True,noninteractive_no_child=True)
        finally:
            try:
                mutate("project","down")
                plan=api("proxy","teardown","--all","--remove-ca")["item"]
                mutate("proxy","teardown","--all","--remove-ca","--apply",plan["digest"],terminal=True)
                mutate("proxy","setup","--remove",terminal=True)
                assert hostname not in Path("/etc/hosts").read_text()
                assert not api("proxy","ca","status")["item"]["present"]
                report["cleanup_confirmed"]=True
            except Exception as e:
                report["cleanup_error"]=type(e).__name__
                raise
            finally:
                args.evidence.parent.mkdir(parents=True,exist_ok=True)
                args.evidence.write_text(json.dumps(report,indent=2)+"\n")
        report["result"]="pass"
        args.evidence.write_text(json.dumps(report,indent=2)+"\n")
        shutil.rmtree(root)
        print(json.dumps({k:v for k,v in report.items() if k!="commands"}))

if __name__=="__main__":main()
