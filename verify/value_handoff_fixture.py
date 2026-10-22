"""Portable installed caller/receiver journey; only synthetic private data."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

REPOSITORY = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPOSITORY / "scripts"))
from vault_test_support import retire_vaults


def run(binary, receiver, directory, base_env=None):
    directory = Path(directory).resolve()
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    project = directory / "project"
    project.mkdir()
    isolated = directory / "store"
    env = dict(base_env or os.environ, DEVTOOLS_ISOLATED_ROOT=str(isolated))
    env.pop("DEVTOOLS_TASK_CONTEXT", None)
    canary = "synthetic-installed-handoff-secret"
    other = "synthetic-outside-scope-secret"
    passphrase = "synthetic-installed-handoff-passphrase"
    children = []
    log_handles = []
    runner, instance = str(uuid.uuid4()), str(uuid.uuid4())
    ledger, effects = directory / "engine-ledger", directory / "effects"

    def api(*args, body=None, text=None, error=None):
        if body is not None:
            text = json.dumps(body)
        result = subprocess.run([str(binary), *args], cwd=project, env=env,
                                input=text, text=True, capture_output=True, timeout=30)
        for secret in (canary, other, passphrase):
            assert secret not in result.stdout + result.stderr, "private input reached CLI response"
        assert not (result.stdout and result.stderr), "mixed response streams"
        response = json.loads(result.stderr if result.returncode else result.stdout)
        assert response["schema_version"] == 1
        if error:
            assert not response["ok"] and response["error"]["code"] == error, (args[:3], response.get("error", {}).get("code"))
            return response["error"]
        assert result.returncode == 0 and response["ok"], (args[:3], response.get("error", {}).get("code"))
        encoded = json.dumps(response)
        for field in ('"patterns"', '"ticket"', '"values"', '"master_key"'):
            if args[:2] == ("values", "execution"):
                assert field not in encoded, "private field reached public execution output"
        return response["data"]

    def execution(action, execution_id=None, body=None, request=None, error=None):
        args = ["values", "execution", action]
        if execution_id:
            args.append(execution_id)
        args += ["--profile", "handoff", "--request-id", request or str(uuid.uuid4()), "--stdin"]
        return api(*args, body=body, error=error)

    helper = "import os,sys; assert 'TOKEN' in os.environ and 'OTHER' not in os.environ; f=open(sys.argv[1],'a'); f.write('effect\\n'); f.flush(); os.fsync(f.fileno()); f.close(); print(os.environ['TOKEN'],flush=True); print(os.environ['TOKEN'],file=sys.stderr,flush=True)"

    def start(current_instance):
        out_path, err_path = directory / (current_instance + ".out"), directory / (current_instance + ".err")
        out, diagnostic = out_path.open("wb"), err_path.open("wb")
        log_handles.extend([out, diagnostic])
        child = subprocess.Popen([str(receiver), "--root", str(isolated / "data"),
                                  "--runner-id", runner, "--instance-id", current_instance,
                                  "--ledger", str(ledger), "--program", sys.executable,
                                  "--", "-c", helper, str(effects)], cwd=project, env=env,
                                 stdin=subprocess.DEVNULL, stdout=out, stderr=diagnostic)
        children.append(child)
        request = str(uuid.uuid4())
        body = {"runner_id": runner, "instance_id": current_instance, "protocol": 1, "redaction_protocol": 1}
        deadline = time.monotonic() + 15
        while True:
            assert child.poll() is None, "owned receiver exited before registration"
            result = subprocess.run([str(binary), "values", "runner", "register", "--request-id", request, "--stdin"],
                                    cwd=project, env=env, input=json.dumps(body), text=True, capture_output=True, timeout=10)
            if result.returncode == 0:
                assert json.loads(result.stdout)["data"]["item"]["instance_id"] == current_instance
                break
            assert json.loads(result.stderr)["error"]["code"] == "runner_protocol_mismatch"
            assert time.monotonic() < deadline, "owned receiver did not become ready"
            time.sleep(.05)
        return child, out_path, err_path

    def lookup():
        result = subprocess.run([str(receiver), "--ledger", str(ledger), "--lookup-job", "job-42"],
                                env=env, cwd=project, capture_output=True, text=True, timeout=10)
        assert result.returncode == 0, "durable caller lookup unavailable"
        assert canary not in result.stdout + result.stderr
        return json.loads(result.stdout)

    try:
        assert api("version")["protocol_version"] == 11
        api("init", "--profile", "handoff")
        api("vault", "init", "--backend", "passphrase", "--passphrase-stdin", text=passphrase + "\n")
        api("secret", "set", "TOKEN", "--stdin", text=canary)
        api("secret", "set", "OTHER", "--stdin", text=other)
        bundles = api("bundle", "list")
        api("bundle", "set", "api", "--key", "TOKEN", "--revision", bundles["revision"], "--request-id", str(uuid.uuid4()))
        initialized = subprocess.run([str(receiver), "--ledger", str(ledger), "--init-ledger"], env=env, capture_output=True, timeout=10)
        assert initialized.returncode == 0 and not initialized.stdout and not initialized.stderr
        child, out_path, err_path = start(instance)
        address = api("values", "runner", "address", runner, "--instance", instance)
        assert address["protocol"] == address["redaction_protocol"] == 1
        prepare_id = str(uuid.uuid4())
        selection = {"external_execution_id": "job-42", "selection": {"keys": ["TOKEN"], "bundle": "api"}}
        prepared = execution("prepare", body=selection, request=prepare_id)["item"]
        recovered = api("values", "execution", "status", "--profile", "handoff", "--request-id", prepare_id)["item"]
        assert recovered == prepared
        assert execution("prepare", body=selection, request=prepare_id)["replayed"]
        # An expanded bundle cannot cross the original caller ceiling; already
        # frozen bytes retain the original one-key selection.
        bundles = api("bundle", "list")
        api("bundle", "set", "api", "--key", "TOKEN", "--key", "OTHER", "--revision", bundles["revision"], "--request-id", str(uuid.uuid4()))
        execution("prepare", body=selection, error="selection_outside_scope")
        execution("deliver", prepared["execution_id"], {"revision": prepared["revision"]}, error="approval_required")
        assert not effects.exists(), "selection implicitly started a job"
        approval = {"revision": prepared["revision"], "runner_id": runner, "runner_instance_id": instance,
                    "external_execution_id": prepared["external_execution_id"], "input_id": prepared["input_id"]}
        approved = execution("approve", prepared["execution_id"], approval)["item"]
        send_id = str(uuid.uuid4())
        send_body = {"revision": approved["revision"]}
        # Discard the real successful response, then recover it with the same
        # input/UUID; effect counters and durable engine lookup prove no rerun.
        discarded = subprocess.run([str(binary), "values", "execution", "deliver", approved["execution_id"],
                                    "--profile", "handoff", "--request-id", send_id, "--stdin"],
                                   cwd=project, env=env, input=json.dumps(send_body), text=True,
                                   stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=30)
        assert discarded.returncode == 0 and not discarded.stderr
        delivered = execution("deliver", approved["execution_id"], send_body, send_id)
        assert delivered["replayed"] and delivered["item"]["delivery_state"] == "delivered"
        deadline = time.monotonic() + 15
        while True:
            fact = lookup()
            logs = out_path.read_bytes() + err_path.read_bytes()
            assert canary.encode() not in logs and other.encode() not in logs
            if fact["state"] == "finished" and logs.count(b"[REDACTED]") == 2:
                break
            assert time.monotonic() < deadline, "owned helper did not finish with protected output"
            time.sleep(.05)
        assert effects.read_text() == "effect\n" and fact["input_id"] == prepared["input_id"]
        child.terminate(); child.wait(timeout=10)
        api("values", "runner", "unregister", runner, "--instance", instance, "--request-id", str(uuid.uuid4()))
        successor = str(uuid.uuid4())
        start(successor)
        fact = lookup()
        report = {"revision": delivered["item"]["revision"], "runner_id": runner,
                  "external_execution_id": fact["external_execution_id"], "input_id": fact["input_id"],
                  "state": "finished", "external_ref": fact["external_ref"], "exit_code": fact["exit_code"]}
        reported = execution("report", prepared["execution_id"], report)["item"]
        assert reported["reporter_instance_id"] == successor and reported["runner_instance_id"] == instance
        assert reported["execution_state"] == "reported_finished" and reported["fact_source"] == "runner_reported"
        assert execution("deliver", prepared["execution_id"], send_body, send_id)["item"] == delivered["item"]
        execution("deliver", prepared["execution_id"], {"revision": reported["revision"]}, error="delivery_already_started")
        empty = execution("prepare", body={"external_execution_id": "empty-job", "selection": {"keys": []}})["item"]
        assert execution("cancel", empty["execution_id"], {"revision": empty["revision"]})["item"]["delivery_state"] == "canceled"
        page = api("values", "execution", "list", "--profile", "handoff", "--limit", "1")
        assert len(page["items"]) == 1 and page["next_cursor"]
        api("values", "execution", "list", "--profile", "handoff", "--limit", "1", "--cursor", page["next_cursor"])
        idle = api("vault", "status")["item"]["idle_expires_at"]
        summary = api("diagnostics")["external_executions"]
        assert summary["verification"] == "verified" and summary["attempted_inputs"] == 1
        assert api("vault", "status")["item"]["idle_expires_at"] == idle
        pending = execution("prepare", body={"external_execution_id": "locked-job", "selection": {"keys": ["TOKEN"]}})["item"]
        pending = execution("approve", pending["execution_id"], {"revision": pending["revision"], "runner_id": runner,
                             "runner_instance_id": successor, "external_execution_id": pending["external_execution_id"], "input_id": pending["input_id"]})["item"]
        locked_send = str(uuid.uuid4())
        api("vault", "lock")
        execution("deliver", pending["execution_id"], {"revision": pending["revision"]}, locked_send, error="vault_locked")
        api("vault", "unlock", "--passphrase-stdin", text=passphrase + "\n")
        # The original admission ticket's key-session epoch remains locked even
        # after the new session is explicitly unlocked; it is never refreshed.
        execution("deliver", pending["execution_id"], {"revision": pending["revision"]}, locked_send, error="vault_locked")
        execution("cancel", pending["execution_id"], {"revision": pending["revision"]})
        api("vault", "lock")
        assert api("diagnostics")["external_executions"] == {"verification": "verification_pending"}
        api("values", "execution", "status", prepared["execution_id"], "--profile", "handoff", error="vault_locked")
        assert effects.read_text() == "effect\n"
        # Only persisted fixture data/logs are scanned. OS coordination leases
        # are excluded because Windows denies reads while a byte-range lock is held.
        for path in directory.rglob("*"):
            if path.is_file() and path.suffix != ".lock":
                body = path.read_bytes()
                for secret in (canary, other, passphrase):
                    assert secret.encode() not in body, "plaintext persisted outside the private channel"
        print("Installed approved local handoff, lost-response replay, protected logs and successor recovery passed")
    finally:
        for child in children:
            if child.poll() is None:
                child.terminate()
            try:
                child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                child.kill(); child.wait(timeout=5)
        for handle in log_handles:
            handle.close()
        retire_vaults(isolated / "data" / "vault")
