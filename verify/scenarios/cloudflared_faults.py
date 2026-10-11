"""Packaged provider failure/recovery and WebSocket frames without public DNS."""

import json
import os
from pathlib import Path
import signal
import socket
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from network_fixture import NetworkFixture, WebSocket


fixture = NetworkFixture()
fakebin = fixture.home / "provider-bin"
fakebin.mkdir()
mode = fixture.home / "provider-mode"
mode.write_text("active")
provider = fakebin / "cloudflared"
provider.write_text(f'''#!/usr/bin/env python3
import os,sys,time
from pathlib import Path
from urllib.parse import urlparse
assert sys.argv[1:4]==['tunnel','--no-autoupdate','--url']
if Path({str(mode)!r}).read_text()=='fail':raise SystemExit(7)
port=urlparse(sys.argv[4]).port
Path({str(fakebin)!r},str(port)+'.pid').write_text(str(os.getpid()))
print('https://fault-fixture.trycloudflare.com',file=sys.stderr,flush=True)
while True:time.sleep(1)
''')
provider.chmod(0o700)
fixture.env["PATH"] = str(fakebin) + ":" + fixture.env["PATH"]


def start(**kwargs):
    options = ["--auth-stdin"] if kwargs.get("auth") else ["--no-auth"]
    return fixture.mutate("expose", "start", "app", "--global", "--provider", "cloudflared", *options, **kwargs)


def wait_until(check, message):
    deadline = time.monotonic() + 6
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(.05)
    raise AssertionError(message)


def gone(pid):
    try:
        os.kill(pid, 0)
        return False
    except ProcessLookupError:
        return True


with fixture.network():
    mode.write_text("fail")
    start(error="cloudflared_exited_without_url")
    failed = fixture.api("expose", "list")["items"][0]
    assert failed["state"] == "recovery_required"
    with socket.socket() as probe:
        probe.settimeout(.5)
        assert probe.connect_ex(("127.0.0.1", failed["origin_port"])) != 0, "failed start left origin open"
    fixture.mutate("expose", "stop", failed["id"])
    mode.write_text("active")
    first = start(auth="reader:CLOUD_FAULT_AUTH_CANARY\n")["item"]
    sibling = start()["item"]
    first_ws, sibling_ws = None, None
    try:
        first_ws = WebSocket(first, "reader:CLOUD_FAULT_AUTH_CANARY")
        sibling_ws = WebSocket(sibling)
        owned_pid = int((fakebin / f'{first["origin_port"]}.pid').read_text())
        os.kill(owned_pid, signal.SIGKILL)
        wait_until(lambda: fixture.api("expose", "status", first["id"])["item"]["state"] == "recovery_required",
                   "provider crash did not revoke exposure")
        first_ws.assert_closed()
        sibling_ws.echo()
        assert fixture.request(sibling)[0] == 200
        fixture.mutate("expose", "stop", first["id"])

        crashed = start(auth="reader:CLOUD_FAULT_AUTH_CANARY\n")["item"]
        daemon_ws = WebSocket(crashed, "reader:CLOUD_FAULT_AUTH_CANARY")
        try:
            fixture.kill_daemon()
            daemon_ws.assert_closed()
            sibling_ws.assert_closed()
            for item in (sibling, crashed):
                pid = int((fakebin / f'{item["origin_port"]}.pid').read_text())
                wait_until(lambda: gone(pid), "daemon crash left provider process alive")
            assert fixture.api("expose", "status", crashed["id"])["item"]["state"] == "auth_required"
            fixture.mutate("proxy", "start")
            for item in (sibling, crashed):
                fixture.mutate("expose", "stop", item["id"])
                assert fixture.api("expose", "status", item["id"])["item"]["state"] == "stopped"
        finally:
            daemon_ws.close()
    finally:
        if first_ws:
            first_ws.close()
        if sibling_ws:
            sibling_ws.close()
    markers = list((fixture.state / "data/exposure-providers").glob("*.json"))
    assert markers and all(json.loads(path.read_text())["state"] == "stopped" for path in markers)
    assert all(b"CLOUD_FAULT_AUTH_CANARY" not in path.read_bytes() for path in fixture.state.rglob("*") if path.is_file())
    fixture.mutate("proxy", "down", "app", "--global")  # Failed/crashed sessions released every pin.
    print("SIMULATED Cloudflared: packaged start-failure origin rollback, real WebSocket framing/revocation/sibling preservation, provider SIGKILL and daemon SIGKILL descendant cleanup/auth loss passed; public ingress remains separate")
