"""Installed legacy/canonical profile storage and identifier-length boundaries."""
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
    result = subprocess.run(args, cwd=root, env=env, text=True, capture_output=True, timeout=25)
    assert result.returncode == expected, (args, result.returncode, result.stdout, result.stderr)
    return json.loads(result.stdout if expected == 0 else result.stderr)["data" if expected == 0 else "error"]


def api(*args, **kwargs):
    return execute("devtools", *args, **kwargs)


def key(profile):
    return "p1-" + hashlib.sha256(profile.encode("ascii")).hexdigest()


def private_json(path, body):
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    path.write_text(json.dumps(body))
    path.chmod(0o600)


execute("sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"])
api("init", "--profile", "migration-probe")
data = Path(api("project", "inspect")["paths"]["data"])

legacy_profile = "LegacyProfile"
legacy_values = data / "profiles" / (legacy_profile.encode().hex() + ".json")
legacy_tasks = data / "tasks" / (legacy_profile.encode().hex() + ".json")
private_json(legacy_values, {"version": 1, "profile": legacy_profile, "envs": {}, "keys": {}})
private_json(legacy_tasks, {"version": 1, "profile": legacy_profile, "events": [], "receipts": {}, "contexts": {}})
values_before, tasks_before = legacy_values.read_bytes(), legacy_tasks.read_bytes()
api("var", "list", "--profile", legacy_profile)
api("task", "list", "--profile", legacy_profile)
assert legacy_values.read_bytes() == values_before
assert legacy_tasks.read_bytes() == tasks_before
assert not (data / "profiles" / (key(legacy_profile) + ".json")).exists()
assert not (data / "tasks" / (key(legacy_profile) + ".json")).exists()

api("var", "set", "READY", "--value", "migrated", "--profile", legacy_profile)
request_id = str(uuid.uuid4())
first = api("task", "add", "--title", "Legacy migration", "--profile", legacy_profile, "--request-id", request_id)
replay = api("task", "add", "--title", "Legacy migration", "--profile", legacy_profile, "--request-id", request_id)
assert replay["replayed"] and replay["revision"] == first["revision"]
for domain, logical in [("profiles", "values"), ("tasks", "tasks")]:
    canonical = data / domain / (key(legacy_profile) + ".json")
    identity = data / domain / ".identity" / (key(legacy_profile) + ".json")
    legacy = data / domain / (legacy_profile.encode().hex() + ".json")
    assert json.loads(canonical.read_text())["profile"] == legacy_profile
    assert json.loads(identity.read_text())["domain"] == logical
    assert json.loads(legacy.read_text())["storage_marker"] == "profile-key-v1"

profiles = ["A" * n for n in [122, 123, 125, 126, 128]] + ["a" * 128]
for profile in profiles:
    api("var", "set", "READY", "--value", str(len(profile)), "--profile", profile)
    api("task", "add", "--title", "Long profile", "--profile", profile, "--request-id", str(uuid.uuid4()))
    assert api("var", "list", "--profile", profile)["profile"] == profile
    assert api("task", "list", "--profile", profile)["profile"] == profile
    for domain in ["profiles", "tasks"]:
        canonical = data / domain / (key(profile) + ".json")
        identity = data / domain / ".identity" / (key(profile) + ".json")
        assert canonical.exists() and identity.exists()
        assert len(canonical.name + ".lock") < 255
        if len(profile) <= 125:
            legacy = data / domain / (profile.encode().hex() + ".json")
            assert json.loads(legacy.read_text())["storage_marker"] == "profile-key-v1"

names = [item["profile"] for item in api("profile", "list")["items"]]
assert all(name in names for name in profiles + [legacy_profile])
assert names.count(legacy_profile) == 1

# A second real legacy state cannot coexist with its canonical counterpart.
marker = legacy_values.read_bytes()
try:
    legacy_values.write_bytes(values_before)
    assert api("var", "list", "--profile", legacy_profile, expected=1)["code"] == "storage_error"
finally:
    legacy_values.write_bytes(marker)
assert api("var", "list", "--profile", legacy_profile)["profile"] == legacy_profile
print("Installed 128-character profiles, legacy lazy migration, replay, identity catalog and split-brain rejection verified")
