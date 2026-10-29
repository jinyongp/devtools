"""Actual task CLI queries under native data/cache write denial.

Only synthetic fixtures are used. Context credentials never enter reports.
Run with a separately built, immutable pre-change CLI for interoperability.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import uuid

from sandbox_query_fixture import Sandbox, fixture_environment, held, inventory
from fixture_paths import temporary_fixture

BASELINE_COMMIT = "f8c22ac3674a14c8d57ed919e866fc104d6d7b5f"


def identity(binary, environment, cwd):
    result = subprocess.run([str(binary), "version"], env=environment, cwd=cwd,
                            text=True, capture_output=True, check=True, timeout=15)
    return {"version": json.loads(result.stdout)["data"],
            "sha256": hashlib.sha256(binary.read_bytes()).hexdigest()}


def trace_summary(path, data):
    lines = path.read_text().splitlines()
    accesses, writes = [], []
    for line in lines:
        if str(data) not in line:
            continue
        if re.search(r'\b(open|openat|openat2|stat|lstat|newfstatat|statx|readlink)\(', line):
            accesses.append(line)
        if re.search(r'\b(open|openat|openat2)\(', line) and re.search(r'O_(WRONLY|RDWR|CREAT|TRUNC|APPEND)', line):
            writes.append(line)
        if re.search(r'\b(mkdir|mkdirat|unlink|unlinkat|rename|renameat|renameat2|chmod|fchmod|fchmodat|truncate|ftruncate)\(', line):
            writes.append(line)
        if re.search(r'\b(write|pwrite64)\(\d+<' + re.escape(str(data)), line):
            writes.append(line)
    assert not writes, "task query attempted a data/lock write"
    return {"data_accesses": len(accesses), "data_write_attempts": len(writes)}


def run(binary, baseline, probe, trace=None):
    with temporary_fixture(prefix="devtools-task-sandbox-") as temporary:
        root = temporary / "store"
        scratch, cache = temporary / "scratch", temporary / "query-cache"
        scratch.mkdir(mode=0o700)
        cache.mkdir(mode=0o700)
        environment = fixture_environment(scratch)
        environment["DEVTOOLS_FORMAT"] = "json"
        credentials = []
        profile = "task-sandbox"
        baseline_identity = identity(baseline, environment, scratch)
        assert baseline_identity["version"]["commit"] == BASELINE_COMMIT
        assert baseline_identity["version"]["protocol_version"] == 19
        candidate_identity = identity(binary, environment, scratch)
        assert candidate_identity["version"]["protocol_version"] == 20
        trace_reports = []

        def argv(arguments, selected=binary):
            return [str(selected), "--isolated-root", str(root), "task", *map(str, arguments), "--profile", profile]

        def api(*arguments, selected=binary, sandbox=None, body=None, context=None, error=None, traced=False):
            command = argv(arguments, selected)
            env = dict(sandbox.environment if sandbox else environment)
            env["DEVTOOLS_FORMAT"] = "json"
            if context:
                env["DEVTOOLS_TASK_CONTEXT"] = context
            if body is not None:
                command += ["--stdin"]
            if sandbox:
                command = sandbox.command(command)
            trace_file = scratch / (str(uuid.uuid4()) + ".trace")
            if trace and traced:
                command = [str(trace), "-f", "-qq", "-yy", "-s", "4096", "-e",
                           "trace=%file,write,pwrite64,ftruncate,fchmod", "-o", str(trace_file), *command]
                if sandbox and sandbox.privileged:
                    # Tracing sudo as an unprivileged process suppresses its
                    # privilege transition. Trace the privileged launcher with
                    # the same credential-free synthetic environment instead.
                    command = ["sudo", "-n", "env", *[key + "=" + value for key, value in env.items()], *command]
            p = subprocess.run(command, cwd=scratch, env=env, text=True, capture_output=True,
                               input=json.dumps(body) if body is not None else None, timeout=20)
            assert not (p.stdout and p.stderr), "mixed CLI response streams"
            assert p.returncode == 0 if error is None else p.returncode != 0, (arguments, p.returncode)
            response = json.loads(p.stderr if error else p.stdout)
            assert response["schema_version"] == 1 and response["ok"] == (error is None)
            if traced and trace:
                raw = trace_file.read_text()
                assert all(secret not in raw for secret in credentials), "context entered syscall trace"
                trace_reports.append(trace_summary(trace_file, root / "data"))
                trace_file.unlink()
            if error:
                assert response["error"]["code"] == error, response["error"]["code"]
                return response["error"]
            result = response["data"]
            if "context" in result and isinstance(result["context"], str):
                credentials.append(result["context"])
            else:
                assert all(secret not in p.stdout for secret in credentials), "query exposed execution context"
            return result

        def mutate(*arguments, **kwargs):
            return api(*arguments, "--request-id", str(uuid.uuid4()), **kwargs)

        # Use the actual old CLI to initialize and write active v3 task storage.
        tasks = [mutate("add", "--title", "Synthetic task " + str(n), selected=baseline)["item"]["id"] for n in range(51)]
        workstreams = [mutate("workstream", "create", "--title", "Synthetic workstream " + str(n))["item"]["id"] for n in range(3)]
        validation = mutate("validation", "add", "--title", "Synthetic validation", "--method", "fixture", "--task", tasks[0])["item"]["id"]
        claims = [mutate("claim", task, "--dir", scratch) for task in tasks[:2]]
        claim = claims[0]
        for n in range(3):
            mutate("checkpoint", claim["run"]["id"], body={"summary":"Synthetic checkpoint " + str(n)}, context=claim["context"])
        data = root / "data"
        queries = [
            ("list", "--limit", "200"), ("show", tasks[0]), ("next",),
            ("current", "--dir", scratch, "--limit", "200"), ("context", tasks[0]),
            ("history", tasks[0], "--limit", "200"), ("impact", tasks[0]),
            ("tree", "--limit", "200"), ("export", tasks[0]),
            ("checkpoint", "list", claim["run"]["id"], "--limit", "200"),
            ("workstream", "list", "--limit", "200"), ("workstream", "show", workstreams[0]),
            ("workstream", "context", workstreams[0]), ("workstream", "history", workstreams[0]),
            ("workstream", "impact", workstreams[0]), ("workstream", "tree", "--limit", "200"),
            ("workstream", "export", workstreams[0]), ("workstream", "check", workstreams[0]),
            ("workstream", "spec", "show", workstreams[0]), ("workstream", "plan", "show", workstreams[0]),
            ("validation", "list"), ("validation", "show", validation),
        ]
        golden = [api(*query) for query in queries]
        assert len(queries) == 22
        # Prepare frozen cursors while both boundaries are writable.
        paged = api("list", "--limit", "1", "--query-cache-dir", cache)
        graph = api("tree", "--limit", "1", "--query-cache-dir", cache)
        before_data, before_cache = inventory(data), inventory(cache)

        def controls(directory):
            control = directory / "policy-control"
            control.mkdir(mode=0o700, exist_ok=True)
            for name in ("new", "renamed"):
                (control / name).unlink(missing_ok=True)
            for name in ("lock", "write", "rename", "unlink"):
                (control / name).write_bytes(b"synthetic")
                (control / name).chmod(0o600)
            return control

        # Every combination has independent OS-level write controls on BOTH paths.
        combinations = []
        for mode in ("ro-ro", "ro-rw", "rw-rw"):
            data_control, cache_control = controls(data), controls(cache)
            sandbox = None if mode == "rw-rw" else Sandbox(data, scratch, readonly=[cache] if mode == "ro-ro" else [], writable=[cache] if mode == "ro-rw" else [])
            for directory, writable in ((data_control, mode == "rw-rw"), (cache_control, mode != "ro-ro")):
                command = [str(probe), "allow-writes" if writable else "deny-writes", str(directory)]
                p = sandbox.run(command) if sandbox else subprocess.run(command, env=environment, cwd=scratch, text=True, capture_output=True, check=True, timeout=15)
                checks = [json.loads(line) for line in p.stdout.splitlines()]
                assert len(checks) == 5 and all(check["denied"] == (not writable) for check in checks)
            before_data, before_cache = inventory(data), inventory(cache)
            for query, expected in zip(queries, golden):
                result = api(*query, sandbox=sandbox, traced=bool(sandbox))
                assert result == expected, query
            assert inventory(data) == before_data, "task query changed data/locks"
            assert inventory(cache) == before_cache, "single query changed explicit cache"
            # Both indexed and graph cursors work with a read-only cache.
            page = api("list", "--limit", "200", "--cursor", paged["next_cursor"], "--query-cache-dir", cache, sandbox=sandbox, traced=bool(sandbox))
            tree = api("tree", "--limit", "200", "--cursor", graph["cursor"], "--query-cache-dir", cache, sandbox=sandbox, traced=bool(sandbox))
            assert page["revision"] == paged["revision"] and len(page["items"]) == 50
            assert tree["revision"] == graph["revision"] and not tree["truncated"]
            if mode == "ro-ro":
                for query in (("list", "--limit", "1"), ("tree", "--limit", "1")):
                    failure = api(*query, "--query-cache-dir", cache, sandbox=sandbox, error="storage_error")
                    assert failure["details"]["facts"]["resource"] == "query_cache"
                    remedy = failure["details"]["remedies"][0]
                    assert "--query-cache-dir" in remedy["argv"] and remedy["effect"] == "value_write"
            else:
                api("list", "--limit", "1", "--query-cache-dir", cache, sandbox=sandbox, traced=bool(sandbox))
                api("tree", "--limit", "1", "--query-cache-dir", cache, sandbox=sandbox, traced=bool(sandbox))
            assert inventory(data) == before_data
            combinations.append({"mode":mode,"queries":22,"data_inventory_unchanged":True,"policy_controls":10})

        # All four existing locks block readers; cancellation releases handles.
        profile_path = data / "tasks" / ("p1-" + hashlib.sha256(profile.encode()).hexdigest() + ".json.lock")
        gates = [data / ".maintenance" / name for name in ("lock", "lock.turnstile")]
        gates += [profile_path, Path(str(profile_path) + ".turnstile")]
        sandbox = Sandbox(data, scratch, readonly=[cache])
        for gate in gates:
            with held([str(probe), "hold-write", str(gate)]):
                operation, path = ("query", data) if gate.parent.name == ".maintenance" else ("profile-query", profile_path)
                blocked = sandbox.run([str(probe), operation, str(path)])
                assert json.loads(blocked.stdout) == {"event":"deadline_exceeded"}
                # Signal the CLI itself; a sandbox launcher can consume SIGINT.
                process = subprocess.Popen(argv(("next",)), env=environment, cwd=scratch,
                                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                try:
                    time.sleep(0.15)
                    assert process.poll() is None, "query bypassed existing writer"
                    process.send_signal(signal.SIGINT)
                    out, err = process.communicate(timeout=5)
                    assert process.returncode == 130 and not out and json.loads(err)["error"]["code"] == "canceled", (process.returncode, out, err)
                finally:
                    if process.poll() is None:
                        process.kill(); process.communicate(timeout=5)
            api("next", sandbox=sandbox)
        with held(sandbox.command([str(probe), "hold-profile-query", str(profile_path)]), sandbox.environment, scratch):
            result = subprocess.run([str(probe), "try-write", str(profile_path)], text=True, capture_output=True, check=True, timeout=10)
            assert json.loads(result.stdout) == {"event":"busy"}

        # Cached cursor reads must not require even the existence of active data.
        moved = root / "data.saved"
        data.rename(moved)
        try:
            api("list", "--cursor", paged["next_cursor"], "--query-cache-dir", cache, sandbox=sandbox, traced=True)
            assert not data.exists()
            if trace:
                assert trace_reports[-1]["data_accesses"] == 0
        finally:
            moved.rename(data)
        # Stock CLI continues writing after candidate reads, same v3 store.
        previous = api("list", "--limit", "200")["revision"]
        mutate("add", "--title", "Old writer after candidate", selected=baseline)
        current = api("list", "--limit", "200", sandbox=sandbox)
        assert current["revision"] == previous + 1 and len(current["items"]) == 52
        assert api("list", "--limit", "200", selected=baseline) == current

        # Failed writable preparation is explicit and leaves the RO store alone.
        profile_path.unlink()
        before = inventory(data)
        failure = api("next", sandbox=sandbox, error="storage_error")
        details = failure["details"]
        assert details["facts"]["state"] == "initialization_required" and details["effects"] == "unknown"
        remedy = details["remedies"][0]
        assert remedy["argv"][:3] == ["devtools", "task", "next"] and remedy["effect"] == "value_write"
        assert remedy["argv"][remedy["argv"].index("--profile") + 1] == profile
        assert remedy["argv"][remedy["argv"].index("--isolated-root") + 1] == str(root)
        assert inventory(data) == before
        preparation = Sandbox(cache, scratch, writable=[data])
        api("next", sandbox=preparation)
        assert profile_path.exists()
        # With >50 tasks, next remains a valid preparation action even when the
        # default cache cannot be written. List would require a snapshot.
        api("list", sandbox=preparation, error="storage_error")
        api("next", sandbox=preparation)
        recovery_file = data / "tasks" / "6f74686572.json"
        recovery_file.write_bytes(b"partial")
        recovery_file.chmod(0o600)
        pending = data / ".maintenance" / "restore-pending.json"
        pending.write_text(json.dumps([{"path":"tasks/6f74686572.json", "exists":True,
                                        "data":base64.b64encode(b"committed").decode()}]))
        pending.chmod(0o600)
        before = inventory(data)
        failure = api("next", sandbox=sandbox, error="storage_error")
        assert failure["details"]["facts"]["state"] == "recovery_required"
        assert inventory(data) == before
        api("next", sandbox=preparation)
        assert recovery_file.read_bytes() == b"committed" and not pending.exists()
        api("next", sandbox=sandbox)
        assert not trace or any(report["data_accesses"] for report in trace_reports)
        return {"os":sys.platform,"baseline":baseline_identity,"candidate":candidate_identity,
                "combinations":combinations,"query_count":22,"lock_contention_and_cancel":4,
                "baseline_cli_roundtrip":True,"cursor_without_data":True,"preparation_recovery_roundtrip":True,
                "syscall_trace":trace_reports if trace else "not available on this OS"}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--probe", type=Path, required=True)
    parser.add_argument("--trace", type=Path)
    parser.add_argument("--artifacts", type=Path)
    args = parser.parse_args()
    result = run(args.binary.resolve(), args.baseline.resolve(), args.probe.resolve(), args.trace.resolve() if args.trace else None)
    if args.artifacts:
        args.artifacts.mkdir(parents=True, exist_ok=True)
        (args.artifacts / "task-queries.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
