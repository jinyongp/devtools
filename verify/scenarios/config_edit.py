"""Exercise installed protocol-11 editing, migration and scoped diagnostics."""

import json
import os
from pathlib import Path
import subprocess

home = Path.home()
environment = dict(os.environ, PATH=str(home / ".local/bin") + ":" + os.environ["PATH"])
project = home / "project"
project.mkdir()


def execute(argv, *, body=None, expected=0):
    result = subprocess.run(argv, cwd=project, env=environment, input=body, text=True,
                            capture_output=True, timeout=20)
    assert result.returncode == expected, (argv, result.returncode, result.stdout, result.stderr)
    return result


execute(["sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version", "0.0.0-test.1",
         "--source", os.environ["DEVTOOLS_TEST_RELEASES"]])


def api(*argv, body=None, expected=0):
    if body is not None and not isinstance(body, str):
        body = json.dumps(body)
    result = execute(["devtools", *argv], body=body, expected=expected)
    response = json.loads(result.stdout or result.stderr)
    assert response["ok"] == (expected == 0), response
    return response.get("data") if expected == 0 else response["error"]


assert api("version")["protocol_version"] == 20
api("init", "--profile", "sample")
path = project / "devtools.toml"
initial = api("config", "inspect")
revision = initial["revision"]
before = path.read_bytes()
edit = {"operations": [
    {"op": "command.add", "name": "dev.웹", "value": {"exec": ["missing-app"], "requires": ["web"]}},
    {"op": "preset.add", "name": "web", "value": {"tools": ["missing-runtime", "missing-package-manager"]}},
    {"op": "tool.add", "name": "missing-runtime", "value": {}},
    {"op": "tool.add", "name": "missing-package-manager", "value": {}},
]}
preview = api("config", "edit", "--revision", revision, "--stdin", "--dry-run", body=edit)
assert preview["would_change"] and not preview["changed"]
assert path.read_bytes() == before
applied = api("config", "edit", "--revision", revision, "--stdin", body=edit)
assert applied["candidate_digest"] == preview["candidate_digest"] and applied["changed"]
assert api("config", "check")["valid"]
explained = api("command", "explain", "dev.웹")
assert explained["item"]["name"] == "dev.웹", explained
assert api("config", "tool", "update", "missing-runtime", "--revision", revision,
           "--stdin", body={"version": "1.2.3"}, expected=3)["code"] == "config_revision_conflict"

report = api("doctor")
assert not report["ready"] and report["selection"] == ["dev.웹"]
checks = report["commands"][0]["checks"]
assert {"tool:missing-runtime", "tool:missing-package-manager", "command_executable"} <= {c["id"] for c in checks if c["status"] == "fail"}
failure = api("run", "dev.웹", expected=127)
assert failure["code"] == "executable_not_found" and "missing-app" in failure["message"], failure
assert len([c for c in failure["details"]["checks"] if c["status"] == "fail"]) >= 3
failed_check = api("doctor", "--check", expected=3)
assert failed_check["details"]["report"]["selection"] == ["dev.웹"]
assert api("dotor", expected=2)["details"]["facts"]["suggestions"] == ["doctor"]

revision = api("config", "inspect")["revision"]
saved = path.read_bytes()
invalid = {"operations": [{"op": "tool.remove", "name": "missing-runtime"}]}
api("config", "edit", "--revision", revision, "--stdin", body=invalid, expected=3)
assert path.read_bytes() == saved
for body in ('{"version":"PRIVATE-CANARY","version":"1.2.3"}', '{"unexpected":"PRIVATE-CANARY"}'):
    failure = api("config", "tool", "add", "go", "--stdin", body=body, expected=2)
    assert "PRIVATE-CANARY" not in json.dumps(failure)
    assert failure["details"]["sources"]
assert path.read_bytes() == saved

for requirement in ("26", "26.12", "^26.2.0", "~26.12.3", ">=26 <27", "26 || 28", "26.2 - 28.1"):
    revision = api("config", "inspect")["revision"]
    api("config", "tool", "update", "missing-runtime", "--revision", revision,
        "--stdin", body={"version": requirement})
    assert requirement in path.read_text()
    api("config", "fmt")
    assert requirement in path.read_text() and api("config", "check")["valid"]

revision = api("config", "inspect")["revision"]
saved = path.read_bytes()
for requirement in ("", " ", "26 | 28", "^bad", "26.12.3_foo"):
    api("config", "tool", "update", "missing-runtime", "--revision", revision,
        "--stdin", body={"version": requirement}, expected=2)
    assert path.read_bytes() == saved

conflict = {"operations": [
    {"op": "tool.update", "name": "missing-runtime", "value": {"version": "26"}},
    {"op": "tool.add", "name": "alias", "value": {"executable": "missing-runtime", "version": "28"}},
    {"op": "preset.update", "name": "web", "value": {"tools": ["missing-runtime", "alias"]}},
]}
api("config", "edit", "--revision", revision, "--stdin", body=conflict, expected=3)
assert path.read_bytes() == saved and api("config", "inspect")["revision"] == revision

path.write_text("profile='sample'\n# tools\n[requirements.tools.node] # runtime\nversion='1.2 || 3' # pinned\n[commands.check]\nexec=['node', 'check']\n")
saved = path.read_bytes()
migration = api("config", "migrate")
assert migration["equivalent"] and path.read_bytes() == saved
api("config", "migrate", "--revision", migration["revision"], "--apply", migration["apply_digest"])
assert api("config", "inspect")["format"] == "registry"
assert all(comment in path.read_text() for comment in ("# tools", "# runtime", "# pinned"))
assert "1.2 || 3" in path.read_text()
api("config", "fmt")
formatted = path.read_bytes()
assert not api("config", "fmt")["changed"] and path.read_bytes() == formatted
assert api("config", "check")["valid"]
print("Installed config editing, stale guards, comment migration and complete preflight verified")
