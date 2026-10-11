"""Negotiate HTTP/2 with the packaged listener and revoke multiplexed LAN streams."""

import json
from pathlib import Path
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from network_fixture import NetworkFixture


fixture = NetworkFixture()
repository = Path(__file__).resolve().parents[2]
peer = fixture.home / "http2-peer"
subprocess.run(["go", "build", "-o", str(peer), "./verify/fixtures/http2_peer.go"],
               cwd=repository, env=fixture.env, check=True, timeout=60)
with fixture.network(wildcard=True):
    item = fixture.mutate("expose", "start", "app", "--global", "--provider", "lan", "--no-auth")["item"]
    public_ca = fixture.home / "public-ca.pem"
    fixture.api("proxy", "ca", "export", "--output", str(public_ca))
    process = subprocess.Popen([str(peer), str(public_ca), str(fixture.tls_port)],
                               env=fixture.env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, text=True)
    try:
        ready = process.stdout.readline()
        if not ready:
            _, diagnostic = process.communicate(timeout=5)
            raise AssertionError(diagnostic)
        assert ready and json.loads(ready) == {"ready": True, "http2": True, "multiplexed": True}, ready
        fixture.mutate("expose", "stop", item["id"])
        output, diagnostic = process.communicate("check\n", timeout=15)
        assert process.returncode == 0, diagnostic
        assert json.loads(output) == {"revoked": True, "sibling_preserved": True, "authority_checked": True}
        print("Actual packaged TLS HTTP/2: multiplexed selected streams revoked, SNI authority enforced and local sibling preserved; same-connection cross-session isolation also covered by Go integration tests")
    finally:
        if process.poll() is None:
            process.kill()
            process.communicate(timeout=5)
