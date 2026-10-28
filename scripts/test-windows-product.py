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

import package as release_package
from vault_test_support import retire_vaults
from native_rewrap import check_native_rewrap
from cli_json import json_argv, json_environment

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'verify'))
from cli_reliability_fixture import check_cli_reliability
from windows_console import windows_terminal


class CLIError(RuntimeError):
    def __init__(self, arguments, code, details):
        self.code, self.details = code, details or {}
        safe = {key: self.details.get(key) for key in ("operation", "phase", "reason", "effects")}
        safe["facts"] = {key: self.details.get("facts", {}).get(key) for key in ("stage", "os_code")}
        safe["conditions"] = [cause.get("code") for cause in self.details.get("causes", [])]
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
    if os.name != "nt" or os.environ.get("DEVTOOLS_DISPOSABLE_HOST") != "1":
        raise RuntimeError("This journey requires explicitly disposable native Windows Actions")
    repository = Path.cwd()
    root = Path(tempfile.mkdtemp(prefix="devtools-native-product-"))
    reliability_root=(root/'reliability').resolve()
    project = root / "프로젝트 공백"
    project.mkdir()
    binary = root / "installed" / "bin" / "devtools.exe"
    base_env = json_environment()
    base_env.pop("DEVTOOLS_ISOLATED_ROOT", None)
    source_env = dict(base_env, DEVTOOLS_ISOLATED_ROOT=str(root / "source"))
    target_env = dict(base_env, DEVTOOLS_ISOLATED_ROOT=str(root / "target"))
    native_env = dict(base_env)
    native_root = None
    steps = []

    def raw(arguments, env=source_env, input="", timeout=40):
        result = subprocess.run(json_argv(binary,arguments,environment=env,cwd=project), cwd=project, env=env, input=input.encode("utf-8"),
                                capture_output=True, timeout=timeout)
        if result.returncode:
            try:
                error = json.loads(result.stderr)["error"]
                raise CLIError(arguments,error["code"],error.get("details"))
            except (ValueError, KeyError, TypeError):
                reason = "non-JSON failure (raw output withheld)"
            raise RuntimeError(f"Installed command {arguments[:3]} exited {result.returncode}: {reason}")
        return result.stdout.decode("utf-8")

    def api(*arguments, env=source_env, input="", timeout=40):
        result = json.loads(raw(list(arguments), env=env, input=input, timeout=timeout))
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
            "-Source", str(args.release_dir.resolve()), "-BinDir", str(binary.parent), "-NoPath", "-Json",
        ], cwd=repository, env=base_env, text=True, encoding="utf-8", capture_output=True, timeout=180)
        assert install.returncode == 0, "Candidate installer did not complete (raw output withheld)"
        identity = api("version")
        assert identity["version"] == args.version and identity["commit"] == args.commit and identity["protocol_version"] == 19
        # Real native Windows pipe/explicit modes. Console VT and ConPTY visual
        # acceptance are separate gates; a captured pipe cannot establish them.
        clean = {key: value for key, value in base_env.items() if key not in (
            "DEVTOOLS_FORMAT", "AI_AGENT", "CODEX_THREAD_ID", "CODEX_CI", "CLAUDECODE",
            "CLAUDE_CODE", "CURSOR_AGENT", "GEMINI_CLI", "OPENCODE", "OPENCODE_CLIENT")}
        captured = subprocess.run([str(binary), "version"], cwd=project, env=clean,
                                  capture_output=True, text=True, encoding="utf-8", timeout=15)
        assert captured.returncode == 0 and json.loads(captured.stdout)["data"] == identity
        captured = subprocess.run([str(binary), "--format", "text", "version"], cwd=project,
                                  env=dict(clean, DEVTOOLS_FORMAT="invalid"), capture_output=True,
                                  text=True, encoding="utf-8", timeout=15)
        assert captured.returncode == 0 and args.version in captured.stdout and "\x1b[" not in captured.stdout
        steps.append("native_pipe_and_explicit_presentation")
        reliability=check_cli_reliability(binary,reliability_root,base_env,expected_commit=args.commit,terminal=windows_terminal)
        assert reliability['result']=='pass'
        steps.append("cli_reliability_recovery_inputs_inventory_cursor_backup")
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
        api("vault", "init", "--backend", "passphrase", "--passphrase-stdin", input="synthetic-source-vault-unlock\n")
        api("sec", "set", "TOKEN", "--stdin", input="synthetic-secret-canary\n")
        assert api("var", "get", "MESSAGE")["value"] == "portable-public-canary"
        task = mutate("task", "add", "--title", "Installed native task")["item"]["id"]
        assert api("task", "show", task)["item"]["id"] == task
        steps.append("values_secret_and_task_journal")

        # Guarded mutations share the legacy writer's revision and replay the
        # original outcome after another installed CLI invocation changes it.
        edit = api("values", "revision")["revision"]
        guarded_id = str(uuid.uuid4())
        guarded = ["var", "set", "GUARDED", "--value", "committed", "--revision", edit]
        mutate(*guarded, request=guarded_id)
        api("var", "set", "GUARDED", "--value", "later")
        assert mutate(*guarded, request=guarded_id)["replayed"] is True
        assert api("var", "get", "GUARDED")["value"] == "later"
        assert api("values", "request", "show", guarded_id)["item"]["state"] == "completed"
        generate_id = str(uuid.uuid4())
        generate = ["secret", "generate", "GENERATED", "--revision", api("values", "revision")["revision"]]
        generated = mutate(*generate, request=generate_id)
        assert generated["changed"] is True and set(generated) == {"profile", "changed", "revision", "replayed"}
        assert mutate(*generate, request=generate_id)["replayed"] is True
        consumer = "const v=process.env.GENERATED;if(!/^[A-Za-z0-9_-]{43}$/.test(v)||Buffer.from(v,'base64url').length!==32)process.exit(1);process.stdout.write('generated-ok')"
        assert raw(["run", "--", node, "-e", consumer]) == "generated-ok"
        idle = api("vault", "status")["item"]["idle_expires_at"]
        assert api("values", "request", "show", generate_id)["item"]["result"]["revision"] == generated["revision"]
        assert api("vault", "status")["item"]["idle_expires_at"] == idle
        steps.append("guarded_mutation_replay_generation_and_passive_lookup")

        # Exercise the installed package through the same portable scenario as
        # Unix; only the cooperating caller executable is built from this SHA.
        receiver = root / "value-handoff-runner.exe"
        subprocess.run(["go", "build", "-o", str(receiver), "./examples/value-handoff-runner"],
                       cwd=repository, check=True, timeout=90)
        sys.path.insert(0, str(repository / "verify"))
        from value_handoff_fixture import run as run_value_handoff
        run_value_handoff(binary, receiver, root / "handoff", base_env)
        steps.append("installed_private_value_handoff_and_successor_recovery")

        # Exercise the packaged CLI's default OS provider as well as the
        # explicit passphrase roots used by the portable-transfer journey.
        # Native mutation is intentionally forbidden in isolated product roots.
        # Claim only a nonexistent default root on this disposable CI account.
        native_paths = api("project", "inspect", env=native_env)["paths"]
        candidate_root = Path(native_paths["data"]).parent
        assert candidate_root.is_absolute()
        assert all(Path(native_paths[name]) == candidate_root / name for name in ("config", "data", "cache", "runtime"))
        if os.path.lexists(candidate_root):
            raise RuntimeError("Refusing to touch an existing native user root")
        candidate_root.mkdir()
        native_root = candidate_root
        native = api("vault", "init", env=native_env)["item"]
        assert native["backend"] == "dpapi" and native["state"] == "unlocked"
        api("sec", "set", "NATIVE_TOKEN", "--stdin", env=native_env, input="synthetic-native-dpapi-canary")
        native_command = ["run", "--", node, "-e", "process.stdout.write(process.env.NATIVE_TOKEN)"]
        assert raw(native_command, env=native_env) == "[REDACTED]"
        native_directory = native_root / "data" / "vault"
        old_session = json.loads((native_directory / "session.json").read_text())["session_id"]
        retire_vaults(native_directory)
        # A new worker must use DPAPI automatically, without explicit unlock or
        # a passphrase; an old cached key cannot satisfy this assertion.
        assert raw(native_command, env=native_env) == "[REDACTED]"
        assert json.loads((native_directory / "session.json").read_text())["session_id"] != old_session
        steps.append("default_dpapi_silent_native_reopen_after_session_retirement")
        api("vault", "lock", env=native_env)
        try:
            raw(native_command, env=native_env)
            raise AssertionError("Manual lock silently reopened the native provider")
        except CLIError as error:
            assert error.code == "requirements_failed", error
            assert error.details.get("phase") == "preflight", error
            assert any(cause.get("code") == "vault_locked"
                       for cause in error.details.get("causes", [])), error
        assert api("vault", "unlock", env=native_env)["item"]["state"] == "unlocked"
        assert raw(native_command, env=native_env) == "[REDACTED]"
        assert api("vault", "status", env=native_env)["item"]["backend"] == "dpapi"
        steps.append("default_dpapi_native_helper_manual_lock_and_explicit_unlock")

        # Exercise the actual updater while native data remains in place. Drain
        # this root's old key session so a cached key cannot hide a new-image
        # DPAPI access failure. Other owned roots retain their existing sessions.
        api("vault", "lock", env=native_env)
        retire_vaults(native_root / "data" / "vault")
        update_version = args.version + (".native-update" if "-" in args.version else "-native-update")
        architecture = subprocess.check_output(["go", "env", "GOHOSTARCH"], env=base_env, text=True).strip()
        update_directory = root / "native-update-releases"
        release_package.package(env=dict(base_env, VERSION=update_version, COMMIT=args.commit,
                                        TARGET_OS="windows", TARGET_ARCH=architecture, OUTPUT_DIR=str(update_directory)))
        changed = api("update", "--version", update_version, "--source", str(update_directory), env=native_env, timeout=120)
        assert changed["changed"] and api("version", env=native_env)["version"] == update_version
        assert api("vault", "unlock", env=native_env)["item"]["state"] == "unlocked"
        assert raw(native_command, env=native_env) == "[REDACTED]"
        updated_native = api("vault", "status", env=native_env)["item"]
        assert updated_native["backend"] == "dpapi" and updated_native["vault_id"] == native["vault_id"]
        steps.append("default_dpapi_data_and_cold_native_unlock_after_self_update")
        native_rewrap = check_native_rewrap(binary, native_env, project, 'native-product', native_directory)
        assert native_rewrap['same_root_and_profile_bytes'] and native_rewrap['native_noop_and_replay']
        steps.append("public_dpapi_passphrase_transition_replay_noop_and_manual_lock")

        http_port, tls_port = available_port(), available_port()
        mutate("proxy", "configure", "--http-address", f"127.0.0.1:{http_port}", "--https-address", f"127.0.0.1:{tls_port}")
        request = str(uuid.uuid4())
        up = mutate("project", "up", "web", "--timeout", "15s", request=request, settle=True)
        running = True
        assert up["items"][0]["status"] == "ready"
        assert up["items"][0]["item"]["proxy_mode"]
        execution = up["items"][0]["item"]["id"]
        replay = mutate("project", "up", "web", "--timeout", "15s", request=request, settle=True)
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
        api("vault", "init", "--backend", "passphrase", "--passphrase-stdin", env=target_env, input="synthetic-target-vault-unlock\n")
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
        result = {"commit": args.commit, "version": args.version, "final_version": update_version, "protocol": identity["protocol_version"],
                  "exe_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                  "steps": {step: "pass" for step in steps}, "isolated": False,"cli_reliability":reliability,
                  "isolated_transfer_and_managed_roots": True, "native_provider_disposable_user_root": True,
                  "os_trust_modified": False, "real_pc": "NOT RUN"}
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
        vault_roots = [root / "source" / "data" / "vault", root / "target" / "data" / "vault"]
        if native_root is not None:
            vault_roots.append(native_root / "data" / "vault")
        vault_roots.extend(path.parent for path in reliability_root.rglob('vault/session.json'))
        retire_vaults(*vault_roots)
        # Mapped supervisor images and private directory pins may finish closing
        # just after successful control RPC; bounded cleanup does not hide failures.
        for owned_root in ([native_root] if native_root is not None else []) + [root]:
            for attempt in range(20):
                try:
                    shutil.rmtree(owned_root)
                    break
                except OSError:
                    if attempt == 19:
                        raise
                    time.sleep(0.1)
    result["owned_roots_removed"] = True
    (repository / "windows-installed-product-results.json").write_text(json.dumps(result, indent=2) + "\n")


if __name__ == "__main__":
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")
    main()
