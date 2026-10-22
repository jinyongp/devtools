"""Run real development servers behind an isolated installed devtools proxy.

Framework dependencies come from the fixture's frozen lockfile. Browser trust
must be prepared on a disposable runner, or contained in --client-mode docker.
The harness never modifies the developer host's DNS or certificate stores.
"""

import argparse
import base64
from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import ssl
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlparse
import uuid


ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "verify/fixtures/proxy-compat"
CASES = {
    "nuxt": ("nuxt/bin/nuxt.mjs", ["dev", "--host", "127.0.0.1"], "app/pages/index.vue"),
    "vite-react": ("vite/bin/vite.js", ["--host", "127.0.0.1", "--strictPort"], "src/App.jsx"),
    "vite-vue": ("vite/bin/vite.js", ["--host", "127.0.0.1", "--strictPort"], "src/App.vue"),
    "next": ("next/dist/bin/next", ["dev", "--webpack", "--hostname", "127.0.0.1"], "app/page.jsx"),
    "sveltekit": ("vite/bin/vite.js", ["--host", "127.0.0.1", "--strictPort"], "src/routes/+page.svelte"),
    "webpack": ("webpack-cli/bin/cli.js", ["serve", "--config", "webpack.config.mjs"], "src/main.js"),
    "ws": (None, [], None),
    "socketio": (None, [], None),
}
IMAGE = "devtools-proxy-browser:1.63.0"


def redact(value):
    text = re.sub(r"(?i)authorization\s*[:=]\s*(?:Basic|Bearer)\s+[^\s,\"']+", "authorization=[redacted]", str(value))
    text = re.sub(r"(?i)(authorization|cookie|password|secret)\s*[:=]\s*[^\s,]+", r"\1=[redacted]", text)
    return re.sub(r"(?i)([?&](?:token|auth|key|sid)=)[^&\s\"']+", r"\1[redacted]", text)


def command(args, **kwargs):
    result = subprocess.run([str(x) for x in args], text=True, capture_output=True, timeout=kwargs.pop("timeout", 45), **kwargs)
    if result.returncode:
        raise RuntimeError(f"{args[0]} exited {result.returncode}: {redact(result.stderr[-6000:] or result.stdout[-6000:])}")
    return result.stdout


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


@contextmanager
def owned_scratch(result):
    scratch = Path(tempfile.mkdtemp(prefix="devtools-proxy-compat-"))
    try:
        yield scratch
    finally:
        if any(key.endswith("cleanup_error") for key in result):
            result["recovery_root"] = str(scratch)
        else:
            shutil.rmtree(scratch)


def stop_owned(process):
    if process is None or process.poll() is not None:
        return
    if os.name == "nt":
        subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"], capture_output=True, timeout=15)
    else:
        os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        if os.name != "nt":
            os.killpg(process.pid, signal.SIGKILL)
        process.kill()
        process.wait(timeout=5)


def ready(process, port, host, log):
    deadline = time.monotonic() + 120
    last = None
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"development server exited {process.returncode}: {redact(log.read_text(errors='replace')[-6000:])}")
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=2)
        try:
            connection.request("GET", "/", headers={"Host": host})
            response = connection.getresponse()
            response.read()
            if response.status == 200:
                return
            last = f"HTTP {response.status}"
        except (OSError, http.client.HTTPException) as error:
            last = str(error)
        finally:
            connection.close()
        # Bounded polling establishes readiness; it never retries a browser test.
        time.sleep(0.1)
    raise RuntimeError(f"development server did not become ready: {last}; {redact(log.read_text(errors='replace')[-6000:])}")


