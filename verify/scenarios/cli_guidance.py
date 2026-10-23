"""Installed protocol-13 guidance follows the observed target and failure phase."""
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid

home = Path.home()
project = home / "project"
project.mkdir()
env = dict(os.environ, PATH=str(home / ".local/bin") + os.pathsep + os.environ["PATH"])
subprocess.run(["sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version",
                "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"]],
               cwd=project, env=env, capture_output=True, check=True, timeout=30)
binary = str(home / ".local/bin/devtools")
canary = "synthetic-private-guidance-canary"
passphrase = "synthetic-guidance-passphrase"


def execute(args, *, body=None, expected=0, cwd=project):
    result = subprocess.run([binary, *args], cwd=cwd, env=env, input=body, text=True,
                            capture_output=True, timeout=25)
    assert result.returncode == expected, (args[:3], result.returncode, result.stderr)
    assert canary not in result.stdout + result.stderr
    assert passphrase not in result.stdout + result.stderr
    return result


def api(*args, body=None, expected=0, cwd=project):
    result = execute(args, body=body, expected=expected, cwd=cwd)
    response = json.loads(result.stdout or result.stderr)
    assert response["schema_version"] == 1 and response["ok"] == (expected == 0)
    return response["data"] if expected == 0 else response["error"]


def checks(failure):
    return {c["id"]: c for c in failure["details"]["checks"]}


assert api("version")["protocol_version"] == 13
config = project / "devtools.toml"
config.write_text("profile='guidance'\n[commands.missing]\nexec=['__missing_guidance__']\n")
root_a, root_b = home / "A", home / "B"
env["DEVTOOLS_ISOLATED_ROOT"] = str(root_a)
api("vault", "init", "--backend", "passphrase", "--passphrase-stdin", body=passphrase + "\n")

# Explicit B must survive an A environment, a relative flag and a changed cwd.
failure = api("sec", "set", "TOKEN", "--stdin", "--isolated-root", "../B",
              body=canary, expected=3)
remedy = failure["details"]["remedies"][0]
assert remedy["effect"] == "read" and not remedy["required_inputs"]
assert str(root_b) in remedy["argv"]
status = api(*remedy["argv"][1:], cwd=home)["item"]
assert status["state"] == "uninitialized"
failure = api("run", "--isolated-root", str(root_b), "missing", expected=127)
assert str(root_b) in failure["details"]["remedies"][0]["argv"]
assert "--isolated-root" in json.dumps(api("command", "explain", "missing", "--env", "", "--isolated-root", str(root_b)))

# Direct diagnostics identify the actual argv token, without a fictitious config path.
api("vault", "init", "--isolated-root", str(root_b), "--backend", "passphrase", "--passphrase-stdin", body=passphrase + "\n")
direct = ["--isolated-root", str(root_b), "command", "run", "--", "__missing_guidance__"]
failure = api(*direct, expected=127)
assert failure["details"]["target"]["kind"] == "direct"
assert checks(failure)["command_executable"]["sources"] == [
    {"kind": "argv", "path": ["child_args", "0"], "offset": len(direct) - 1}]
assert all("doctor" not in r["argv"] for r in failure["details"]["remedies"])

env["DEVTOOLS_ISOLATED_ROOT"] = str(root_b)
failure = api("run", "--profile", "invalid-new", "--env", "missing", "--", sys.executable, expected=3)
assert failure["details"]["effects"] == "none"
assert "invalid-new" not in {p["profile"] for p in api("profile", "list")["items"]}
failure = api("run", "--profile", "valid-new", "--", "__missing_guidance__", expected=127)
assert failure["details"]["effects"] == "partial"

marker = project / "child-started"
config.write_text("profile='guidance'\n[commands.need]\nexec=" + json.dumps(
    [sys.executable, "-c", "from pathlib import Path;Path('child-started').touch()"])
    + "\ninject=true\n[commands.need.requirements]\nsecs=['TOKEN']\n")
