import fcntl
import json
import os
from pathlib import Path
import signal
import subprocess
import time

home = Path.home()
root = home / "project"
root.mkdir()
env = dict(os.environ, PATH=str(home / ".local/bin") + ":" + os.environ["PATH"])

def execute(*args, expected=0):
    result = subprocess.run(args, cwd=root, env=env, text=True, capture_output=True, timeout=20)
    assert result.returncode == expected, (args, result.returncode, result.stdout, result.stderr)
    payload = result.stdout if expected == 0 else result.stderr
    return json.loads(payload)["data" if expected == 0 else "error"]

execute("sh", os.environ["DEVTOOLS_TEST_INSTALLER"], "install",
        "--version", "0.0.0-test.1", "--source", os.environ["DEVTOOLS_TEST_RELEASES"])
execute("devtools", "init", "--profile", "probe")
execute("devtools", "var", "set", "READY", "--value", "yes")

data_root = Path(os.environ["XDG_DATA_HOME"]) / "devtools"
lock_dir = data_root / ".maintenance"
lock_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
lock_path = lock_dir / "lock"
fd = os.open(lock_path, os.O_CREAT | os.O_RDWR, 0o600)
lock = os.fdopen(fd, "r+")
fcntl.flock(lock, fcntl.LOCK_EX)

def interrupt_while_locked(*args):
    process = subprocess.Popen(
        ["devtools", *args],
        cwd=root,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    time.sleep(0.12)
    assert process.poll() is None, (args, "request crossed maintenance lock")
    process.send_signal(signal.SIGINT)
    stdout, stderr = process.communicate(timeout=2)
    assert process.returncode == 130, (args, process.returncode, stdout, stderr)
    error = json.loads(stderr)["error"]
    assert error["code"] == "canceled", (args, error)
    return error

try:
    interrupt_while_locked("var", "list")
    interrupt_while_locked("task", "list")
finally:
    fcntl.flock(lock, fcntl.LOCK_UN)
    lock.close()

assert execute("devtools", "var", "list")["profile"] == "probe"
assert execute("devtools", "task", "list")["profile"] == "probe"
print("Installed maintenance-lock cancellation for values and task reads verified")
