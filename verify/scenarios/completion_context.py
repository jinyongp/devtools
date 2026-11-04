"""Installed context filtering: actual runs, task state, env slots and shell input."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time

root = Path.home() / "project"
root.mkdir()
environment = dict(os.environ, PATH=str(Path.home() / ".local/bin") + os.pathsep + os.environ["PATH"])
subprocess.run(["sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"]], env=environment, check=True, capture_output=True, timeout=30)
binary = str(Path.home() / ".local/bin/devtools")
marker = root / "MUST_NOT_RUN_READY_PROBE"
config = '''profile="completion-context"
[ports.live]
[ports.ready]
[commands.live]
exec=LIVE_EXEC
serve=["live"]
inject=false
[commands.ready]
exec=LIVE_EXEC
serve=["ready"]
inject=false
[commands.ready.ready]
exec=READY_EXEC
[commands.ended]
exec=ENDED_EXEC
inject=false
'''.replace("LIVE_EXEC", json.dumps([sys.executable, "-c", "import time;time.sleep(120)"]))
config = config.replace("READY_EXEC", json.dumps([sys.executable, "-c", "from pathlib import Path;Path('MUST_NOT_RUN_READY_PROBE').touch()"]))
config = config.replace("ENDED_EXEC", json.dumps([sys.executable, "-c", "print('captured fixture output')"]))
(root / "devtools.toml").write_text(config)


def api(*args):
    result = subprocess.run([binary, "--format", "json", *args], cwd=root, env=environment, capture_output=True, text=True, timeout=20)
    assert result.returncode == 0, (args, result.returncode, result.stderr)
    return json.loads(result.stdout)["data"]


def complete(*words, described=False, directory=root):
    args = [binary, "__complete"] + (["--describe"] if described else [])
    result = subprocess.run(args, input="\0".join(words) + "\0", cwd=directory, env=environment, capture_output=True, text=True, timeout=3)
    assert result.returncode == 0 and result.stderr == "", result
    return [line.split("\t", 1)[0] for line in result.stdout.splitlines()]


def eventually(expected, *words):
    for _ in range(50):
        actual = set(complete(*words))
        if actual == set(expected):
            return
        time.sleep(.04)
    raise AssertionError((words, actual, expected))


foreground = subprocess.Popen([binary, "run", "live"], cwd=root, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
owned = []
try:
    for _ in range(100):
        assert foreground.poll() is None, foreground.communicate()
        rows = api("process", "list", "--mode", "foreground")["items"]
        if rows and rows[0].get("execution_id") and rows[0]["state"] == "running":
            live_id = rows[0]["execution_id"]
            owned.append(live_id)
            break
        time.sleep(.04)
    else:
        raise AssertionError("Foreground registration did not complete")
    ready_id = api("process", "start", "ready")["item"]["id"]
    owned.append(ready_id)
    ended_id = api("process", "start", "ended", "--capture-logs")["item"]["id"]
    owned.append(ended_id)
    for _ in range(100):
        if api("process", "status", ended_id)["item"]["ended_at"]:
            break
        time.sleep(.025)
    else:
        raise AssertionError("One-off did not finish")
    eventually([live_id, ready_id], "process", "stop", "")
    eventually([ready_id], "process", "check", "")
    eventually([ready_id], "process", "wait", "")
    assert complete("process", "logs", "") == [ended_id]
    assert complete("process", "stop", live_id[:12]) == [live_id]
    assert ended_id in complete("process", "restart", "")
    assert ended_id in complete("process", "status", "")
    eventually(["live", "ready"], "project", "down", "")
    assert complete("project", "logs", "") == []
    other = Path.home() / "other-project"
    other.mkdir()
    (other / "devtools.toml").write_text(config)
    assert complete("project", "down", "", directory=other) == []
    api("process", "stop", ready_id)
    eventually([live_id], "process", "stop", "")
    assert not marker.exists(), "Tab executed the readiness probe"

    api("var", "set", "COMMON", "--value", "public fixture")
    api("env", "create", "local")
    api("var", "set", "LOCAL", "--env", "local", "--value", "public fixture")
    assert set(complete("var", "get", "--env", "local", "")) == {"COMMON", "LOCAL"}
    assert complete("var", "unset", "--env", "local", "") == ["LOCAL"]
    assert "--profile" not in complete("var", "set", "KEY", "--profile", "completion-context", "--pro", described=True)
    assert "--value" not in complete("var", "set", "KEY", "--stdin", "--val", described=True)
    assert "--value" in complete("var", "set", "KEY", "--stdin=false", "--val", described=True)
    assert "--no-proxy" not in complete("run", "--proxy=false", "--no-pro", described=True)

    claimed = api("task", "add", "--title", "Candidate fixture")
    task_id = claimed["item"]["id"]
    assert task_id in complete("task", "claim", "")
    claim = api("task", "claim", task_id)
    run_id = claim["run"]["id"]
    assert task_id not in complete("task", "claim", "")
    assert task_id in complete("task", "unclaim", "")
    assert run_id in complete("task", "release", "")
    api("task", "release", run_id, "--context", claim["context"])
    assert run_id not in complete("task", "release", "")
    assert run_id in complete("task", "checkpoint", "list", "")
finally:
    for execution_id in owned:
        api("process", "stop", execution_id)
    if foreground.poll() is None:
        foreground.terminate()
    foreground.communicate(timeout=10)
print("Installed completion filtered control/readiness/logs, project scope, env slots, task lifecycle and partial option contracts")
