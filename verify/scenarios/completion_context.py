"""Installed context filtering: actual runs, task state, env slots and shell input."""
import gzip
import hashlib
import json
import math
import os
from pathlib import Path
import select
import shutil
import subprocess
import sys
import time

root = Path.home() / "project"
root.mkdir()
environment = dict(os.environ, PATH=str(Path.home() / ".local/bin") + os.pathsep + os.environ["PATH"])
subprocess.run(["sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install", "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"]], env=environment, check=True, capture_output=True, timeout=30)
binary = str(Path.home() / ".local/bin/devtools")
# The installer preserves an existing dvt anywhere on PATH. Exercise the
# candidate alias in the owned fixture even when the runner has an older dvt.
alias = Path(binary).with_name("dvt")
if not alias.is_symlink() and not alias.exists():
    alias.symlink_to("devtools")
assert shutil.which("dvt", path=environment["PATH"]) == str(alias)
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
[commands.collision_a]
exec=LIVE_EXEC
inject=false
[commands.collision_b]
exec=LIVE_EXEC
inject=false
'''.replace("LIVE_EXEC", json.dumps([sys.executable, "-c", "import time;time.sleep(240)"]))
config = config.replace("READY_EXEC", json.dumps([sys.executable, "-c", "from pathlib import Path;Path('MUST_NOT_RUN_READY_PROBE').touch()"]))
config = config.replace("ENDED_EXEC", json.dumps([sys.executable, "-c", "print('captured fixture output')"]))
(root / "devtools.toml").write_text(config)


def cli(*args, timeout=20):
    return subprocess.run([binary, "--format", "json", *args], cwd=root, env=environment, capture_output=True, text=True, timeout=timeout)


def api(*args, timeout=20):
    result = cli(*args, timeout=timeout)
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


def seed_ended_history(template_id, count):
    data = (Path.home() / "Library/Application Support/devtools/data" if sys.platform == "darwin"
            else Path(environment["XDG_DATA_HOME"]) / "devtools")
    processes = data / "processes"
    template = json.loads((processes / template_id / "record.json").read_bytes())["metadata"]
    # Current-format records with independently encoded and verified bodies.
    for index in range(count):
        execution_id = f"ee{index:06x}-0000-4000-8000-{index:012x}"
        body = json.dumps(dict(template, id=execution_id), separators=(",", ":")).encode()
        digest = hashlib.sha256(b"process_record\0" + body).hexdigest()
        artifact = data / "retained-bodies" / digest
        artifact.touch(mode=0o600)
        artifact.write_bytes(b"DTRBODY1" + gzip.compress(body, compresslevel=1))
        folder = processes / execution_id
        folder.mkdir(mode=0o700)
        metadata = folder / "record.json"
        metadata.touch(mode=0o600)
        metadata.write_text(json.dumps({
            "storage_version": 2,
            "metadata": json.loads(body),
            "body": {"version": 1, "kind": "process_record", "digest": digest, "bytes": len(body)},
        }, separators=(",", ":")))


def measure_zsh_completion(execution_id):
    shell = shutil.which("zsh")
    assert shell, "installed Unix completion verification requires zsh"
    master, slave = os.openpty()
    try:
        process = subprocess.Popen(
            [shell, "-dfi"], cwd=root, env=dict(environment, TERM="xterm", ZDOTDIR=str(root)),
            stdin=slave, stdout=slave, stderr=slave, start_new_session=True,
        )
    except BaseException:
        os.close(master)
        raise
    finally:
        os.close(slave)

    def read_until(marker):
        output = b""
        deadline = time.monotonic() + 5
        while marker not in output and time.monotonic() < deadline:
            if select.select([master], [], [], .1)[0]:
                output += os.read(master, 8192)
        assert marker in output, (marker, output[-3000:])

    try:
        setup = r"""
PS1='DVT> '
autoload -Uz compinit
compinit -D -i
source <(command dvt completion zsh)
[[ "${_comps[dvt]-}" == _devtools ]] || { print -u2 'dvt completion was not registered'; exit 1; }
bindkey '^I' complete-word
report-buffer() {
  print -r -- $'\nDVT_BUFFER:'"${BUFFER%% }"
  BUFFER=''
  CURSOR=0
  zle reset-prompt
}
zle -N report-buffer
bindkey '^G' report-buffer
print -r -- $'\nDVT_READY'
"""
        os.write(master, setup.encode())
        read_until(b"\r\nDVT_READY\r\n")
        samples = []
        for index in range(20):
            prefix = "" if index % 2 == 0 else execution_id[:4]
            started = time.monotonic()
            # Ctrl-G is a single ZLE binding. Ctrl-X would include zsh's key
            # sequence timeout in the measurement after completion had finished.
            os.write(master, ("dvt process stop " + prefix + "\t\x07").encode())
            read_until(("DVT_BUFFER:dvt process stop " + execution_id + "\r\n").encode())
            samples.append((time.monotonic() - started) * 1000)
        p95 = sorted(samples)[math.ceil(.95 * len(samples)) - 1]
        print(f"10,000 ended records, real zsh Tab: p95={p95:.1f}ms; samples={','.join(f'{value:.1f}' for value in samples)}", flush=True)
        assert p95 <= 150, f"installed zsh Tab p95 {p95:.1f}ms exceeds 150ms"
    finally:
        os.close(master)
        if process.poll() is None:
            process.kill()
            process.wait(timeout=5)


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
    eventually(["ready"], "project", "restart", "rea")
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

    first = "a4531000-0000-4000-8000-000000000001"
    second = "a453b000-0000-4000-8000-000000000002"
    stop_request = "a453c000-0000-4000-8000-000000000003"
    stop = ("process", "stop", "a453", "--request-id", stop_request)
    owned.append(api("process", "start", "collision_a", "--request-id", first)["item"]["id"])
    assert owned[-1] == first
    stopped = api(*stop)
    assert stopped["changed"] and stopped["item"]["id"] == first
    owned.append(api("process", "start", "collision_b", "--request-id", second)["item"]["id"])
    assert owned[-1] == second
    conflict = cli("process", "stop", "a453")
    assert conflict.returncode == 3 and not conflict.stdout, conflict
    error = json.loads(conflict.stderr)["error"]
    assert error["code"] == "ambiguous_id", error
    assert error["details"]["facts"]["suggestions"] == ["a4531", "a453b"], error
    assert "collision_a" in error["message"] and "collision_b" in error["message"], error
    replay = api(*stop)
    assert replay["replayed"] and replay["item"]["id"] == first
    assert api("process", "status", second)["item"]["control"]["stop"]
    api("process", "stop", second)

    seed_ended_history(ended_id, 10_000)
    api("doctor", "--storage", "--fix", "--timeout", "90s", timeout=100)
    assert complete("process", "stop", "") == [live_id]
    measure_zsh_completion(live_id)
finally:
    for execution_id in owned:
        api("process", "stop", execution_id)
    if foreground.poll() is None:
        foreground.terminate()
    foreground.communicate(timeout=10)
print("Installed completion filtered control/readiness/logs, project scope, env slots, task lifecycle and partial option contracts")
