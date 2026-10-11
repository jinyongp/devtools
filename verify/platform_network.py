"""Opt-in real hosts, OS trust and low-port smoke on a disposable macOS/Linux host."""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import pty
import select
import signal
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
        original_hosts=Path("/etc/hosts").read_bytes()
        hostname="network-"+uuid.uuid4().hex[:12]+".test"
        with socket.socket() as s:s.bind(("127.0.0.1",0));backend=s.getsockname()[1]
        (project/"server.py").write_text('''import http.server,os,socketserver
class Backend(http.server.HTTPServer):
 def server_bind(self):
  socketserver.TCPServer.server_bind(self)
  self.server_name,self.server_port=self.server_address[:2]
Backend(('127.0.0.1',int(os.environ['PORT'])),http.server.SimpleHTTPRequestHandler).serve_forever()
''')
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
[commands.web.ready]
exec=['python3','-c',"import os,socket; socket.create_connection(('127.0.0.1',int(os.environ['PORT'])),timeout=1).close()"]
timeout='10s'
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
                            # Retain only this fixture command's process tree and
                            # partial output, including a Go stack for a blocked CLI.
                            processes=subprocess.run(["ps","-axo","pid=,ppid=,comm="],capture_output=True,text=True,timeout=5)
                            rows=[line.split(None,2) for line in processes.stdout.splitlines()]
                            owned={pid}
                            while True:
                                children={int(row[0]) for row in rows if len(row)==3 and int(row[1]) in owned}
                                expanded=owned|children
                                if expanded==owned:break
                                owned=expanded
                            details=subprocess.run(["ps","-p",','.join(map(str,sorted(owned))),"-o","pid=,ppid=,command="],capture_output=True,text=True,timeout=5)
                            report["terminal_timeout_processes"]=details.stdout
                            try:os.kill(pid,signal.SIGQUIT)
                            except ProcessLookupError:pass
                            drain=time.monotonic()+2
                            while time.monotonic()<drain:
                                ready,_,_=select.select([fd],[],[],.1)
                                if ready:
                                    try:part=os.read(fd,65536)
                                    except OSError:break
                                    if not part:break
                                    content.extend(part)
                            report["terminal_timeout_output"]=content.decode(errors="replace")
                            report["commands"].append({"argv":list(words),"exit":None,"timeout":True})
                            try:os.kill(pid,signal.SIGKILL)
                            except ProcessLookupError:pass
                            os.waitpid(pid,0)
                            raise TimeoutError("Interactive network operation exceeded 45 seconds; fixture process tree and partial output retained in evidence")
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
            mutate("proxy","setup",terminal=True)
            setup=api("proxy","setup","--check")["item"]
            if sys.platform=="linux":assert setup["enabled"],setup
            mutate("proxy","configure","--http-address","127.0.0.1:80","--https-address","127.0.0.1:443")
            if sys.platform=="darwin":
                mutate("proxy","start",error="permission_required")
                assert not api("proxy","status")["item"]["running"]
                mutate("proxy","configure","--http-address","0.0.0.0:80","--https-address","0.0.0.0:443")
                report["specific_low_port_permission_checked"]=True
            # DNS preparation fails before a child starts when there is no terminal.
            mutate("project","up","web","--proxy",error="permission_required")
            assert not api("process","list","--profile","platform-network")["items"]
            mutate("project","up","web","--proxy",terminal=True)
            assert "127.0.0.1" in socket.gethostbyname_ex(hostname)[2]
            c=http.client.HTTPConnection(hostname,80,timeout=5)
            c.request("GET","/path?q=1");r=c.getresponse();r.read()
            assert r.status in (301,302,307,308) and r.getheader("Location")==f"https://{hostname}/path?q=1"
            c.close()
            ca_path=root/"public.pem"
            api("proxy","ca","export","--output",str(ca_path))
            # A public-CA client verifies transport independently of Python's OS-store integration.
            c=http.client.HTTPSConnection(hostname,443,context=ssl.create_default_context(cafile=str(ca_path)),timeout=5)
            c.request("GET","/");r=c.getresponse();r.read();assert r.status==200,r.status;c.close()
            if sys.platform=="linux":
                c=http.client.HTTPSConnection(hostname,443,context=ssl.create_default_context(),timeout=5)
                try:
                    c.request("GET","/")
                    raise AssertionError("System roots trusted the fresh CA before explicit trust")
                except ssl.SSLCertVerificationError:pass
                finally:c.close()
            mutate("proxy","trust",terminal=True)
            assert api("proxy","ca","status")["trusted"]
            result=subprocess.run(["curl","--noproxy","*","--silent","--show-error","--fail","--max-time","5",f"https://{hostname}/"],env=env,capture_output=True,text=True,timeout=10)
            assert result.returncode==0,result.stderr
            if sys.platform=="linux":
                replacement=binary.with_name("replacement")
                shutil.copy2(source,replacement);os.replace(replacement,binary)
                setup=api("proxy","setup","--check")["item"]
                assert setup["recovery_required"] and setup["executable_changed"] and not setup["enabled"],setup
                mutate("proxy","setup",terminal=True)
                setup=api("proxy","setup","--check")["item"]
                assert setup["enabled"] and not setup["recovery_required"],setup
                report["external_replacement_capability_recovery"]=True
            report.update(hosts_automatic=True,os_trust_confirmed=True,verified_https_443=True,http_80_redirect=True,system_root_https=True,noninteractive_no_child=True)
        finally:
            try:
                mutate("project","down")
                plan=api("proxy","teardown","--all","--remove-ca")["item"]
                mutate("proxy","teardown","--all","--remove-ca","--apply",plan["digest"],terminal=True)
                mutate("proxy","setup","--remove",terminal=True)
                if sys.platform=="linux":assert not api("proxy","setup","--check")["item"]["enabled"]
                assert Path("/etc/hosts").read_bytes()==original_hosts
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
