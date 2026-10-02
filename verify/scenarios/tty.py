import errno
import json
import os
import sys
from pathlib import Path

scenario_directory = os.path.dirname(os.path.abspath(__file__))
sys.path = [entry for entry in sys.path if os.path.abspath(entry or os.getcwd()) != scenario_directory]

import pty
import select
import signal
import subprocess
import termios
import time
import uuid

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
shell_pgrp = -1


def wait_foreground(pgrp, timeout=3):
    deadline = time.monotonic() + timeout
    observed = []
    while time.monotonic() < deadline:
        try:
            foreground = os.tcgetpgrp(master)
        except OSError:
            foreground = -1
        if not observed or observed[-1] != foreground:
            observed.append(foreground)
        if foreground == pgrp:
            return
        time.sleep(0.01)
    raise AssertionError(("foreground_pgrp", pgrp, foreground, observed, bytes(pending)))


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
    shell_pgrp = os.tcgetpgrp(master)
    assert shell_pgrp > 0

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

    # Source-first profile transfer prompts securely on a real terminal. Turn
    # shell echo on deliberately so ReadPassword must suppress only the secret.
    transfer_passphrase = "TTY_TRANSFER_PASSPHRASE_CANARY"
    send("stty echo\n")
    read_until(prompt)
    send("devtools var set MODE --value tty-source\n")
    read_until(prompt)

    send("devtools profile export --output .\n")
    export_prompt = read_until(b"Profile transfer passphrase: ")
    assert not termios.tcgetattr(master)[3] & (termios.ECHO | termios.ECHONL)
    assert transfer_passphrase.encode() not in export_prompt
    send(transfer_passphrase + "\n")
    confirm_prompt = read_until(b"Confirm profile transfer passphrase: ")
    assert not termios.tcgetattr(master)[3] & (termios.ECHO | termios.ECHONL)
    assert transfer_passphrase.encode() not in confirm_prompt
    send(transfer_passphrase + "\n")
    exported = read_until(prompt)
    assert transfer_passphrase.encode() not in exported, exported
    assert b'"changed":true' in exported and (root / "tty.age").exists(), exported

    send("devtools profile import --file ./tty.age --as tty-copy\n")
    import_prompt = read_until(b"Profile transfer passphrase: ")
    assert not termios.tcgetattr(master)[3] & (termios.ECHO | termios.ECHONL)
    assert transfer_passphrase.encode() not in import_prompt
    send(transfer_passphrase + "\n")
    preview_output = read_until(prompt)
    assert transfer_passphrase.encode() not in preview_output, preview_output
    preview_lines = [line for line in preview_output.splitlines() if line.startswith(b'{"schema_version"')]
    assert preview_lines, preview_output
    preview = json.loads(preview_lines[-1])
    digest = preview["data"]["digest"]
    request_id = str(uuid.uuid4())

    send(f"devtools profile import --file ./tty.age --as tty-copy --apply {digest} --request-id {request_id}\n")
    apply_prompt = read_until(b"Profile transfer passphrase: ")
    assert not termios.tcgetattr(master)[3] & (termios.ECHO | termios.ECHONL)
    assert transfer_passphrase.encode() not in apply_prompt
    send(transfer_passphrase + "\n")
    applied = read_until(prompt)
    assert transfer_passphrase.encode() not in applied, applied
    assert b'"changed":true' in applied, applied

    send("devtools var get MODE --profile tty-copy\n")
    copied = read_until(prompt)
    assert b'"value":"tty-source"' in copied, copied

    # Cancellation must finish without another line of input and restore echo.
    send("devtools profile export --output ./canceled.age\n")
    read_until(b"Profile transfer passphrase: ")
    assert not termios.tcgetattr(master)[3] & (termios.ECHO | termios.ECHONL)
    send(b"\x03")
    canceled = read_until(prompt, timeout=3)
    assert b'"code":"canceled"' in canceled, canceled
    # Readline disables echo at its prompt; inspect restored settings from a
    # foreground child after Bash restores the normal terminal mode.
    send("printf 'CANCEL_STATUS:%s\\n' $?; python3 -c 'import termios;print(\"CANCEL_ECHO:\"+str(bool(termios.tcgetattr(0)[3]&termios.ECHO)))'\n")
    restored = read_until(prompt)
    assert b"CANCEL_STATUS:130" in restored and b"CANCEL_ECHO:True" in restored, restored

    send("devtools profile export --output ./term-canceled.age\n")
    read_until(b"Profile transfer passphrase: ")
    partial_passphrase = b"PARTIAL_TRANSFER_PRIVATE_CANARY"
    send(partial_passphrase)
    time.sleep(.05)
    os.kill(os.tcgetpgrp(master), signal.SIGTERM)
    terminated = read_until(prompt, timeout=3)
    assert b'"code":"canceled"' in terminated and partial_passphrase not in terminated, terminated
    send("printf 'TERM_CANCEL_STATUS:%s\\n' $?\n")
    resumed = read_until(prompt)
    assert b"TERM_CANCEL_STATUS:130" in resumed and partial_passphrase not in resumed, resumed
    send("stty -echo\n")
    read_until(prompt)
finally:
    active_error = sys.exc_info()[0] is not None
    try:
        send("exit\n")
    except OSError:
        pass
    os.close(master)
    os.waitpid(pid, 0)

print("Installed foreground TTY input/signals/job control plus source-first profile passphrase prompts verified")
