"""Passive CLI parity under native write denial, using owned synthetic state."""
import argparse
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import subprocess
import shutil
import sys
import threading
import time
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))

from fixture_paths import temporary_fixture
from sandbox_query_fixture import Sandbox, fixture_environment, inventory
from vault_fixture import retire_session


def summary(code, body):
    result = {"exit": code, "ok": body["ok"]}
    report = body.get("data", {})
    if not body["ok"]:
        error = body["error"]
        result["code"] = error["code"]
        report = error.get("details", {}).get("report", {})
    if "ready" in report:
        result["ready"] = report["ready"]
    if "issues" in report:
        result["issues"] = [{"section": item["section"], "code": item["code"]} for item in report["issues"]]
    if "external_executions" in report:
        result["external_executions"] = report["external_executions"]
    if isinstance(report.get("item"), dict) and "state" in report["item"]:
        result["item_state"] = report["item"]["state"]
        result["item_reason"] = report["item"].get("reason")
    observations = []

    def visit(value):
        if isinstance(value, dict):
            if "state" in value and "reason" in value and any(key in value for key in ("delivered", "current", "integrity_verified")):
                observations.append({key: value[key] for key in ("state", "reason")})
            if value.get("id") in ("value_inputs", "vault_session", "bindings"):
                observations.append({key: value[key] for key in ("id", "status", "reason")})
            for child in value.values():
                visit(child)
        elif isinstance(value, list):
            for child in value:
                visit(child)

    visit(body)
    if observations:
        result["observations"] = observations
    if "synthetic-log" in json.dumps(report):
        result["captured_log"] = True
    return result


@contextmanager
def unavailable_supervisor(control):
    # Keep the actual managed process and its lease alive while an owned HTTP
    # endpoint rejects status RPC. Vault inspection still uses its Unix socket.
    class Unavailable(BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(503)
            self.end_headers()

        def log_message(self, *_):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Unavailable)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    original = control.read_bytes()
    record = json.loads(original)
    record["address"] = "http://127.0.0.1:" + str(server.server_port)
    thread.start()
    try:
        control.write_text(json.dumps(record), encoding="utf-8")
        yield
    finally:
        control.write_bytes(original)
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
        assert not thread.is_alive()


