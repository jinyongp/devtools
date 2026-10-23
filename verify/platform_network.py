"""Opt-in real hosts, OS trust and low-port smoke on a disposable macOS/Linux host."""
import argparse
import hashlib
import http.client
import json
import os
import plistlib
from pathlib import Path
import shutil
import socket
import ssl
import subprocess
import sys
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from fixture_paths import create_fixture_directory

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary",type=Path,required=True)
    parser.add_argument("--evidence",type=Path,required=True)
    args=parser.parse_args()
    if os.environ.get("DEVTOOLS_DISPOSABLE_HOST")!="1" or os.geteuid()==0:
        parser.error("Requires DEVTOOLS_DISPOSABLE_HOST=1 and a non-root user on a disposable host with passwordless sudo available.")
    source=args.binary.resolve()
    report={"platform":sys.platform,"binary_sha256":hashlib.sha256(source.read_bytes()).hexdigest(),"result":"fail","commands":[]}
    temporary=create_fixture_directory(prefix="devtools-system-network-")
    # Retain keys and recovery state if any OS cleanup cannot be confirmed.
    if temporary:
        # macOS's /var temporary path is an OS symlink. Select the real path
        # before passing HOME to the no-follow private storage boundary.
        root=temporary;root.chmod(0o700)
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
        def api(*words,error=None,env_override=None):
            argv=[str(binary),*words]
            execution_env=env if env_override is None else env_override
            p=subprocess.Popen(argv,cwd=project,env=execution_env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
            try:
                try:
                    stdout,stderr=p.communicate(timeout=30 if sys.platform=="darwin" else 45)
                except subprocess.TimeoutExpired:
                    if sys.platform=="darwin":
                        processes=subprocess.run(["ps","-axo","pid=,ppid=,comm="],capture_output=True,text=True,timeout=5)
                        rows=[line.split(None,2) for line in processes.stdout.splitlines()]
                        owned={p.pid}
                        while True:
                            expanded=owned|{int(row[0]) for row in rows if len(row)==3 and int(row[1]) in owned}
                            if expanded==owned:break
                            owned=expanded
                        for row in rows:
                            if len(row)==3 and int(row[0]) in owned and row[2]=="/usr/bin/security":
                                sample=subprocess.run(["sudo","-n","/usr/bin/sample",row[0],"1","1"],capture_output=True,text=True,timeout=10)
                                report["security_sample"]=sample.stdout+sample.stderr
                        stdout,stderr=p.communicate(timeout=120)
                    else:
                        raise
            finally:
                if p.poll() is None:
                    p.kill();p.communicate()
            code=p.returncode;text=stderr if code else stdout
            report["commands"].append({"argv":list(words),"exit":code})
            # Preparation previews can precede the single JSON envelope.
            start=text.find('{"schema_version"')
            if start<0:raise RuntimeError("Network command did not return a JSON envelope")
            result=json.JSONDecoder().raw_decode(text[start:])[0]
            if code:report["commands"][-1]["error_code"]=result["error"]["code"]
            if error:
                assert code!=0 and result['error']['code']==error,result
                return result['error']
            assert code==0,result
            return result['data']
        def mutate(*words,**kwargs):return api(*words,"--request-id",str(uuid.uuid4()),**kwargs)
        sibling_env=None
        def trust_settings():
            path=root/"trust-settings.plist"
            subprocess.run(["/usr/bin/security","trust-settings-export","-d",str(path)],capture_output=True,text=True,timeout=10,check=True)
            return plistlib.loads(path.read_bytes())["trustList"]
        def check_public():
            p=subprocess.run([str(binary),"__trust-check"],input=ca_path.read_bytes(),capture_output=True,timeout=15,check=True)
            status=json.loads(p.stdout)
            assert status["known"] and not status["trusted"],status
        try:
            mutate("proxy","setup")
            setup=api("proxy","setup","--check")["item"]
            if sys.platform=="linux":assert setup["enabled"],setup
            mutate("proxy","configure","--http-address","127.0.0.1:80","--https-address","127.0.0.1:443")
            if sys.platform=="darwin":
                mutate("proxy","start",error="permission_required")
                assert not api("proxy","status")["item"]["running"]
                mutate("proxy","configure","--http-address","0.0.0.0:80","--https-address","0.0.0.0:443")
                report["specific_low_port_permission_checked"]=True
            # An unavailable authorization must fail before launching a backend.
            denied=root/"denied";denied.mkdir()
            sudo=denied/"sudo"
            sudo.write_text("#!/bin/sh\nprintf '%s' \"$1\" > \"$0.first-arg\"\nexit 1\n");sudo.chmod(0o700)
            previous_path=env["PATH"];env["PATH"]=str(denied)+os.pathsep+previous_path
            try:
                mutate("project","up","web",error="permission_required")
                assert sudo.with_name("sudo.first-arg").read_text()=="-n"
                assert not api("process","list","--profile","platform-network")["items"]
            finally:env["PATH"]=previous_path
            mutate("project","up","web")
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
            mutate("proxy","trust")
            assert api("proxy","ca","status")["trusted"]
            if sys.platform=="darwin":
                sibling_root=root/"sibling";sibling_root.mkdir()
                sibling_env=dict(env,HOME=str(sibling_root),XDG_CONFIG_HOME=str(sibling_root/"config"),XDG_DATA_HOME=str(sibling_root/"data"),XDG_CACHE_HOME=str(sibling_root/"cache"))
                mutate("proxy","ca","init",env_override=sibling_env)
                mutate("proxy","trust",env_override=sibling_env)
                before_trust=trust_settings()
                selected_hash=hashlib.sha1(ssl.PEM_cert_to_DER_cert(ca_path.read_text())).hexdigest().upper()
                mutate("proxy","untrust")
                check_public()
                assert api("proxy","ca","status",env_override=sibling_env)["trusted"]
                after_trust=trust_settings()
                assert {k:v for k,v in before_trust.items() if k!=selected_hash}=={k:v for k,v in after_trust.items() if k!=selected_hash}
                assert after_trust[selected_hash]["trustSettings"][0]["kSecTrustSettingsResult"]==3,after_trust[selected_hash]
                # Re-importing only the public certificate cannot restore trust.
                keychain="/Library/Keychains/System.keychain"
                subprocess.run(["sudo","-n","/usr/bin/security","add-certificates","-k",keychain,str(ca_path)],check=True,capture_output=True,timeout=10)
                check_public()
                mutate("proxy","untrust")
                mutate("proxy","trust")
                assert api("proxy","ca","status")["trusted"]
                report.update(macos_reimport_rejected=True,macos_sibling_trust_preserved=True,macos_explicit_retrust=True,macos_public_deny_retained=True)
            result=subprocess.run(["curl","--noproxy","*","--silent","--show-error","--fail","--max-time","5",f"https://{hostname}/"],env=env,capture_output=True,text=True,timeout=10)
            assert result.returncode==0,result.stderr
            if sys.platform=="linux":
                replacement=binary.with_name("replacement")
                shutil.copy2(source,replacement);os.replace(replacement,binary)
                setup=api("proxy","setup","--check")["item"]
                assert setup["recovery_required"] and setup["executable_changed"] and not setup["enabled"],setup
                mutate("proxy","setup")
                setup=api("proxy","setup","--check")["item"]
                assert setup["enabled"] and not setup["recovery_required"],setup
                report["external_replacement_capability_recovery"]=True
            report.update(hosts_automatic=True,os_trust_confirmed=True,verified_https_443=True,http_80_redirect=True,system_root_https=True,noninteractive=True,noninteractive_no_child=True)
        finally:
            try:
                mutate("project","down")
                plan=api("proxy","teardown","--all","--remove-ca")["item"]
                mutate("proxy","teardown","--all","--remove-ca","--apply",plan["digest"])
                mutate("proxy","setup","--remove")
                if sibling_env is not None:
                    assert api("proxy","ca","status",env_override=sibling_env)["trusted"]
                    sibling_plan=api("proxy","teardown","--all","--remove-ca",env_override=sibling_env)["item"]
                    mutate("proxy","teardown","--all","--remove-ca","--apply",sibling_plan["digest"],env_override=sibling_env)
                    assert not api("proxy","ca","status",env_override=sibling_env)["item"]["present"]
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
