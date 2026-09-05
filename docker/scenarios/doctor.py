"""Diagnose installed project prerequisites without exposing values or probes."""
import json
import os
from pathlib import Path
import subprocess

home = Path.home()
env = dict(os.environ, PATH=str(home / ".local/bin") + ":" + os.environ["PATH"])
project = home / "project"
project.mkdir()
canary = "doctor-canary-secret"

def execute(args, input=None, expected=0, cwd=project):
    result = subprocess.run(args, cwd=cwd, env=env, input=input, text=True,
                            capture_output=True, timeout=20)
    assert result.returncode == expected, (args, result.returncode, result.stderr)
    assert canary not in result.stdout + result.stderr, "Diagnostic leaked a value or probe output"
    return result

execute(["sh", "/opt/devtools-install.sh", "install", "--version", "0.0.0-test.1", "--source", "/opt/releases"])
def api(*args, **kwargs):
    result = execute(["devtools", *args], **kwargs)
    return json.loads(result.stdout or result.stderr)

paths = api("project", "inspect", "--profile", "fixture")["data"]["paths"]
assert api("doctor", "--profile", "fixture")["data"]["ready"]
assert not (Path(paths["data"]) / "profiles").exists(), "Diagnosis initialized storage"
assert not api("doctor", "--dir", "missing")["data"]["ready"]

config = '''profile = "fixture"
[requirements]
vars = ["PORT"]
[requirements.tools.fixture]
executable = "./fixture-tool"
version = "1.2.3"
[requirements.tools.devtools]
version = "0.0.0-test.1"
version_args = ["version"]
[commands.check]
exec = ["/bin/sh", "-c", "test -n \\"$TOKEN\\" && printf completed > marker"]
inject = true
env = "dev"
[commands.check.requirements]
secs = ["TOKEN"]
[commands.plain]
exec = ["/bin/true"]
'''
(project / "devtools.toml").write_text(config)
tool = project / "fixture-tool"
tool.write_text(f"#!/bin/sh\necho 'fixture 1.2.3'\necho '{canary}' >&2\n")
tool.chmod(0o700)
assert not api("doctor", "check")["data"]["ready"]
api("env", "create", "dev")
api("var", "set", "PORT", "--value", "3000")
assert not api("doctor", "check")["data"]["ready"]
failure = api("run", "check", expected=3)
assert failure["error"]["code"] == "requirements_failed"
assert not (project / "marker").exists()
api("sec", "set", "TOKEN", "--env", "dev", "--stdin", input=canary)
nested = project / "nested"
nested.mkdir()
report = api("doctor", "check", cwd=nested)["data"]
assert report["ready"] and report["env"] == "dev"
assert report["directory"] == str(project)
assert all(c["status"] == "pass" for c in report["checks"])
execute(["devtools", "run", "check"], cwd=nested)
assert (project / "marker").read_text() == "completed"
assert not api("doctor", "plain")["data"]["ready"]
api("run", "plain", expected=3)
assert not api("doctor", "check", "--env", "missing")["data"]["ready"]

# Exact matching, missing executables, and corrupted storage stay actionable.
(project / "marker").unlink()
tool.write_text(f"#!/bin/sh\necho 'fixture 1.2.30 {canary}'\n")
assert not api("doctor", "check")["data"]["ready"]
api("run", "check", expected=3)
assert not (project / "marker").exists()
tool.unlink()
assert not api("doctor")["data"]["ready"]
profile_file = next((Path(paths["data"]) / "profiles").glob("*.json"))
profile_file.write_text(canary)
report = api("doctor")["data"]
assert not report["ready"]
assert any(c["id"] == "value_storage" and c["status"] == "fail" for c in report["checks"])
print("Installed doctor metadata, exact versions, safe diagnostics, and run preflight verified")