def rejected_origins(origin, ca, kind, auth=None, connect_host=None):
    parsed = urlparse(origin)
    path = "/socket" if kind == "ws" else "/socket.io/?EIO=4&transport=websocket"
    results = []
    inputs = ["https://different.localhost", "null", f"{parsed.scheme}://{parsed.hostname}:1"]
    if kind == "ws":
        inputs.append(None)
    for other in [origin, *inputs]:
        connection = socket.create_connection((connect_host or parsed.hostname, parsed.port or (443 if parsed.scheme == "https" else 80)), timeout=5)
        if parsed.scheme == "https":
            connection = ssl.create_default_context(cafile=str(ca) if ca else None).wrap_socket(connection, server_hostname=parsed.hostname)
        try:
            headers = [f"GET {path} HTTP/1.1", f"Host: {parsed.netloc}", "Connection: Upgrade", "Upgrade: websocket", "Sec-WebSocket-Version: 13", "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==", "X-Forwarded-Host: different.localhost"]
            if other is not None:
                headers.append("Origin: " + other)
            if auth:
                headers.append("Authorization: Basic " + base64.b64encode(auth.encode()).decode())
            connection.sendall(("\r\n".join(headers) + "\r\n\r\n").encode())
            response = connection.makefile("rb")
            status = response.readline().decode().strip()
            code = int(status.split()[1])
            expected = code == 101 if other == origin else 400 <= code < 500
            if not expected:
                raise AssertionError(f"origin {other!r} had unexpected result: {status}")
            results.append({"origin": other, "status": code})
        finally:
            connection.close()
    return results


def public_status(origin, ca=None, auth=None, connect_host=None):
    parsed = urlparse(origin)
    if parsed.scheme == "https":
        connection = http.client.HTTPSConnection(parsed.hostname, parsed.port or 443, timeout=10, context=ssl.create_default_context(cafile=str(ca) if ca else None))
    else:
        connection = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=10)
    if connect_host:
        connection._create_connection = lambda address, timeout, source_address=None: socket.create_connection((connect_host, address[1]), timeout)
    try:
        headers = {}
        if auth:
            headers["Authorization"] = "Basic " + base64.b64encode(auth.encode()).decode()
        connection.request("GET", "/", headers=headers)
        response = connection.getresponse()
        response.read()
        return response.status
    finally:
        connection.close()


def windows_path(path):
    return command(["wslpath", "-w", str(path)]).strip()


