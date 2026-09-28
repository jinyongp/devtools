import errno
import os
import sys
from pathlib import Path

scenario_directory = os.path.dirname(os.path.abspath(__file__))
sys.path = [entry for entry in sys.path if os.path.abspath(entry or os.getcwd()) != scenario_directory]

import pty
import select
import subprocess
import termios
import time

root = Path.home() / "project"
root.mkdir()
env = dict(os.environ, PATH=str(Path.home() / ".local/bin") + ":" + os.environ["PATH"])

subprocess.run(
    [
        "sh",
        os.environ["DEVTOOLS_TEST_INSTALLER"],
        "install",
        "--version",
        "0.0.0-test.1",
        "--source",
        os.environ["DEVTOOLS_TEST_RELEASES"],
    ],
    cwd=root,
    env=env,
    check=True,
)

(root / "devtools.toml").write_text(
    """profile="tty"

[commands.read]
exec=["python3","read.py"]

[commands.wait]
exec=["sh","wait.sh"]

[commands.bg]
exec=["python3","bg.py"]

[commands.missing]
exec=["./definitely-missing"]
"""
)
(root / "read.py").write_text(
    """print("READ_READY", flush=True)
value=input()
print("RESULT:"+value, flush=True)
"""
)
(root / "wait.sh").write_text(
    """trap 'exit 42' INT
printf 'WAIT_READY\n'
while :; do sleep 1; done
"""
)
(root / "bg.py").write_text(
    """from pathlib import Path
import time

release = Path("bg.release")
release.unlink(missing_ok=True)
print("BG_READY", flush=True)
while not release.exists():
    time.sleep(0.02)
print("BG_DONE", flush=True)
"""
)

prompt = b"__DT_TTY_PROMPT__> "
pid, master = pty.fork()
if pid == 0:
    attrs = termios.tcgetattr(0)
    attrs[3] &= ~termios.ECHO
    termios.tcsetattr(0, termios.TCSANOW, attrs)
    child_env = dict(env)
    os.chdir(root)
    os.execvpe("bash", ["bash", "--noprofile", "--norc", "-i"], child_env)

pending = bytearray()
shell_pgrp = os.getpgid(pid)


def wait_foreground(pgrp, timeout=3):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            foreground = os.tcgetpgrp(master)
        except OSError:
            foreground = -1
        if foreground == pgrp:
            return
        time.sleep(0.01)
    raise AssertionError(("foreground_pgrp", pgrp, foreground, bytes(pending)))


def send(data):
    if isinstance(data, str):
        data = data.encode()
    os.write(master, data)


def read_until(token, timeout=10):
    deadline = time.monotonic() + timeout
    while token not in pending:
        remaining = deadline - time.monotonic()
        assert remaining > 0, (token, bytes(pending))
        readable, _, _ = select.select([master], [], [], remaining)
        assert readable, (token, bytes(pending))
        try:
            chunk = os.read(master, 4096)
        except OSError as error:
            if error.errno == errno.EIO:
                break
            raise
        assert chunk, (token, bytes(pending))
        pending.extend(chunk)
    assert token in pending, (token, bytes(pending))
    end = pending.index(token) + len(token)
    result = bytes(pending[:end])
    del pending[:end]
    return result


try:
    # Set a deterministic prompt after bash startup. Input echo is disabled on
    # the slave so the prompt token cannot be matched from the assignment text.
    time.sleep(0.2)
    while select.select([master], [], [], 0)[0]:
        try:
            pending.extend(os.read(master, 4096))
        except OSError as error:
            if error.errno == errno.EIO:
                break
            raise
    pending.clear()
    send("PS1='__DT_TTY_PROMPT__> '\n")
    read_until(prompt)

    # Interactive stdin reaches the foreground child and terminal ownership is
    # restored when it exits.
    send("devtools run read\n")
    read_until(b"READ_READY")
    send("hello\n")
    assert b"RESULT:hello" in read_until(b"RESULT:hello")
    read_until(prompt)

    # A foreground exec failure can happen after the child-side foreground
    # handoff. The wrapper must restore the shell before reporting the error.
    send("devtools run missing\n")
    read_until(prompt)
    send("printf 'MISSING_STATUS:%s\\n' \"$?\"\n")
    missing = read_until(prompt)
    assert b"MISSING_STATUS:127" in missing, missing

    # Terminal-generated SIGINT goes directly to the child process group and
    # devtools preserves the child's chosen exit status.
    send("devtools run wait\n")
    read_until(b"WAIT_READY")
    send(b"\x03")
    read_until(prompt)
    send("printf 'STATUS:%s\\n' \"$?\"\n")
    status = read_until(prompt)
    assert b"STATUS:42" in status, status

    # Ctrl+Z stops the child, devtools yields the terminal to the shell, and fg
    # restores the child as foreground before continuing it.
    send("devtools run read\n")
    read_until(b"READ_READY")
    stopped_child_pgrp = os.tcgetpgrp(master)
    assert stopped_child_pgrp != shell_pgrp, stopped_child_pgrp
    send(b"\x1a")
    stopped = read_until(prompt)
    assert b"Stopped" in stopped or b"stopped" in stopped, stopped
    send("fg\n")
    wait_foreground(stopped_child_pgrp)
    send("resumed\n")
    assert b"RESULT:resumed" in read_until(b"RESULT:resumed")
    read_until(prompt)

    # bg must continue the child without stealing the terminal from the shell.
    send("devtools run bg\n")
    read_until(b"BG_READY")
    send(b"\x1a")
    stopped = read_until(prompt)
    assert b"Stopped" in stopped or b"stopped" in stopped, stopped
    send("bg\n")
    read_until(prompt)
    wait_foreground(shell_pgrp)
    send("printf 'SHELL_OK\\n'\n")
    shell = read_until(prompt)
    assert b"SHELL_OK" in shell, shell

    # Keep the background child alive until the shell has proved it still owns
    # the terminal. Release it explicitly instead of relying on a fixed sleep,
    # which can expire before this synchronization under full-suite load.
    send("touch bg.release\n")
    read_until(prompt)
    read_until(b"BG_DONE")
    send("wait\n")
    read_until(prompt)

    # A final shell command proves terminal ownership is still usable after the
    # background child exits and Bash has reaped the completed job.
    send("printf 'FINAL_OK\\n'\n")
    final = read_until(prompt)
    assert b"FINAL_OK" in final, final
finally:
    active_error = sys.exc_info()[0] is not None
    try:
        send("exit\n")
    except OSError:
        pass
    os.close(master)
    os.waitpid(pid, 0)

print("Installed foreground TTY input, signals, stop/fg/bg and terminal restoration verified")
