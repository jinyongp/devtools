"""Installed opt-in mutation guards, generation and lost-response recovery."""
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
secret = "synthetic-installed-guarded-secret\r\nsecond\n"
passphrase = "synthetic-installed-mutations-passphrase"


def api(*args, input=None, error=None):
    result = subprocess.run([binary, *args], cwd=project, env=env, input=input,
                            text=True, capture_output=True, timeout=20)
    assert secret not in result.stdout + result.stderr
    assert secret.split("\r", 1)[0] not in result.stdout + result.stderr
    assert passphrase not in result.stdout + result.stderr
    assert not (result.stdout and result.stderr), "mixed response streams"
    raw = result.stderr if error else result.stdout
    response = json.loads(raw)
    assert response["schema_version"] == 1 and response["ok"] == (error is None)
    if error:
        assert result.returncode != 0 and response["error"]["code"] == error
        return response["error"]
    assert result.returncode == 0, (args[:2], result.returncode)
    return response["data"]


def revision():
    data = api("values", "revision")
    assert set(data) == {"profile", "env", "revision", "integrity_verified"}
    assert data["integrity_verified"] is False
    return data["revision"]


def guard(token=None, request=None):
    return ["--revision", token or revision(), "--request-id", request or str(uuid.uuid4())]


assert api("version")["protocol_version"] == 10
api("init", "--profile", "mutation-fixture")
assert api("var", "set", "URL", "--value", "before") == {"profile": "mutation-fixture", "changed": True}
token, request = revision(), str(uuid.uuid4())
lost = ["var", "set", "URL", "--value", "committed", *guard(token, request)]
# The client response is discarded after actual commit. Recover with the saved
# request input even after another legacy writer advances the common revision.
result = subprocess.run([binary, *lost], cwd=project, env=env,
                        stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=20)
assert result.returncode == 0 and not result.stderr
api("var", "set", "URL", "--value", "later")
replay = api(*lost)
assert replay["changed"] is True and replay["replayed"] is True
assert api("var", "get", "URL")["value"] == "later"
saved = api("values", "request", "show", request)["item"]
assert saved["state"] == "completed" and saved["result"]["replayed"] is False
assert saved["result"]["revision"] == replay["revision"]
api("var", "set", "URL", "--value", "different", *guard(token, request), error="request_conflict")
api("var", "set", "URL", "--value", "stale", *guard(token), error="revision_conflict")
api("values", "request", "show", str(uuid.uuid4()), error="request_not_found")

api("vault", "init", "--backend", "passphrase", "--passphrase-stdin", input=passphrase + "\n")
api("env", "create", "local", *guard())
set_request = str(uuid.uuid4())
set_args = ["secret", "set", "INPUT", "--env", "local", "--stdin", "--create-only", *guard(request=set_request)]
first = api(*set_args, input=secret)
api("secret", "unset", "INPUT", "--env", "local", *guard())
api("env", "remove", "local", *guard())
assert api(*set_args, input=secret)["revision"] == first["revision"]

generate_request = str(uuid.uuid4())
generate = ["secret", "generate", "TOKEN", *guard(request=generate_request)]
generated = api(*generate)
assert generated["changed"] is True and set(generated) == {"profile", "changed", "revision", "replayed"}
assert api(*generate)["replayed"] is True
consumer = "import os,base64,re; v=os.environ['TOKEN']; assert re.fullmatch(r'[A-Za-z0-9_-]{43}',v); assert len(base64.urlsafe_b64decode(v+'='))==32; print('generated-ok',end='')"
result = subprocess.run([binary, "run", "--", sys.executable, "-c", consumer],
                        cwd=project, env=env, capture_output=True, timeout=20)
assert result.returncode == 0 and result.stdout == b"generated-ok" and not result.stderr
api("secret", "generate", "TOKEN", *guard(), error="value_exists")
api("secret", "generate", "TOKEN", "--replace", *guard())

before = api("vault", "status")["item"]
assert api("values", "request", "show", generate_request)["item"]["state"] == "completed"
after = api("vault", "status")["item"]
assert before["idle_expires_at"] == after["idle_expires_at"]
api("vault", "lock")
api("values", "request", "show", generate_request, error="vault_locked")
assert api("values", "revision")["integrity_verified"] is False
print("Installed guarded mutations, lost-response replay, generation, child delivery and passive lookup passed")