api("var", "set", "TOKEN", "--value", "public")
failure = api("run", "need", expected=3)
assert checks(failure)["sec:TOKEN"]["reason"] == "kind_mismatch"
assert checks(failure)["sec:TOKEN"]["facts"] == {"expected_kind": "secret", "actual_kind": "variable"}
assert all(not r["argv"] for r in checks(failure)["sec:TOKEN"]["remedies"])
assert not marker.exists()

# Revisions and uncertain responses require different investigations.
revision = api("values", "revision")["revision"]
request = str(uuid.uuid4())
argv = ["var", "set", "MODE", "--value", "first", "--revision", revision, "--request-id", request]
api(*argv)
assert api(*argv)["replayed"] is True
failure = api("var", "set", "MODE", "--value", "other", "--revision", revision,
              "--request-id", request, expected=3)
assert failure["details"]["retry"] == "inspect_first"
assert api(*failure["details"]["remedies"][0]["argv"][1:])["item"]["state"] == "completed"
failure = api("var", "set", "MODE", "--value", "other", "--revision", revision,
              "--request-id", str(uuid.uuid4()), expected=3)
assert failure["details"]["retry"] == "after_refresh"
api(*failure["details"]["remedies"][0]["argv"][1:])

for args in (["help", "missing"], ["help", "values", "missing"]):
    failure = api(*args, expected=2)
    execute(failure["details"]["remedies"][0]["argv"][1:])
api("values", "execution", "status", expected=2)
api("values", "execution", "status", "0" * 32, "--request-id", str(uuid.uuid4()), expected=2)
assert api("schema", "secret", "generate")["workflow"]

dotenv = project / "input.env"
dotenv.write_text("TOKEN=" + canary + "\n")
failure = api("import", "--file", str(dotenv), expected=3)
assert failure["details"]["facts"]["import_items"] == [
    {"key": "TOKEN", "kind": "variable", "action": "conflict"}]
dotenv.write_text("OK=public\nTOKEN=\"" + canary + "\n")
failure = api("import", "--file", str(dotenv), expected=2)
assert failure["details"]["sources"] == [{"kind": "dotenv", "path": [], "line": 2}]

valid = config.read_text()
config.write_text(valid.replace("inject=true", "inject='" + canary + "'"))
failure = api("config", "check", expected=3)
assert any(c["facts"].get("expected_type") == "boolean" and c["facts"].get("observed_type") == "string"
           for c in failure["details"]["causes"])
assert all(r["argv"][:3] != ["devtools", "config", "check"] for r in failure["details"]["remedies"])
config.write_text(valid.replace("secs=['TOKEN']", "secs=[]"))

# The installed managed child must carry the same safe limit facts as foreground.
api("sec", "set", "LARGE", "--stdin", body="x" * 66000)
for args in (["run", "need"], ["process", "start", "need", "--request-id", str(uuid.uuid4())]):
    failure = api(*args, expected=3)
    facts = failure["details"]["facts"]
    assert failure["code"] == "redaction_unavailable"
    assert {k: facts[k] for k in ("limit_kind", "observed", "limit")} == {
        "limit_kind": "pattern_bytes", "observed": 66000, "limit": 65536}
    assert all("raw" not in r["argv"] for r in failure["details"]["remedies"])
assert not marker.exists()
api("vault", "lock")
failure = api("run", "need", expected=3)
assert failure["details"]["retry"] == "after_dependency_change"
unlock = failure["details"]["remedies"][0]
assert "--passphrase-stdin" in unlock["argv"] and unlock["required_inputs"] == ["stdin"]
api(*unlock["argv"][1:], body=passphrase + "\n")
api("vault", "lock")
api("vault", "lock", "--isolated-root", str(root_a))
print("Installed guidance: roots, direct sources, effects, prerequisite kinds, recovery, help, imports, config and launcher limits passed")
