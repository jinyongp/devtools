import hashlib
import json
import os
from pathlib import Path
import subprocess
import uuid

home = Path.home()
root = home / "project"
root.mkdir()
env = dict(os.environ, PATH=str(home / ".local/bin") + ":" + os.environ["PATH"])

def execute(*args, expected=0):
    result = subprocess.run(args, cwd=root, env=env, text=True, capture_output=True, timeout=30)
    assert result.returncode == expected, (args, result.returncode, result.stdout, result.stderr)
    payload = result.stdout if expected == 0 else result.stderr
    return json.loads(payload)["data" if expected == 0 else "error"]

def api(*args, **kwargs):
    return execute("devtools", *args, **kwargs)

def key(profile):
    return "p1-" + hashlib.sha256(profile.encode("ascii")).hexdigest()

def private_json(path, body):
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    path.write_text(json.dumps(body))
    path.chmod(0o600)

execute("sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install",
        "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"])
api("init", "--profile", "task-storage")
data = Path(api("project", "inspect")["paths"]["data"])

# Legacy task storage is passive on read and migrates only on the first committed mutation.
legacy_profile = "legacy-task-storage"
legacy_path = data / "tasks" / (legacy_profile.encode().hex() + ".json")
private_json(legacy_path, {
    "version": 1,
    "profile": legacy_profile,
    "events": [],
    "receipts": {},
    "contexts": {},
})
legacy_before = legacy_path.read_bytes()
assert api("task", "list", "--profile", legacy_profile)["items"] == []
assert legacy_path.read_bytes() == legacy_before
assert not (data / "tasks" / (key(legacy_profile) + ".json")).exists()

legacy_request = str(uuid.uuid4())
created = api("task", "add", "--title", "Migrated task", "--profile", legacy_profile,
              "--request-id", legacy_request)
assert api("task", "add", "--title", "Migrated task", "--profile", legacy_profile,
           "--request-id", legacy_request)["replayed"]
canonical = data / "tasks" / (key(legacy_profile) + ".json")
head_path = data / "tasks" / (key(legacy_profile) + ".head.json")
identity = data / "tasks" / ".identity" / (key(legacy_profile) + ".json")
assert json.loads(canonical.read_text())["storage_marker"] == "task-v3"
assert json.loads(legacy_path.read_text())["storage_marker"] == "profile-key-v1"
assert json.loads(identity.read_text())["profile"] == legacy_profile
head = json.loads(head_path.read_text())
generation = data / "tasks" / (key(legacy_profile) + ".generations") / head["generation"]
wal = generation / "wal"
snapshot = generation / "snapshot.json"
assert wal.is_file() and snapshot.is_file()

# Public CLI claim produces a live context after migration. Zero-event
# receipt replay is covered by the package-level v3 storage recovery tests.
task_id = created["item"]["id"]
claim = api("task", "claim", task_id, "--profile", legacy_profile, "--request-id", str(uuid.uuid4()))

# A crash-truncated WAL tail is discarded by the next committed mutation.
with wal.open("ab") as handle:
    handle.write(b"DTV")
    handle.flush()
    os.fsync(handle.fileno())
repair_request = str(uuid.uuid4())
api("task", "add", "--title", "After truncated tail", "--profile", legacy_profile,
    "--request-id", repair_request)
assert api("task", "add", "--title", "After truncated tail", "--profile", legacy_profile,
           "--request-id", repair_request)["replayed"]

# A complete invalid frame/header is corruption, not an incomplete tail.
healthy = wal.read_bytes()
with wal.open("ab") as handle:
    handle.write(b"x" * 64)
    handle.flush()
    os.fsync(handle.fileno())
assert api("task", "list", "--profile", legacy_profile, expected=1)["code"] == "storage_error"
wal.write_bytes(healthy)
wal.chmod(0o600)
assert len(api("task", "list", "--profile", legacy_profile)["items"]) == 2

# The live context remains usable after the storage transitions above.
assert claim["context_valid"]
print("Installed task v3 lazy migration, live-context continuity, WAL-tail repair and corruption rejection verified")
