"""Local peers for packaged network scenarios; no OS configuration is changed."""

import base64
import contextlib
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import shlex
import signal
import socket
import socketserver
import subprocess
import threading
import time
import uuid


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class LocalServer(http.server.ThreadingHTTPServer):
    daemon_threads = True

    def server_bind(self):
        # Avoid reverse DNS, which can depend on the runner's network.
        socketserver.TCPServer.server_bind(self)
        self.server_name, self.server_port = self.server_address[:2]


class Backend(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        if self.path == "/ws":
            key = self.headers["Sec-WebSocket-Key"]
            accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
            self.send_response(101)
            self.send_header("Upgrade", "websocket")
            self.send_header("Connection", "Upgrade")
            self.send_header("Sec-WebSocket-Accept", accept)
            self.end_headers()
            try:
                while True:
                    header = self.rfile.read(2)
                    if not header:
                        return
                    assert len(header) == 2 and header[0] == 0x81 and header[1] & 0x80
                    length = header[1] & 0x7f
                    assert length < 126
                    mask, payload = self.rfile.read(4), self.rfile.read(length)
                    assert len(mask) == 4 and len(payload) == length
                    body = bytes(value ^ mask[index % 4] for index, value in enumerate(payload))
                    self.wfile.write(bytes([0x81, len(body)]) + body)
                    self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                pass
            return
        if self.path == "/stream":
            self.send_response(200)
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            try:
                while True:
                    body = f"alive {time.time_ns()}\n".encode()
                    self.wfile.write(f"{len(body):x}\r\n".encode() + body + b"\r\n")
                    self.wfile.flush()
                    time.sleep(.05)
            except (BrokenPipeError, ConnectionResetError):
                pass
            return
        body = json.dumps(dict(self.headers)).encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


@contextlib.contextmanager
def serving(handler):
    server = LocalServer(("127.0.0.1", 0), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


class NetworkFixture:
    def __init__(self):
        self.home = Path.home()
        self.binary = self.home / ".local/bin/devtools"
        self.state = self.home / "isolated"
        self.env = dict(os.environ)
        self.env.pop("DEVTOOLS_TASK_CONTEXT", None)
        self.env.pop("DEVTOOLS_ISOLATED_ROOT", None)
        subprocess.run([
            "sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install",
            "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"],
        ], check=True, capture_output=True, timeout=30)

    def api(self, *args, auth=None, error=None):
        result = subprocess.run([
            str(self.binary), "--isolated-root", str(self.state), *args,
        ], cwd=self.home, env=self.env, input=auth or "", text=True,
            capture_output=True, timeout=40)
        if error:
            assert result.returncode != 0, args
            value = json.loads(result.stderr)["error"]
            assert value["code"] == error, (args, value)
            return value
        assert result.returncode == 0, (args, result.stderr)
        return json.loads(result.stdout)["data"]

    def mutate(self, *args, **kwargs):
        return self.api(*args, "--request-id", str(uuid.uuid4()), **kwargs)

    @contextlib.contextmanager
    def network(self, wildcard=False):
        with serving(Backend) as backend:
            self.http_port, self.tls_port = free_port(), free_port()
            address = "0.0.0.0" if wildcard else "127.0.0.1"
            try:
                self.mutate("proxy", "configure", "--http-address", f"{address}:{self.http_port}",
                            "--https-address", f"{address}:{self.tls_port}")
                self.mutate("proxy", "add", "app", str(backend.server_port), "--global", "--host", "app.localhost")
                self.mutate("proxy", "up", "app", "--global")
                yield self
            finally:
                self.mutate("proxy", "start")
                for item in self.api("expose", "list")["items"]:
                    if item["state"] != "stopped":
                        self.mutate("expose", "stop", item["id"])
                plan = self.api("proxy", "teardown", "--all")["item"]
                self.mutate("proxy", "teardown", "--all", "--apply", plan["digest"])

    def kill_daemon(self):
        assert self.api("proxy", "status")["item"]["running"]
        record = json.loads((self.state / "data/proxy/record.json").read_text())
        expected = [str(self.binary), "__proxy-serve", str(self.state / "data"), record["attempt_id"]]
        processes = subprocess.run(["ps", "-axww", "-o", "pid=,command="], check=True,
                                   text=True, capture_output=True, timeout=5)
        owned = []
        for line in processes.stdout.splitlines():
            parts = line.strip().split(None, 1)
            if len(parts) == 2 and str(self.binary) in parts[1]:
                if shlex.split(parts[1]) == expected:
                    owned.append(int(parts[0]))
        # Match both the private root and current generation before signalling.
        assert len(owned) == 1 and owned[0] > 1, owned
        os.kill(owned[0], signal.SIGKILL)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if not self.api("proxy", "status")["item"]["running"]:
                return
            time.sleep(.05)
        raise AssertionError("killed daemon still reported running")

    def request(self, item, auth=None):
        from urllib.parse import urlparse
        connection = http.client.HTTPConnection("127.0.0.1", item["origin_port"], timeout=3)
        headers = {"Host": urlparse(item["url"]).netloc}
        if auth:
            headers["Authorization"] = "Basic " + base64.b64encode(auth.encode()).decode()
        try:
            connection.request("GET", "/", headers=headers)
            response = connection.getresponse()
            return response.status, response.read()
        finally:
            connection.close()