def run(binary, probe=None):
    binary = Path(binary).resolve()
    with temporary_fixture(prefix="devtools-inspection-sandbox-") as temporary:
        root, scratch = temporary / "store", temporary / "scratch"
        scratch.mkdir(mode=0o700)
        environment = fixture_environment(scratch)
        cli = [str(binary), "--format", "json", "--isolated-root", str(root)]
        passphrase = "synthetic-inspection-passphrase"
        secret = "synthetic-inspection-token"
        records = []
        process_id = serving_id = None

        def call(arguments, body=None, sandbox=None):
            command = [*cli, *map(str, arguments)]
            result = sandbox.run(command, check=False, timeout=25) if sandbox else subprocess.run(
                command, cwd=scratch, env=environment, input=body, text=True, capture_output=True, timeout=25)
            assert passphrase not in result.stdout + result.stderr and secret not in result.stdout + result.stderr
            assert not (result.stdout and result.stderr), "mixed response streams"
            response = json.loads(result.stdout or result.stderr)
            assert response["schema_version"] == 1
            return result.returncode, response

        def setup(arguments, body=None):
            code, response = call(arguments, body)
            if code:
                raise RuntimeError("inspection fixture setup failed: " + " ".join(arguments[:2]) + " / " + response["error"]["code"])
            return response["data"]

        def parity(name, arguments, sandbox):
            expected = summary(*call(arguments))
            observed = summary(*call(arguments, sandbox=sandbox))
            assert expected == observed, {"case": name, "writable": expected, "readonly": observed}
            records.append({"case": name, "result": observed})
            return observed

        try:
            setup(["init", "--profile", "app"])
            for name, inject, executable in (
                ("plain", False, ["/usr/bin/true"]),
                ("injected", True, ["/usr/bin/true"]),
                ("hold", True, ["/bin/sh", "-c", "printf 'synthetic-log\\n'; printf '%s\\n' \"$TOKEN\"; exec sleep 600"]),
            ):
                setup(["command", "add", name, "--stdin"], json.dumps({"exec": executable, "inject": inject}))
            setup(["var", "set", "PUBLIC", "--value", "synthetic-public"])
            setup(["var", "set", "OTHER", "--profile", "other", "--value", "synthetic-other"])
            setup(["task", "add", "--title", "Synthetic task", "--request-id", str(uuid.uuid4())])
            setup(["vault", "init", "--backend", "passphrase", "--passphrase-stdin"], passphrase + "\n")
            setup(["secret", "set", "TOKEN", "--stdin"], secret)
            revision = setup(["values", "revision"])["revision"]
            receipt_id, prepare_id = str(uuid.uuid4()), str(uuid.uuid4())
            setup(["var", "set", "PUBLIC", "--value", "synthetic-revised", "--revision", revision, "--request-id", receipt_id])
            prepared = setup(["values", "execution", "prepare", "--request-id", prepare_id, "--stdin"],
                             json.dumps({"external_execution_id": "synthetic-job", "selection": {"keys": ["TOKEN"]}}))
            execution_id = prepared["item"]["execution_id"]
            process_id = setup(["process", "start", "hold", "--no-proxy", "--capture-logs", "--request-id", str(uuid.uuid4())])["item"]["id"]
            deadline = time.monotonic() + 10
            while "synthetic-log" not in json.dumps(setup(["process", "logs", process_id])):
                assert time.monotonic() < deadline, "captured fixture log did not become available"
                time.sleep(0.02)
            sandbox = Sandbox(root, scratch)
            missing = str(uuid.uuid4())
            cases = [
                ("doctor project none", ["doctor", "--project-only", "--bundle", "none", "--check"]),
                ("doctor plain", ["doctor", "plain", "--check"]),
                ("doctor injected", ["doctor", "injected", "--check"]),
                ("values inspect none", ["values", "inspect", "--bundle", "none"]),
                ("values inspect all", ["values", "inspect"]),
                ("values uses", ["values", "uses"]),
                ("values revision", ["values", "revision"]),
                ("bundle list", ["bundle", "list"]),
                ("values request show", ["values", "request", "show", receipt_id]),
                ("execution list", ["values", "execution", "list"]),
                ("execution history", ["values", "execution", "list", "--view", "history"]),
                ("execution status", ["values", "execution", "status", execution_id]),
                ("execution request status", ["values", "execution", "status", "--request-id", prepare_id]),
                ("runner show missing", ["values", "runner", "show", missing]),
                ("profile list", ["profile", "list"]),
                ("profile inspect", ["profile", "inspect", "app"]),
                ("profile diff", ["profile", "diff", "app", "other"]),
                ("diagnostics", ["diagnostics"]),
                ("vault status", ["vault", "status"]),
                ("env list", ["env", "list"]),
                ("var list", ["var", "list"]),
                ("var get", ["var", "get", "PUBLIC"]),
                ("secret list", ["secret", "list"]),
                ("task list", ["task", "list"]),
                ("task current", ["task", "current"]),
                ("process list", ["process", "list"]),
                ("process history", ["process", "list", "--view", "history"]),
                ("exposure list", ["expose", "list"]),
                ("process status", ["process", "status", process_id]),
                ("project status", ["project", "status"]),
                ("port list", ["port", "list"]),
                ("instance list", ["instance", "list"]),
                ("proxy status", ["proxy", "status"]),
                ("proxy list", ["proxy", "list"]),
                ("backup status", ["backup", "status"]),
                ("config inspect", ["config", "inspect"]),
                ("config check", ["config", "check"]),
                ("command list", ["command", "list"]),
                ("command inspect", ["command", "inspect", "injected"]),
                ("command explain", ["command", "explain", "injected"]),
                ("process logs", ["process", "logs", process_id]),
                ("project logs", ["project", "logs", "hold"]),
            ]
            # Warm all ordinary query initialization before denying writes.
            for _, arguments in cases:
                call(arguments)
            before = inventory(root / "data")
            idle = setup(["vault", "status"])
            for name, arguments in cases:
                parity(name, arguments, sandbox)
            # A writable new project can infer a name against read-only data.
            # Existing metadata must still block accidental namespace sharing.
            for profile, expected in (("app", 3), ("new-init", 0)):
                init_directory = scratch / profile
                init_directory.mkdir(mode=0o700)
                init_sandbox = Sandbox(root, init_directory)
                response = init_sandbox.run([*cli, "init", "--no-interactive"], check=False, timeout=20)
                assert response.returncode == expected and not (response.stdout and response.stderr)
                body = json.loads(response.stdout or response.stderr)
                if expected:
                    assert body["error"]["code"] == "profile_name_in_use"
                    assert not (init_directory / "devtools.toml").exists()
                else:
                    assert body["data"]["item"]["profile_presence"] == "absent"
                    assert body["data"]["item"]["profile_source"] == "directory"
                records.append({"case":"init readonly namespace: "+profile,"exit":expected})
            denied_project = scratch / "denied-init-project"
            denied_project.mkdir(mode=0o700)
            denied_scratch = temporary / "denied-init-scratch"
            denied_scratch.mkdir(mode=0o700)
            denied = Sandbox(denied_project, denied_scratch)
            result = denied.run(["/bin/sh","-c",'cd "$1" && shift && exec "$@"',"fixture",str(denied_project),*cli,"init","--profile","explicit"],check=False,timeout=20)
            assert result.returncode == 1 and not result.stdout
            assert json.loads(result.stderr)["error"]["code"] == "io_error"
            assert not any(denied_project.iterdir()), "failed init left a configuration or staging file"
            records.append({"case":"init readonly project","exit":1})
            assert inventory(root / "data") == before, "passive matrix changed managed data"
            assert setup(["vault", "status"]) == idle, "passive matrix changed key session state or idle"
            nested = Sandbox(root / "data" / "profiles", scratch, readonly=[root / "data" / "tasks"],
                             writable=[root / "data" / ".maintenance"])
            if probe:
                positive = nested.run([str(Path(probe).resolve()), "try-write", str(root / "data" / ".maintenance" / "lock")])
                assert json.loads(positive.stdout) == {"event": "acquired"}
            for name, arguments in cases[13:16]:
                parity(name + ": writable global gate", arguments, nested)
            with unavailable_supervisor(root / "data" / "processes" / process_id / "control.json"):
                before = inventory(root / "data")
                observed = parity("process status: supervisor RPC unavailable", ["process", "status", process_id], sandbox)
                assert observed.get("item_state") == "unknown", observed
                assert inventory(root / "data") == before
            ports_project = scratch / "ports"
            ports_project.mkdir(mode=0o700)
            sleep = shutil.which("sleep")
            assert sleep, "native sleep executable is unavailable"
            (ports_project / "devtools.toml").write_text('profile="port-fixture"\n[ports.web]\nrange=[42000,43000]\n[commands.serving]\nexec='+json.dumps([sleep,"600"])+ '\ninject=false\nserve=["web"]\n', encoding="utf-8")
            serving_id = setup(["process", "start", "serving", "--dir", str(ports_project), "--no-proxy", "--request-id", str(uuid.uuid4())])["item"]["id"]
            for state in ("busy", "idle"):
                if state == "idle":
                    setup(["process", "stop", serving_id, "--request-id", str(uuid.uuid4())])
                    serving_id = None
                observed = parity("doctor serving: " + state, ["doctor", "serving", "--dir", str(ports_project), "--check"], sandbox)
                assert observed.get("ready") == (state == "idle"), observed
            return {"queries": records, "inventory_unchanged": True, "idle_unchanged": True,
                    "supervisor_fallback": True, "nested_readonly": True, "busy_idle_claims": True}
        finally:
            for owned in (serving_id, process_id):
                if owned:
                    code, _ = call(["process", "stop", owned, "--request-id", str(uuid.uuid4())])
                    assert code == 0, "owned fixture process did not stop"
            endpoint = root / "data" / "vault" / "session.json"
            if endpoint.exists():
                retire_session(endpoint)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--probe", type=Path)
    args = parser.parse_args()
    print(json.dumps(run(args.binary, args.probe)))