def run_case(options, binary, target, case, scheme, policy, browser, artifacts):
    artifacts.mkdir(parents=True)
    result = {"target": target, "case": case, "scheme": scheme, "policy": policy, "browser": browser, "provider": options.provider, "auth_mode": options.auth_mode}
    environment = dict(os.environ)
    environment.pop("DEVTOOLS_TASK_CONTEXT", None)
    environment.pop("DEVTOOLS_ISOLATED_ROOT", None)
    environment.pop("PROXY_COMPAT_AUTH", None)
    for key in ("TS_AUTHKEY", "GH_TOKEN", "GITHUB_TOKEN"):
        environment.pop(key, None)
    environment.update(NUXT_TELEMETRY_DISABLED="1", NEXT_TELEMETRY_DISABLED="1")
    server = None
    sibling_server = None
    configured = False
    exposures = []
    trusted = False
    container = "devtools-proxy-" + uuid.uuid4().hex
    windows_peer = options.client_mode == "windows-peer"
    certutil = "/mnt/c/Windows/System32/certutil.exe" if windows_peer else "certutil"
    serve_before = None
    if options.provider == "tailscale":
        serve_before = json.loads(command(["tailscale", "serve", "status", "--json"]))
    with owned_scratch(result) as scratch:
        scratch = Path(scratch)
        consumer_home = scratch / "home"
        consumer_home.mkdir()
        environment.update(HOME=str(consumer_home), USERPROFILE=str(consumer_home), XDG_CONFIG_HOME=str(consumer_home / ".config"), XDG_DATA_HOME=str(consumer_home / ".local/share"), XDG_CACHE_HOME=str(consumer_home / ".cache"))
        isolated = scratch / "state"
        work = scratch / "fixtures"
        shutil.copytree(FIXTURES, work, ignore=shutil.ignore_patterns("node_modules", "pnpm-lock.yaml"))
        os.symlink(FIXTURES / "node_modules", work / "node_modules", target_is_directory=True)
        backend_port, http_port, tls_port = free_port(), free_port(), free_port()
        while len({backend_port, http_port, tls_port}) != 3:
            backend_port, http_port, tls_port = free_port(), free_port(), free_port()
        origin = f"{scheme}://{options.hostname}:{tls_port if scheme == 'https' else http_port}"
        ca = artifacts / "ca.pem" if scheme == "https" else None
        connect_host = "127.0.0.1"
        auth = "reader:" + uuid.uuid4().hex if options.auth_mode == "basic" else None
        if auth:
            environment["PROXY_COMPAT_AUTH"] = json.dumps(dict(zip(("username", "password"), auth.split(":", 1))))
        environment.update(PROXY_COMPAT_PORT=str(backend_port), PROXY_COMPAT_ORIGIN=origin)
        log = scratch / "server.log"

        def api(*args, mutate=False, auth_input=None):
            argv = [binary, "--isolated-root", isolated, *args]
            if mutate:
                argv += ["--request-id", str(uuid.uuid4())]
            return json.loads(command(argv, env=environment, cwd=scratch, input=auth_input))["data"]

        def expose(name, sibling=False):
            args = ["expose", "start", name, "--global", "--provider", options.provider]
            if options.provider == "lan":
                args += ["--alias", ("sibling." if sibling else "") + "compat.local"]
            if options.provider == "tailscale":
                args += ["--serve-port", str(options.tailscale_serve_port + int(sibling))]
            args += ["--auth-stdin"] if auth else ["--no-auth"]
            item = api(*args, mutate=True, auth_input=auth + "\n" if auth else None)["item"]
            exposures.append(item["id"])
            return item

        try:
            result["cli"] = api("version")
            result["binary_sha256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
            result["lock_sha256"] = hashlib.sha256((FIXTURES / "pnpm-lock.yaml").read_bytes()).hexdigest()
            api("proxy", "ca", "init", mutate=True)
            api("proxy", "ca", "export", "--output", str(artifacts / "ca.pem"))
            bind = "0.0.0.0" if options.provider == "lan" else "127.0.0.1"
            configure = ["proxy", "configure", "--http-address", f"{bind}:{http_port}"]
            if scheme == "https":
                configure += ["--https-address", f"{bind}:{tls_port}"]
            api(*configure, mutate=True)
            configured = True
            add = ["proxy", "add", "app", str(backend_port), "--global", "--host", options.hostname, "--dns", "external"]
            if target == "candidate":
                add += ["--host-policy", policy]
            api(*add, mutate=True)
            api("proxy", "up", "app", "--global", mutate=True)
            shared = None
            if options.provider != "local":
                shared = expose("app")
                origin = shared["url"].rstrip("/")
                if options.provider in {"cloudflared", "tailscale"}:
                    ca, connect_host = None, None
                environment["PROXY_COMPAT_ORIGIN"] = origin
                sibling_port = free_port()
                sibling_add = ["proxy", "add", "sibling", str(sibling_port), "--global", "--host", "sibling.compat.localhost", "--dns", "external"]
                if target == "candidate":
                    sibling_add += ["--host-policy", policy]
                api(*sibling_add, mutate=True)
                sibling = expose("sibling", sibling=True)
                sibling_environment = dict(environment, PROXY_COMPAT_PORT=str(sibling_port), PROXY_COMPAT_ORIGIN=sibling["url"])
                sibling_server = subprocess.Popen(["node", "server.mjs"], cwd=work / "ws", env=sibling_environment, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=os.name != "nt")
                ready(sibling_server, sibling_port, urlparse(sibling["url"]).netloc, log)
                result["ingress"] = {"url": origin, "state": shared["state"], "peer_network": options.peer_network or ("docker bridge" if options.client_mode == "docker" else "prepared client")}
            entry, args, marker = CASES[case]
            if entry:
                argv = ["node", str(work / "node_modules" / entry), *args]
                if case != "webpack":
                    argv += ["--port", str(backend_port)]
            else:
                argv = ["node", "server.mjs"]
            with log.open("w") as stream:
                server = subprocess.Popen(argv, cwd=work / case, env=environment, stdout=stream, stderr=subprocess.STDOUT, start_new_session=os.name != "nt")
            ready(server, backend_port, urlparse(origin).netloc, log)
            if ca:
                environment["NODE_EXTRA_CA_CERTS"] = str(ca)
            result["server_node"] = command(["node", "--version"]).strip()
            if options.provider in {"cloudflared", "tailscale"}:
                result["provider_version"] = command([options.provider, "--version" if options.provider == "cloudflared" else "version"]).splitlines()[0]
            if auth:
                if public_status(origin, ca, connect_host=connect_host) != 401:
                    raise AssertionError("unauthenticated ingress did not challenge")
                result["auth_challenge"] = 401
            if options.disposable_trust and ca:
                command([certutil, "-user", "-addstore", "Root", windows_path(ca) if windows_peer else ca])
                trusted = True
            browser_config = {
                "case": case, "origin": origin, "policy": policy, "browser": browser,
                "backendAuthority": f"127.0.0.1:{backend_port}",
                "artifacts": str(artifacts), "fixtureWork": str(work),
                "fixturePackages": str(FIXTURES / "node_modules"),
                "hmrFile": f"{case}/{marker}" if marker else None,
                "ca": str(ca) if ca else None,
                "stopProbe": shared is not None and (policy == "preserve" or case == "nuxt"),
            }
            if options.client_mode == "docker":
                browser_config.update(artifacts="/artifacts", fixtureWork="/fixture", fixturePackages="/fixture-packages", ca="/artifacts/ca.pem" if ca else None)
            elif windows_peer:
                browser_config.update(artifacts=windows_path(artifacts), fixtureWork=windows_path(work), fixturePackages=windows_path(options.peer_packages), ca=windows_path(ca) if ca else None)
            (artifacts / "browser.json").write_text(json.dumps(browser_config), encoding="utf8")
            if options.client_mode == "docker":
                network = options.peer_network or ("host" if options.provider == "local" else "bridge")
                argv = ["docker", "run", "--rm", "--name", container, "--network", network, "-e", "PROXY_COMPAT_AUTH",
                        "--mount", f"type=bind,source={artifacts},target=/artifacts",
                        "--mount", f"type=bind,source={work},target=/fixture",
                        "--mount", f"type=bind,source={FIXTURES / 'node_modules'},target=/fixture-packages,readonly",
                        "--mount", f"type=bind,source={ROOT / 'node_modules'},target=/runner/node_modules,readonly",
                        "--mount", f"type=bind,source={ROOT / 'verify/proxy_compat_browser.mjs'},target=/runner/browser.mjs,readonly", IMAGE]
                if options.provider in {"local", "lan"}:
                    host = urlparse(origin).hostname
                    argv[2:2] = ["--add-host", f"{host}:{'127.0.0.1' if options.provider == 'local' else 'host-gateway'}"]
            elif windows_peer:
                argv = [str(options.windows_node), windows_path(options.windows_runner), windows_path(artifacts / "browser.json")]
                environment["WSLENV"] = ":".join(filter(None, [environment.get("WSLENV"), "NODE_EXTRA_CA_CERTS/w", "PROXY_COMPAT_AUTH/w"]))
                if ca:
                    environment["NODE_EXTRA_CA_CERTS"] = windows_path(ca)
            else:
                argv = ["node", str(ROOT / "verify/proxy_compat_browser.mjs"), str(artifacts / "browser.json")]
            # The browser result is retained even when a browser assertion fails.
            with subprocess.Popen([str(x) for x in argv], env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True) as worker:
                deadline = time.monotonic() + 300
                while worker.poll() is None and time.monotonic() < deadline:
                    if shared and (artifacts / "ready-to-stop").exists() and shared["id"] in exposures:
                        if case in {"ws", "socketio"} and policy == "preserve":
                            result["rejected_origins"] = rejected_origins(origin, ca, case, auth, connect_host)
                        stopped = api("expose", "stop", shared["id"], mutate=True)["item"]
                        exposures.remove(shared["id"])
                        if stopped["state"] != "stopped" or public_status(sibling["url"], ca, auth, connect_host) != 200:
                            raise AssertionError("stop did not preserve the sibling ingress")
                        result["stop_and_sibling"] = True
                        (artifacts / "stop-done").write_text("stopped", encoding="utf8")
                    time.sleep(0.1)
                if worker.poll() is None:
                    worker.kill()
                    raise TimeoutError("browser worker exceeded 300 seconds")
                stdout, stderr = worker.communicate(timeout=10)
                browser_code = worker.returncode
            browser_result = artifacts / "browser-result.json"
            if not browser_result.exists():
                raise RuntimeError(f"browser worker failed: {redact(stderr[-6000:] or stdout[-6000:])}")
            result["browser_result"] = json.loads(browser_result.read_text())
            if browser_code or result["browser_result"]["status"] != "pass":
                raise AssertionError(result["browser_result"].get("error", "browser worker failed"))
            logs = log.read_text(errors="replace")
            if case == "nuxt":
                warnings = logs.count("Ignored a cross-origin RPC connection")
                result["rpc_origin_warnings"] = warnings
                same_origin = sum("Ignored a cross-origin RPC connection" in line and f'from origin "{origin}"' in line for line in logs.splitlines())
                result["same_origin_rpc_warnings"] = same_origin
                if policy == "preserve" and same_origin:
                    raise AssertionError("same-origin Nuxt RPC was rejected")
                if policy == "rewrite" and not same_origin:
                    raise AssertionError("Nuxt baseline origin rejection was not reproduced")
            if case in {"ws", "socketio"} and policy == "preserve" and shared is None:
                result["rejected_origins"] = rejected_origins(origin, ca, case, auth, connect_host)
            result["status"] = "pass"
        except Exception as error:
            result.update(status="fail", error=redact(error))
        finally:
            if options.client_mode == "docker":
                subprocess.run(["docker", "rm", "-f", container], capture_output=True, timeout=20)
            if configured and options.provider != "local":
                try:
                    # A failed start can leave recoverable metadata/pins before
                    # returning an ID. This root contains only this run's records.
                    exposures = [item["id"] for item in api("expose", "list")["items"] if item["state"] != "stopped"]
                except Exception as error:
                    result.update(status="fail", exposure_cleanup_error=redact(error))
            for exposure in reversed(exposures):
                try:
                    api("expose", "stop", exposure, mutate=True)
                except Exception as error:
                    result.update(status="fail", exposure_cleanup_error=redact(error))
            if serve_before is not None:
                try:
                    after = json.loads(command(["tailscale", "serve", "status", "--json"]))
                    if after != serve_before:
                        raise AssertionError("unowned Serve state changed")
                    result["serve_configuration_restored"] = True
                except Exception as error:
                    result.update(status="fail", serve_cleanup_error=redact(error))
            if configured:
                try:
                    api("proxy", "stop", "--all", mutate=True)
                except Exception as error:
                    result.update(status="fail", cleanup_error=redact(error))
            stop_owned(server)
            stop_owned(sibling_server)
            if trusted:
                thumbprint = hashlib.sha1(ssl.PEM_cert_to_DER_cert((artifacts / "ca.pem").read_text())).hexdigest()
                try:
                    command([certutil, "-user", "-delstore", "Root", thumbprint])
                except Exception as error:
                    result.update(status="fail", trust_cleanup_error=redact(error))
            if log.exists():
                (artifacts / "server.log").write_text(redact(log.read_text(errors="replace")), encoding="utf8")
    (artifacts / "result.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf8")
    print(json.dumps({k: v for k, v in result.items() if k not in {"browser_result"}}), flush=True)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--baseline", type=Path)
    parser.add_argument("--artifacts", type=Path, default=Path(tempfile.gettempdir()) / "devtools-proxy-compat-results")
    parser.add_argument("--case", action="append", choices=list(CASES))
    parser.add_argument("--scheme", action="append", choices=["http", "https"])
    parser.add_argument("--policy", action="append", choices=["rewrite", "preserve"])
    parser.add_argument("--browser", action="append", choices=["chromium", "firefox", "webkit"])
    parser.add_argument("--hostname", default="compat.localhost")
    parser.add_argument("--client-mode", choices=["local", "docker", "windows-peer"], default="local")
    parser.add_argument("--windows-node", type=Path, help="Explicit Windows Node executable for the WSL browser peer")
    parser.add_argument("--windows-runner", type=Path, help="Browser worker in a Windows checkout with Windows dependencies installed")
    parser.add_argument("--peer-packages", type=Path, help="Windows fixture node_modules, expressed as a WSL path")
    parser.add_argument("--provider", choices=["local", "lan", "cloudflared", "tailscale"], default="local")
    parser.add_argument("--live", action="store_true", help="Explicitly authorize this run's selected external ingress and cleanup")
    parser.add_argument("--auth-mode", choices=["none", "basic"], default="none")
    parser.add_argument("--peer-network", help="Prepared Docker peer network, e.g. container:tailscale-peer; never host for shared tests")
    parser.add_argument("--tailscale-serve-port", type=int, default=8443, help="Two free dedicated ports are required (this port and the next)")
    parser.add_argument("--disposable-trust", action="store_true", help="Import/remove the test CA in a disposable Windows runner's CurrentUser root store")
    options = parser.parse_args()
    options.binary = options.binary.resolve(strict=True)
    if options.provider != "local" and not options.live:
        parser.error("Nonlocal providers require explicit --live; no exposure was started")
    if options.provider == "local" and options.auth_mode != "none":
        parser.error("Basic sharing authentication requires a nonlocal provider")
    if options.provider != "local" and options.peer_network == "host":
        parser.error("Shared tests require a separate peer network")
    if options.provider == "tailscale" and (not options.peer_network or not options.peer_network.startswith("container:")):
        parser.error("Tailscale requires a prepared second tailnet device in --peer-network container:NAME")
    if options.provider == "cloudflared" and "2026.10.3" not in command(["cloudflared", "--version"]):
        parser.error("Prepare cloudflared 2026.10.3 before this live run")
    if options.provider == "tailscale":
        if command(["tailscale", "version"]).splitlines()[0] != "1.104.2":
            parser.error("Prepare Tailscale 1.104.2 before this live run")
        peer = options.peer_network.split(":", 1)[1]
        if command(["docker", "exec", peer, "tailscale", "version"]).splitlines()[0] != "1.104.2":
            parser.error("Prepare Tailscale 1.104.2 on the peer device too")
        host_ip = command(["tailscale", "ip", "-4"]).strip()
        peer_ip = command(["docker", "exec", peer, "tailscale", "ip", "-4"]).strip()
        if not host_ip or not peer_ip or host_ip == peer_ip:
            parser.error("A distinct logged-in tailnet peer is required")
    windows_peer = options.client_mode == "windows-peer"
    if windows_peer and (sys.platform != "linux" or "microsoft" not in Path("/proc/sys/kernel/osrelease").read_text().lower() or not all([options.windows_node, options.windows_runner, options.peer_packages])):
        parser.error("windows-peer requires actual WSL2 and explicit prepared Windows browser paths")
    if options.disposable_trust and ((os.name != "nt" and not windows_peer) or os.environ.get("DEVTOOLS_DISPOSABLE_HOST") != "1"):
        parser.error("--disposable-trust requires an explicitly disposable Windows runner")
    if not (FIXTURES / "node_modules").is_dir() or not (FIXTURES / "pnpm-lock.yaml").is_file():
        parser.error("Install verify/fixtures/proxy-compat with pnpm --frozen-lockfile first")
    if options.client_mode == "docker" and sys.platform != "linux":
        parser.error("Docker host-network browser mode requires a Linux execution host")
    run = options.artifacts.resolve() / (datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "-" + uuid.uuid4().hex[:8])
    run.mkdir(parents=True)
    results = []
    targets = [("candidate", options.binary)]
    if options.baseline:
        targets.insert(0, ("baseline", options.baseline.resolve(strict=True)))
    for target, binary in targets:
        for case in options.case or CASES:
            for scheme in options.scheme or (["https"] if options.provider in {"cloudflared", "tailscale"} else ["http", "https"]):
                for policy in (["rewrite"] if target == "baseline" else options.policy or ["rewrite", "preserve"]):
                    for browser in options.browser or ["chromium"]:
                        destination = run / f"{target}-{case}-{scheme}-{policy}-{browser}"
                        results.append(run_case(options, binary, target, case, scheme, policy, browser, destination))
    (run / "summary.json").write_text(json.dumps(results, indent=2) + "\n", encoding="utf8")
    print(f"Evidence: {run}", flush=True)
    return int(any(result["status"] != "pass" for result in results))


if __name__ == "__main__":
    raise SystemExit(main())
