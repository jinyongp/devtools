"""Installed metadata queries under enforced native write denial."""
import base64
import hashlib
import json
import os
from pathlib import Path
import selectors
import subprocess
import sys
import uuid

repository = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(repository / "verify"))
from sandbox_query_fixture import Sandbox, held, inventory, native_probe

home = Path.home()
subprocess.run(["sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version",
                "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"]],
               capture_output=True, check=True, timeout=30)
binary = home / ".local/bin/devtools"
probe = home / "query-lock"
subprocess.run(["go", "build", "-o", str(probe), "./verify/probes/sandbox_query_lock.go"],
               cwd=repository, check=True, timeout=60)
scratch = home / "scratch"
scratch.mkdir(mode=0o700)
passphrase = "synthetic-sandbox-passphrase"
secret = "synthetic-sandbox-secret"
checks = []


def invoke(root, profile, args, sandbox=None, state=None, error=None, input=None):
    command = [str(binary), *args, "--isolated-root", str(root)]
    if args[0] not in ("vault", "version"):
        command += ["--profile", profile]
    result = sandbox.run(command, check=False) if sandbox else subprocess.run(
        command, cwd=home, text=True, capture_output=True, input=input, timeout=20)
    assert secret not in result.stdout + result.stderr
    assert passphrase not in result.stdout + result.stderr
    assert not (result.stdout and result.stderr), "mixed response streams"
    assert error or result.returncode == 0, (args, result.returncode, result.stderr)
    response = json.loads(result.stderr if error else result.stdout)
    assert response["schema_version"] == 1 and response["ok"] == (error is None)
    if error:
        assert result.returncode != 0 and not result.stdout
        failure = response["error"]
        assert failure["code"] == error, failure
        if state:
            details = failure["details"]
            assert details["facts"]["state"] == state and details["effects"] == "unknown", details
            assert details["retry"] == "after_dependency_change", details
            assert details["reason"] in ("access_denied", "read_only"), details
            assert details["remedies"][0]["effect"] == "value_write", details
            remedy = details["remedies"][0]["argv"]
            assert remedy[remedy.index("--isolated-root") + 1] == str(root), details
            if "--env" in args:
                assert remedy[remedy.index("--env") + 1] == args[args.index("--env") + 1], details
        return failure
    assert result.returncode == 0, (args, result.returncode, result.stderr)
    return response["data"]


controls = native_probe(probe, home / "controls", scratch)
for protected in (False, True):
    root = home / ("protected" if protected else "public")
    profile = "sandbox-fixture"

    def api(*args, **kwargs):
        return invoke(root, profile, args, **kwargs)

    api("variable", "set", "URL", "--value", "before")
    api("variable", "set", "EMPTY", "--value", "")
    api("env", "create", "local")
    api("variable", "set", "URL", "--value", "local", "--env", "local")
    if protected:
        api("vault", "init", "--backend", "passphrase", "--passphrase-stdin", input=passphrase + "\n")
        api("secret", "set", "TOKEN", "--stdin", input=secret)
    data = root / "data"
    sandbox = Sandbox(data, scratch)
    queries = [(name, "list") for name in ("variable", "var", "secret", "sec", "env")]
    queries += [(name, "get", key, *scope) for name in ("variable", "var")
                for key in ("URL", "EMPTY") for scope in ((), ("--env", "local"))]
    queries += [(name, "list", "--env", "local") for name in ("variable", "var", "secret", "sec")]
    expected = [api(*args) for args in queries]
    for locked in ((False, True) if protected else (False,)):
        if locked:
            api("vault", "lock")
        before = inventory(data)
        for args, golden in zip(queries, expected):
            result = api(*args, sandbox=sandbox)
            assert result == golden, args
            if "integrity_verified" in result:
                assert result["integrity_verified"] is False
        api("var", "list", "--env", "missing", sandbox=sandbox, error="env_not_found")
        assert inventory(data) == before, "metadata query wrote storage/session state"
    checks.append({"protected": protected, "query_count": len(queries), "inventory_unchanged": True})

    if protected:
        continue
    # Legacy input is read without migration, under the same existing gate.
    legacy = data / "profiles" / ("LegacySandbox".encode().hex() + ".json")
    legacy.write_text(json.dumps({"version": 1, "profile": "LegacySandbox", "envs": {}, "keys": {}}))
    legacy.chmod(0o600)
    before = inventory(data)
    invoke(root, "LegacySandbox", ("var", "list"), sandbox=sandbox)
    assert inventory(data) == before

    # A query must wait behind the original writer gate, without opening it writable.
    with held([str(probe), "hold-write", str(data / ".maintenance/lock")]):
        process = subprocess.Popen(sandbox.command([str(binary), "var", "list", "--isolated-root",
                                   str(root), "--profile", profile]), env=sandbox.environment,
                                   cwd=scratch, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                assert not selector.select(0.3), "query bypassed writer gate"
        except BaseException:
            process.kill()
            process.communicate(timeout=10)
            raise
    output, diagnostic = process.communicate(timeout=15)
    assert process.returncode == 0 and not diagnostic and json.loads(output)["ok"]

    # Observe only complete, committed public values while an ordinary CLI writes.
    for value in ("after", "before", "after"):
        writer = subprocess.Popen([str(binary), "var", "set", "URL", "--value", value,
                                   "--isolated-root", str(root), "--profile", profile],
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        assert api("var", "get", "URL", sandbox=sandbox)["value"] in ("before", "after")
        output, diagnostic = writer.communicate(timeout=15)
        assert writer.returncode == 0 and not diagnostic and json.loads(output)["ok"]

    def repair(state):
        before = inventory(data)
        api("var", "list", "--env", "local", sandbox=sandbox, error="storage_error", state=state)
        assert inventory(data) == before, "failed query changed pending storage"
        restored = api("var", "list")
        assert api("var", "list", sandbox=sandbox) == restored

    (data / ".maintenance/lock.turnstile").unlink()
    repair("initialization_required")
    orphan = data / ".maintenance/transactions" / str(uuid.uuid4())
    orphan.mkdir(mode=0o700, parents=True)
    repair("recovery_required")
    assert not orphan.exists()
    canonical = data / "profiles" / ("p1-" + hashlib.sha256(profile.encode()).hexdigest() + ".json")
    original = canonical.read_bytes()
    pending = data / ".maintenance/restore-pending.json"
    pending.write_text(json.dumps([{"path": str(canonical.relative_to(data)),
                                   "data": base64.b64encode(original).decode(), "exists": True}]))
    pending.chmod(0o600)
    canonical.write_bytes(b"partial")
    repair("recovery_required")
    assert canonical.read_bytes() == original and not pending.exists()
    pending.write_bytes(b"invalid-pointer")
    pending.chmod(0o600)
    before = inventory(data)
    api("var", "list", sandbox=sandbox, error="storage_error")
    assert inventory(data) == before
    pending.unlink()

print(json.dumps({"binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                  "version": invoke(home / "public", "sandbox-fixture", ("version",)),
                  "native_controls": controls, "fixtures": checks,
                  "recovery_round_trips": True, "writer_exclusion": True}))
