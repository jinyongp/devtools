"""Installed native CLI journey, using only owned data and public loopback ports."""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import uuid
from urllib.parse import urlsplit


class CLIError(RuntimeError):
    def __init__(self, arguments, code, details):
        self.code, self.details = code, details or {}
        safe = {key: self.details.get(key) for key in ("operation", "os_error")}
        super().__init__(f"Installed command {arguments[:3]} failed: {code} {safe}")


def available_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--release-dir", required=True, type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    args = parser.parse_args()
    if os.name != "nt":
        raise RuntimeError("This journey runs only on native Windows Actions")
    repository = Path.cwd()
    root = Path(tempfile.mkdtemp(prefix="devtools-native-product-"))
    project = root / "프로젝트 공백"
    project.mkdir()
    binary = root / "installed" / "bin" / "devtools.exe"
    base_env = dict(os.environ)
    base_env.pop("DEVTOOLS_ISOLATED_ROOT", None)
    source_env = dict(base_env, DEVTOOLS_ISOLATED_ROOT=str(root / "source"))
    target_env = dict(base_env, DEVTOOLS_ISOLATED_ROOT=str(root / "target"))
    steps = []

    def raw(arguments, env=source_env, input="", timeout=40):
        result = subprocess.run([str(binary), *arguments], cwd=project, env=env, input=input.encode("utf-8"),
                                capture_output=True, timeout=timeout)
        if result.returncode:
            try:
                error = json.loads(result.stderr)["error"]
                raise CLIError(arguments,error["code"],error.get("details"))
            except (ValueError, KeyError, TypeError):
                reason = "non-JSON failure (raw output withheld)"
            raise RuntimeError(f"Installed command {arguments[:3]} exited {result.returncode}: {reason}")
        return result.stdout.decode("utf-8")

    def api(*arguments, env=source_env, input=""):
        result = json.loads(raw(list(arguments), env=env, input=input))
        assert result["schema_version"] == 1 and result["ok"]
        return result["data"]

    def mutate(*arguments, env=source_env, input="", request=None, settle=False):
        fixed = [*arguments, "--request-id", request or str(uuid.uuid4())]
        deadline = time.monotonic() + 20
        while True:
            try:
                return api(*fixed, env=env, input=input)
            except CLIError as error:
                codes = [item.get("condition", {}).get("code") for item in (error.details.get("items") or []) if item.get("condition")]
                pending = error.code in {"proxy_pending", "process_pending"} or (
                    error.code == "project_partial_failure" and codes and all(code in {"proxy_pending", "process_pending"} for code in codes))
                if not settle or not pending or time.monotonic() >= deadline:
                    raise
                # Resume only a typed uncertain operation, with unchanged input/UUID.
                time.sleep(0.05)

    running = False
    try:
        install = subprocess.run([
            shutil.which("powershell.exe"), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
            "-File", str(repository / "scripts" / "install.ps1"), "-Version", args.version,
            "-Source", str(args.release_dir.resolve()), "-BinDir", str(binary.parent), "-NoPath",
        ], cwd=repository, env=base_env, text=True, encoding="utf-8", capture_output=True, timeout=180)
        assert install.returncode == 0, "Candidate installer did not complete (raw output withheld)"
        identity = api("version")
        assert identity["version"] == args.version and identity["commit"] == args.commit and identity["protocol_version"] == 7
        alias = binary.with_name("dvt.exe")
        assert hashlib.sha256(alias.read_bytes()).digest() == hashlib.sha256(binary.read_bytes()).digest()
        steps.append("candidate_install_and_alias")

        node = shutil.which("node")
        assert node
        backend = project / "server.mjs"
        backend.write_text('''import http from 'node:http';
const server=http.createServer((req,res)=>{res.writeHead(200,{'content-type':'text/plain'});res.end('native-product-canary');});
server.listen(Number(process.env.PORT),'127.0.0.1');
for(const name of ['SIGINT','SIGBREAK','SIGTERM'])process.on(name,()=>server.close(()=>process.exit(0)));
''', encoding="utf-8")
        probe = "fetch('http://127.0.0.1:'+process.env.PORT+'/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
        config = f'''profile = "native-product"
[ports.web]
range = [40000, 45000]
[proxies.web]
host = "native.product.localhost"
port = "web"
[commands.web]
exec = {json.dumps([node, str(backend)], ensure_ascii=False)}
serve = ["web"]
[commands.web.bind]
PORT = {{ port = "web" }}
[commands.web.ready]
exec = {json.dumps([node, "-e", probe])}
timeout = "3s"
'''
        (project / "devtools.toml").write_text(config, encoding="utf-8")
        api("var", "set", "MESSAGE", "--value", "portable-public-canary")
        api("sec", "set", "TOKEN", "--stdin", input="synthetic-secret-canary\n")
        assert api("var", "get", "MESSAGE")["value"] == "portable-public-canary"
        task = mutate("task", "add", "--title", "Installed native task")["item"]["id"]
        assert api("task", "show", task)["item"]["id"] == task
        steps.append("values_secret_and_task_journal")

        http_port, tls_port = available_port(), available_port()
        mutate("proxy", "configure", "--http-address", f"127.0.0.1:{http_port}", "--https-address", f"127.0.0.1:{tls_port}")
        request = str(uuid.uuid4())
        up = mutate("project", "up", "web", "--proxy", "--timeout", "15s", request=request, settle=True)
        running = True
        assert up["items"][0]["status"] == "ready"
        execution = up["items"][0]["item"]["id"]
        replay = mutate("project", "up", "web", "--proxy", "--timeout", "15s", request=request, settle=True)
        assert replay["replayed"] and replay["items"][0]["item"]["id"] == execution
        port = api("port", "show", "web")["item"]["port"]
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
        connection.request("GET", "/health")
        response = connection.getresponse()
        assert response.status == 200 and response.read() == b"native-product-canary"
        connection.close()
        certificate = project / "public-ca.pem"
        exported = api("proxy", "ca", "export", "--output", str(certificate))["item"]
        der = ssl.PEM_cert_to_DER_cert(certificate.read_text())
        assert hashlib.sha256(der).hexdigest() == exported["fingerprint"]
        assert "PRIVATE KEY" not in certificate.read_text()
        context = ssl.create_default_context(cafile=str(certificate))
        with socket.create_connection(("127.0.0.1", tls_port), timeout=5) as sock:
            with context.wrap_socket(sock, server_hostname="native.product.localhost") as tls:
                tls.sendall(f"GET / HTTP/1.1\r\nHost: native.product.localhost:{tls_port}\r\nConnection: close\r\n\r\n".encode())
                body = bytearray()
                while chunk := tls.recv(65536):
                    body.extend(chunk)
                assert body.startswith(b"HTTP/1.1 200") and b"native-product-canary" in body
        restarted = mutate("project", "restart", "web", "--timeout", "15s", settle=True)
        assert restarted["items"][0]["item"]["id"] != execution
        mutate("project", "down", "web")
        running = False
        mutate("proxy", "stop")
        steps.append("managed_readiness_retry_restart_stop_and_verified_tls")

        passphrase = os.urandom(24).hex() + "\n"
        archive = project / "portable.age"
        api("profile", "export", "--passphrase-stdin", "--output", str(archive), input=passphrase)
        encrypted = archive.read_bytes()
        assert b"synthetic-secret-canary" not in encrypted and passphrase.strip().encode() not in encrypted
        base = ["profile", "import", "--file", str(archive), "--passphrase-stdin"]
        preview = api(*base, env=target_env, input=passphrase)
        request = str(uuid.uuid4())
        restored = mutate(*base, "--apply", preview["digest"], env=target_env, input=passphrase, request=request)
        assert restored["changed"]
        assert mutate(*base, "--apply", preview["digest"], env=target_env, input=passphrase, request=request)["replayed"]
        assert api("var", "get", "MESSAGE", env=target_env)["value"] == "portable-public-canary"
        assert api("task", "show", task, env=target_env)["item"]["id"] == task
        raw(["run", "--", node, "-e", "process.exit(process.env.TOKEN===process.env.DEVTOOLS_TEST_EXPECTED_TOKEN?0:1)"],
            env=dict(target_env, DEVTOOLS_TEST_EXPECTED_TOKEN="synthetic-secret-canary\n"))
        assert api("backup", "status", env=target_env)["item"]["configured"] is False
        assert api("diagnostics")["ready"]
        steps.append("encrypted_profile_transfer_preview_apply_retry")
        result = {"commit": args.commit, "version": args.version, "protocol": 7,
                  "exe_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                  "steps": {step: "pass" for step in steps}, "isolated": True,
                  "os_trust_modified": False, "real_pc": "NOT RUN"}
        (repository / "windows-installed-product-results.json").write_text(json.dumps(result, indent=2) + "\n")
    finally:
        if binary.exists():
            try:
                mutate("project", "down", "web")
            except Exception:
                pass
            try:
                mutate("proxy", "stop")
            except Exception:
                pass
        # Listener stop intentionally leaves the per-user controller alive. End
        # only this owned isolation's daemon via its authenticated private RPC;
        # never enumerate/kill PIDs or infer ownership from executable names.
        control_path = root / "source" / "data" / "proxy" / "control.json"
        if control_path.is_file():
            control = json.loads(control_path.read_text(encoding="utf-8"))
            address = urlsplit(control.get("address", ""))
            token = control.get("token", "")
            if address.scheme != "http" or address.hostname != "127.0.0.1" or not address.port or address.username or len(token) != 64:
                raise RuntimeError("Owned controller cleanup metadata is invalid")
            connection = http.client.HTTPConnection("127.0.0.1", address.port, timeout=5)
            try:
                connection.request("POST", "/stop", headers={"Authorization": "Bearer " + token})
                connection.getresponse().read()
            except (OSError, http.client.HTTPException):
                pass  # A concurrent orderly shutdown can close its response.
            finally:
                connection.close()
        # Mapped supervisor images and private directory pins may finish closing
        # just after successful control RPC; bounded cleanup does not hide failures.
        for attempt in range(20):
            try:
                shutil.rmtree(root)
                break
            except OSError:
                if attempt == 19:
                    raise
                time.sleep(0.1)


if __name__ == "__main__":
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")
    main()
