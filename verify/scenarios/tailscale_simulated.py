"""Exercise the packaged Tailscale adapter against a local conditional-write API."""

import base64
import copy
import hashlib
import http.server
import json
from pathlib import Path
import socket
import sys
import threading

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from network_fixture import NetworkFixture, serving


TOKEN = "LOCAL_API_CREDENTIAL_CANARY"
DOMAIN = "fixture.tailnet.ts.net"
config = {
    "TCP": {"443": {"HTTPS": True}},
    "Web": {f"{DOMAIN}:443": {"Handlers": {"/": {"Text": "user-owned"}}}},
    "FutureField": {"preserve": [1, 2, 3]},
}
original = copy.deepcopy(config)
lock = threading.Lock()
fault = None
requests = []


def encoded():
    return json.dumps(config, sort_keys=True).encode()


def etag():
    return '"' + hashlib.sha256(encoded()).hexdigest() + '"'


class LocalAPI(http.server.BaseHTTPRequestHandler):
    def reply(self, code, body=b""):
        self.send_response(code)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("ETag", etag())
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        with lock:
            assert self.path == "/localapi/v0/serve-config", self.path
            expected = "Basic " + base64.b64encode((":" + TOKEN).encode()).decode()
            assert self.headers.get("Authorization") == expected
            if fault == "permission":
                self.reply(403, TOKEN.encode())
            else:
                self.reply(200, encoded())

    def do_POST(self):
        global fault, config
        with lock:
            assert self.path == "/localapi/v0/serve-config"
            expected = "Basic " + base64.b64encode((":" + TOKEN).encode()).decode()
            assert self.headers.get("Authorization") == expected
            body = self.rfile.read(int(self.headers["Content-Length"]))
            assert self.headers.get("Content-Type") == "application/json"
            requests.append(self.headers.get("If-Match"))
            assert requests[-1], "adapter omitted conditional-write precondition"
            if fault == "conflict":
                # An independent user update races the adapter's earlier GET.
                config["FutureField"]["concurrent"] = "user-update"
                fault = None
                self.reply(412)
                return
            if self.headers["If-Match"] != etag():
                self.reply(412)
                return
            config = json.loads(body)
            if fault == "response-loss":
                # Commit succeeded, but the client cannot observe the response.
                fault = None
                self.connection.shutdown(socket.SHUT_RDWR)
                self.close_connection = True
                return
            self.reply(200)

    def log_message(self, *args):
        pass


fixture = NetworkFixture()
with serving(LocalAPI) as local_api:
    fakebin = fixture.home / "provider-bin"
    fakebin.mkdir()
    provider = fakebin / "tailscale"
    provider.write_text(f'''#!/usr/bin/env python3
import json,sys
args=sys.argv[1:]
if args==['status','--json']:
 print(json.dumps({{"Version":"1.86.0","BackendState":"Running","Self":{{"DNSName":"{DOMAIN}."}},"CertDomains":["{DOMAIN}"]}}))
elif args==['debug','local-creds']:
 print('curl -u:{TOKEN} http://localhost:{local_api.server_port}/localapi/v0/status')
else:
 raise SystemExit('unexpected provider command: '+repr(args))
''')
    provider.chmod(0o700)
    fixture.env["PATH"] = str(fakebin) + ":" + fixture.env["PATH"]

    def start(port, **kwargs):
        options = ["--auth-stdin"] if kwargs.get("auth") else ["--no-auth"]
        return fixture.mutate("expose", "start", "app", "--global", "--provider", "tailscale",
                              "--serve-port", str(port), *options, **kwargs)

    with fixture.network():
        first = start(8443, auth="reader:TAILSCALE_AUTH_CANARY\n")["item"]
        sibling = start(9443)["item"]
        assert first["state"] == sibling["state"] == "active"
        assert fixture.request(first)[0] == 401
        code, body = fixture.request(first, "reader:TAILSCALE_AUTH_CANARY")
        headers = json.loads(body)
        assert code == 200 and headers["X-Forwarded-Proto"] == "https"
        assert headers["X-Forwarded-Host"] == f"{DOMAIN}:8443" and "Authorization" not in headers
        with lock:
            config["Web"][f"{DOMAIN}:8443"]["Handlers"]["/user"] = {"Text": "retain-path"}
            fault = "conflict"
        fixture.mutate("expose", "stop", first["id"], error="tailscale_configuration_changed")
        assert fixture.request(sibling)[0] == 200, "failed selected stop affected sibling"
        fixture.mutate("expose", "stop", first["id"])
        with lock:
            assert config["Web"][f"{DOMAIN}:8443"]["Handlers"] == {"/user": {"Text": "retain-path"}}
            assert config["FutureField"]["concurrent"] == "user-update"
            assert config["Web"][f"{DOMAIN}:443"] == original["Web"][f"{DOMAIN}:443"]

        with lock:
            fault = "response-loss"
        start(11443, error="tailscale_command_failed")
        lost = [item for item in fixture.api("expose", "list")["items"] if item["serve_port"] == 11443][0]
        assert lost["state"] == "recovery_required"
        with lock:
            assert "/" in config["Web"][f"{DOMAIN}:11443"]["Handlers"], "fault did not commit provider state"
        fixture.mutate("expose", "stop", lost["id"])
        with lock:
            assert f"{DOMAIN}:11443" not in config["Web"], "uncertain start left an owned provider handler"

        # SIGKILL removes in-memory auth; recovery uses durable exact ownership.
        crashed = start(12443, auth="reader:TAILSCALE_AUTH_CANARY\n")["item"]
        fixture.kill_daemon()
        assert fixture.api("expose", "status", crashed["id"])["item"]["state"] == "auth_required"
        fixture.mutate("proxy", "start")
        fixture.mutate("expose", "stop", crashed["id"])
        fixture.mutate("expose", "stop", sibling["id"])
        # No active session probes race this deliberately unavailable API.
        with lock:
            fault = "permission"
        error = start(10443, error="tailscale_permission_required")
        assert TOKEN not in json.dumps(error), "LocalAPI diagnostic leaked credentials"
        with lock:
            fault = None
        failed = [item for item in fixture.api("expose", "list")["items"] if item["serve_port"] == 10443]
        assert len(failed) == 1 and failed[0]["state"] == "recovery_required"
        fixture.mutate("expose", "stop", failed[0]["id"])
        with lock:
            assert f"{DOMAIN}:12443" not in config["Web"] and f"{DOMAIN}:9443" not in config["Web"]
            assert config["TCP"]["443"] == original["TCP"]["443"]
            assert config["FutureField"]["preserve"] == original["FutureField"]["preserve"]
        for path in fixture.state.rglob("*"):
            if path.is_file():
                data = path.read_bytes()
                assert TOKEN.encode() not in data and b"TAILSCALE_AUTH_CANARY" not in data, path
        assert requests and all(requests)
        print("SIMULATED Tailscale: packaged CLI/LocalAPI auth, conditional conflict, response-loss recovery, SIGKILL/auth loss and user configuration preservation passed; real tailnet/peer remains separate")
