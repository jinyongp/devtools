"""Reproduce the protocol-25 writer paths a profile rename must fence."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

from fixture_paths import temporary_fixture


def receipt_count(data):
    return sum(len(list((data / directory / "receipts").glob("*.json")))
               for directory in ("processes", "project-operations"))


def record_count(data):
    return len(list((data / "processes").glob("*/record.json")))


def file_state(data):
    return {path.relative_to(data): hashlib.sha256(path.read_bytes()).digest()
            for path in data.rglob("*") if path.is_file()}


def write_private(path, value):
    with path.open("w", encoding="utf-8") as output:
        json.dump(value, output)
    if os.name != "nt":
        path.chmod(0o600)


def wait_for_legacy_gate(waiting, publication):
    descriptors = Path(f"/proc/{waiting.pid}/fd")
    deadline = time.monotonic() + 10
    while True:
        if waiting.poll() is not None or time.monotonic() >= deadline:
            raise RuntimeError("Legacy foreground did not open the publication lock")
        if any(descriptor.is_symlink() and descriptor.resolve() == publication
               for descriptor in descriptors.iterdir()):
            return
        time.sleep(0.01)


def probe_waiting_foreground(argv, scratch, env, data, effect_count):
    """Hold the legacy gate until the actual foreground writer opens it."""
    import fcntl

    publication = data / "processes" / "publication.lock"
    turnstile = publication.with_name(publication.name + ".turnstile")
    before = record_count(data)
    effects_before = effect_count()
    with publication.open("r+b") as gate:
        fcntl.flock(gate, fcntl.LOCK_EX)
        with subprocess.Popen(
            [*argv, "run", "probe"], cwd=scratch, env=env,
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        ) as waiting:
            try:
                wait_for_legacy_gate(waiting, publication)

                # A new open rejects the symlink. The waiter already has the
                # original lock descriptor and holds the original turnstile.
                for path in (publication, turnstile):
                    replacement = path.with_name(path.name + ".blocked")
                    replacement.symlink_to("unused-lock-target")
                    replacement.replace(path)
                fresh = subprocess.run(
                    [*argv, "run", "probe"], cwd=scratch, env=env,
                    stdin=subprocess.DEVNULL, capture_output=True, timeout=10,
                )
                if fresh.returncode == 0 or record_count(data) != before or effect_count() != effects_before:
                    raise RuntimeError("Fresh foreground bypassed the replacement lock")
                fcntl.flock(gate, fcntl.LOCK_UN)
                waiting.communicate(timeout=10)
                if (waiting.returncode or record_count(data) != before + 1
                        or effect_count() != effects_before + 1):
                    raise RuntimeError("Legacy foreground did not publish after its open gate was replaced")
                return {
                    "fresh_exit_code": fresh.returncode,
                    "queued_exit_code": waiting.returncode,
                    "queued_new_records": record_count(data) - before,
                }
            finally:
                if waiting.poll() is None:
                    waiting.kill()
                    waiting.communicate()
                for path in (publication, turnstile):
                    if path.is_symlink():
                        path.unlink()


def probe_closing_path_with_waiter(argv, scratch, env, data, close, observe, effect_count):
    if sys.platform != "linux":
        close()
        return None

    import fcntl

    publication = data / "processes" / "publication.lock"
    fd = os.open(publication, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "r+b") as gate:
        fcntl.flock(gate, fcntl.LOCK_EX)
        with subprocess.Popen(
            [*argv, "run", "probe"], cwd=scratch, env=env,
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        ) as waiting:
            try:
                wait_for_legacy_gate(waiting, publication)
                close()
                before = file_state(observe)
                effects_before = effect_count()
                fcntl.flock(gate, fcntl.LOCK_UN)
                waiting.communicate(timeout=10)
                if (waiting.returncode == 0 or file_state(observe) != before
                        or effect_count() != effects_before):
                    raise RuntimeError("Queued legacy foreground published after its data path was closed")
                return {"queued_exit_code": waiting.returncode, "changed_files": 0, "child_effects": 0}
            finally:
                if waiting.poll() is None:
                    waiting.kill()
                    waiting.communicate()


def probe(binary, scratch, candidate=None):
    env = {key: value for key, value in os.environ.items() if not key.startswith("DEVTOOLS_")}
    argv = [str(binary), "--isolated-root", str(scratch), "--format", "json"]
    data = scratch / "data"
    effects = scratch / "child-effects.txt"
    source = scratch / "devtools.toml"
    child = (
        "from pathlib import Path; import sys\n"
        'with Path(sys.argv[1]).open("a") as output: output.write("ran" + chr(10))'
    )
    execution = [sys.executable, "-c", child, str(effects)]
    source.write_text(
        'profile = "old"\n\n[commands.probe]\n'
        f'exec = {json.dumps(execution, ensure_ascii=False)}\n'
        'inject = false\n',
        encoding="utf-8",
    )

    def effect_count():
        return len(effects.read_text(encoding="utf-8").splitlines()) if effects.exists() else 0

    def call(*arguments):
        return subprocess.run(
            [*argv, *arguments], cwd=scratch, env=env,
            stdin=subprocess.DEVNULL, capture_output=True, timeout=30,
        )

    def require_ok(*arguments):
        result = call(*arguments)
        if result.returncode != 0:
            raise RuntimeError(f"Fixture setup failed for {' '.join(arguments[:2])}")
        return json.loads(result.stdout)["data"]

    def attempts():
        managed_id, project_id = str(uuid.uuid4()), str(uuid.uuid4())
        commands = {
            "values": ["var", "set", "FIXTURE", "--profile", "old", "--value", "synthetic"],
            "tasks": ["task", "add", "--profile", "old", "--title", "Synthetic rename probe"],
            "ports": ["instance", "name", "different"],
            "foreground": ["run", "probe"],
            "managed": ["process", "start", "probe", "--request-id", managed_id],
            "project": ["project", "up", "probe", "--request-id", project_id],
        }
        exits = {kind: call(*args).returncode for kind, args in commands.items()}
        return exits, managed_id, project_id

    version = require_ok("version")
    if version["protocol_version"] != 25:
        raise RuntimeError("Supply a protocol-25 legacy binary")
    require_ok("var", "set", "FIXTURE", "--profile", "baseline", "--value", "synthetic")
    require_ok("instance", "name", "initial")
    registry = data / "ports" / "registry.json"
    registry_doc = json.loads(registry.read_text(encoding="utf-8"))
    if registry_doc["version"] != 4:
        raise RuntimeError("Unexpected legacy registry format")
    registry_doc["version"] = 5
    write_private(registry, registry_doc)
    write_private(data / ".maintenance" / "restore-pending.json", {"version": 5, "phase": "ready"})

    partial, managed_id, project_id = attempts()
    partial_records = record_count(data)
    partial_receipts = receipt_count(data)
    if (not all(partial[kind] for kind in ("values", "tasks", "ports", "managed", "project"))
            or partial["foreground"] != 0 or partial_records != 1 or effect_count() != 1
            or not (data / "processes" / "receipts" / f"{managed_id}.json").is_file()
            or not (data / "project-operations" / "receipts" / f"{project_id}.json").is_file()):
        raise RuntimeError(
            f"Unexpected legacy partial-fence result: {partial}, "
            f"records={partial_records}, receipts={partial_receipts}"
        )

    queued = (probe_waiting_foreground(argv, scratch, env, data, effect_count)
              if sys.platform == "linux" else None)

    # Preserve the canonical data root required by protected handoff records.
    # Give the candidate separate operational paths and close the old ones.
    runtime = data / ".runtime-v26"
    runtime_domains = ("ports", "processes", "project-operations", "network-operations", "proxy", "exposures")
    runtime_marker = b"devtools runtime format 26\n"
    legacy_root_identity = data.stat()

    def close_runtime_paths():
        runtime.mkdir(mode=0o700)
        (data / ".maintenance" / "restore-pending.json").write_bytes(
            b'{"version":5,"phase":"legacy-write-closed"}')
        for name in runtime_domains:
            legacy = data / name
            if legacy.is_dir():
                legacy.rename(runtime / name)
            legacy.write_bytes(runtime_marker)
            if os.name != "nt":
                legacy.chmod(0o600)

    closed_runtime_queued = probe_closing_path_with_waiter(
        argv, scratch, env, data, close_runtime_paths, data, effect_count)
    runtime_before = file_state(data)
    runtime_effects = effect_count()
    runtime_closed, _, _ = attempts()
    if (not all(runtime_closed.values()) or file_state(data) != runtime_before
            or effect_count() != runtime_effects or not os.path.samestat(legacy_root_identity, data.stat())):
        raise RuntimeError(f"Legacy writer bypassed the closed runtime paths: {runtime_closed}")

    if candidate:
        # The malformed registry was a negative control. The candidate uses
        # the original valid registry now isolated from the legacy pathname.
        registry_doc["version"] = 4
        write_private(runtime / "ports" / "registry.json", registry_doc)
        candidate_argv = [str(candidate), *argv[1:]]

        def candidate_call(*arguments):
            result = subprocess.run(
                [*candidate_argv, *arguments], cwd=scratch, env=env,
                stdin=subprocess.DEVNULL, capture_output=True, timeout=30,
            )
            if result.returncode != 0:
                raise RuntimeError(f"Candidate failed after runtime closure: {arguments!r} {result.stderr!r}")
            return result

        value = json.loads(candidate_call("var", "get", "FIXTURE", "--profile", "baseline").stdout)["data"]
        if value["value"] != "synthetic":
            raise RuntimeError("Candidate lost the original public profile")
        candidate_call("var", "set", "FIXTURE", "--profile", "baseline", "--value", "candidate")
        preview = json.loads(candidate_call("profile", "rename", "baseline", "changed").stdout)["data"]
        if not preview["values"] or preview["source_profile"] != "baseline":
            raise RuntimeError("Candidate did not preview the original profile")
        records_before = record_count(runtime)
        candidate_call("run", "probe")
        if record_count(runtime) != records_before + 1 or effect_count() != runtime_effects + 1:
            raise RuntimeError("Candidate did not launch and record one foreground execution")
        if any((data / name).read_bytes() != runtime_marker for name in runtime_domains):
            raise RuntimeError("Candidate reopened a legacy runtime path")

    displaced = scratch / "fixture-data"
    data.rename(displaced)
    write_private(data, {"fixture": "closed path"})
    before = file_state(displaced)
    effects_before = effect_count()
    closed, _, _ = attempts()
    if (not all(closed.values()) or not data.is_file() or file_state(displaced) != before
            or effect_count() != effects_before):
        raise RuntimeError(f"Legacy writer bypassed the closed fixture path: {closed}")
    return {
        "version": version,
        "partial_fence_exit_codes": partial,
        "partial_fence_new_records": partial_records,
        "partial_fence_new_receipts": partial_receipts,
        "queued_foreground_after_lock_replacement": queued,
        "queued_foreground_after_runtime_path_closure": closed_runtime_queued,
        "closed_runtime_exit_codes": runtime_closed,
        "candidate_on_versioned_paths": bool(candidate),
        "closed_path_exit_codes": closed,
    }


def probe_application(legacy, candidate, scratch):
    env = {key: value for key, value in os.environ.items() if not key.startswith("DEVTOOLS_")}
    effects = scratch / "effects"
    config = scratch / "devtools.toml"
    child = "from pathlib import Path; import sys; p=Path(sys.argv[1]); p.write_text(p.read_text()+'x' if p.exists() else 'x')"

    def select(name):
        config.write_text(
            f'profile = "{name}"\n[commands.probe]\ninject = false\n'
            f'exec = {json.dumps([sys.executable, "-c", child, str(effects)])}\n', encoding="utf-8")

    def call(binary, *args, error=None):
        result = subprocess.run(
            [str(binary), "--isolated-root", str(scratch), "--format", "json", *args],
            cwd=scratch, env=env, stdin=subprocess.DEVNULL, capture_output=True, timeout=30,
        )
        if error == "legacy_denied":
            if result.returncode == 0:
                raise RuntimeError(f"Legacy writer succeeded after apply: {args!r}")
            return
        if result.returncode != 0:
            envelope = json.loads(result.stderr)
            if error and envelope["error"]["code"] == error:
                return
            raise RuntimeError(f"Unexpected result: {args!r} {result.stderr!r}")
        if error:
            raise RuntimeError(f"Expected {error} for {args!r}")
        if result.stdout:
            return json.loads(result.stdout)["data"]

    select("old")
    call(legacy, "var", "set", "FIXTURE", "--value", "original")
    task = call(legacy, "task", "add", "--title", "Legacy task")["item"]["id"]
    call(legacy, "instance", "name", "local")
    call(legacy, "run", "probe")
    plan = call(candidate, "profile", "rename", "old", "new")
    if not plan["storage_upgrade_required"] or not all(plan[key] for key in ("values", "tasks", "ports")):
        raise RuntimeError(f"Preview missed legacy domains: {plan!r}")
    request = ("profile", "rename", "old", "new", "--apply", plan["digest"], "--request-id", str(uuid.uuid4()))
    root = scratch / "data"
    if os.name != "nt":
        import fcntl
        with (root / "processes" / "publication.lock").open("r+b") as gate:
            fcntl.flock(gate, fcntl.LOCK_EX)
            call(candidate, *request, error="runtime_writer_active")
        if not (root / "processes").is_dir() or (root / ".runtime-v26" / "transition").exists():
            raise RuntimeError("A busy legacy writer started the transition")
    result = call(candidate, *request)
    replay = call(candidate, *request)
    if not result["changed"] or result["replayed"] or not replay["replayed"]:
        raise RuntimeError("Apply or saved result did not complete")
    before = file_state(root)
    for args in (
        ("var", "set", "FIXTURE", "--value", "overwrite"),
        ("task", "add", "--title", "duplicate"),
        ("instance", "name", "duplicate"),
        ("run", "probe"),
        ("process", "start", "probe", "--request-id", str(uuid.uuid4())),
        ("project", "up", "probe", "--request-id", str(uuid.uuid4())),
    ):
        call(legacy, *args, error="legacy_denied")
    if file_state(root) != before or effects.read_text() != "x":
        raise RuntimeError("Legacy CLI changed the published store or ran a child")
    call(candidate, "var", "set", "FIXTURE", "--value", "collision", error="profile_renamed")
    select("new")
    if call(candidate, "task", "show", task)["item"]["id"] != task:
        raise RuntimeError("Task identity changed during apply")
    if call(candidate, "var", "get", "FIXTURE")["value"] != "original":
        raise RuntimeError("Value changed during apply")
    call(candidate, "run", "probe")
    if effects.read_text() != "xx":
        raise RuntimeError("Candidate did not execute exactly one child at the new name")
    return {"legacy_writes_denied": 6, "request_replayed": True, "preserved_task_and_value": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--legacy", type=Path, required=True)
    parser.add_argument("--candidate", type=Path)
    args = parser.parse_args()
    with temporary_fixture(prefix="devtools-rename-legacy-") as scratch:
        candidate = args.candidate.resolve(strict=True) if args.candidate else None
        legacy = args.legacy.resolve(strict=True)
        result = probe(legacy, scratch, candidate)
    if candidate:
        with temporary_fixture(prefix="devtools-rename-apply-") as scratch:
            result["actual_apply"] = probe_application(legacy, candidate, scratch)
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
