"""Negotiate HTTP/2 with the packaged listener and revoke multiplexed LAN streams."""

import json
from pathlib import Path
import subprocess
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from network_fixture import NetworkFixture, free_port


fixture = NetworkFixture()
repository = Path(__file__).resolve().parents[2]
peer = fixture.home / "http2-peer"
subprocess.run(["go", "build", "-o", str(peer), "./verify/fixtures/http2_peer.go"],
               cwd=repository, env=fixture.env, check=True, timeout=60)
with fixture.network(wildcard=True):
    public_ca = fixture.home / "public-ca.pem"
    fixture.api("proxy", "ca", "export", "--output", str(public_ca))
    project = fixture.home / "project"
    project.mkdir()
    fixture.projects.append(project)
    (project / "server.py").write_text(f'''import os,sys
sys.path.insert(0,{str(repository / 'verify')!r})
from network_fixture import Backend,LocalServer
LocalServer(('127.0.0.1',int(os.environ['PORT'])),Backend).serve_forever()
''')
    (project / "devtools.toml").write_text(f'''profile='http2-fixture'
env_files=['.env']
[ports.app]
port={free_port()}
strict=true
[proxies.shared]
host='${{DOMAIN:-shared.localhost}}'
port='app'
[commands.app]
exec=['python3','-B','server.py']
inject=false
serve=['app']
[commands.app.bind]
PORT={{port='app'}}
[commands.app.ready]
exec=['python3','-c','import os,urllib.request;urllib.request.urlopen("http://127.0.0.1:"+os.environ["PORT"],timeout=1).read()']
timeout='5s'
''')
    (project / ".env").write_text("DOMAIN=shared.localhost\n")
    fixture.mutate("project", "up", "app", "--proxy", "--dir", str(project))
    for source in (None, project / ".env", project / "devtools.toml"):
        if source:
            fixture.mutate("proxy", "up", "shared", "--dir", str(project))
            route = ["shared", "--dir", str(project)]
        else:
            route = ["app", "--global"]
        item = fixture.mutate("expose", "start", *route, "--provider", "lan", "--alias", "app.local", "--no-auth")["item"]
        process = subprocess.Popen([str(peer), str(public_ca), str(fixture.tls_port)],
                                   env=fixture.env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, text=True)
        try:
            ready = process.stdout.readline()
            if not ready:
                _, diagnostic = process.communicate(timeout=5)
                raise AssertionError(diagnostic)
            assert json.loads(ready) == {"ready": True, "http2": True, "multiplexed": True}, ready
            if source:
                source.write_text(source.read_text() + "\n# changed while streams are open\n")
                deadline = time.monotonic() + 8
                while fixture.api("expose", "status", item["id"])["item"]["state"] == "active":
                    assert time.monotonic() < deadline, "source watcher did not revoke HTTP/2 exposure"
                    time.sleep(.05)
            else:
                fixture.mutate("expose", "stop", item["id"])
            output, diagnostic = process.communicate("check\n", timeout=15)
            assert process.returncode == 0, diagnostic
            assert json.loads(output) == {"revoked": True, "sibling_preserved": True, "authority_checked": True}
            fixture.mutate("expose", "stop", item["id"])
        finally:
            if process.poll() is None:
                process.kill()
                process.communicate(timeout=5)
    print("Actual packaged TLS HTTP/2: stop/TOML/env-file revocation of multiplexed streams, SNI authority and fresh sibling heartbeats passed; same-connection cross-session isolation also covered by Go integration tests")
