"""Two real PTYs share a key session; manual lock survives an owned daemon crash."""
import fcntl
import json
import os
from pathlib import Path
import re
import select
import shlex
import signal
import subprocess
import sys
import time
import uuid

# Keep the scenario named tty.py from shadowing the standard library.
scenario_directory = str(Path(__file__).resolve().parent)
sys.path = [entry for entry in sys.path if os.path.abspath(entry or os.getcwd()) != scenario_directory]
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import pty
import termios
from vault_fixture import retire_session

home = Path.home()
project = home / 'project'
project.mkdir()
binary = str(home / '.local/bin/devtools')
env = dict(os.environ, PATH=str(Path(binary).parent) + os.pathsep + os.environ['PATH'])
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install', '--version',
                '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               cwd=project, env=env, capture_output=True, check=True, timeout=30)
secret = 'synthetic-two-terminal-secret'
passfile = home / 'vault-passphrase'
secretfile = home / 'secret-input'
for path, value in [(passfile, 'synthetic-two-terminal-vault-passphrase\n'), (secretfile, secret)]:
    path.write_text(value)
    path.chmod(0o600)


def api(*args):
    result = subprocess.run([binary, *args], cwd=project, env=env, capture_output=True, text=True, timeout=20)
    assert result.returncode == 0 and secret not in result.stdout + result.stderr, args[:2]
    return json.loads(result.stdout)['data']


class Terminal:
    def __init__(self):
        self.prompt = ('__DVT_PROMPT_' + uuid.uuid4().hex + '__').encode()
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(project)
            os.execve('/bin/bash', ['bash', '--noprofile', '--norc', '-i'],
                      dict(env, PS1=self.prompt.decode(), PS2='', PROMPT_COMMAND='', HISTFILE='/dev/null', TERM='dumb'))
        attributes = termios.tcgetattr(self.fd)
        attributes[3] &= ~termios.ECHO
        termios.tcsetattr(self.fd, termios.TCSANOW, attributes)
        try:
            self.read_prompt()
        except BaseException:
            self.close()
            raise

    def read_prompt(self):
        output = bytearray()
        deadline = time.monotonic() + 20
        while self.prompt not in output:
            if time.monotonic() >= deadline:
                raise AssertionError('Owned terminal did not finish its command')
            if select.select([self.fd], [], [], .1)[0]:
                chunk = os.read(self.fd, 4096)
                assert chunk, 'Owned terminal closed unexpectedly'
                output.extend(chunk)
            assert len(output) < 131072, 'Unexpected terminal output size'
        assert secret.encode() not in output, 'Secret reached terminal evidence'
        return bytes(output).decode().split(self.prompt.decode())[0]

    def command(self, *args, expected=0):
        command = shlex.join([binary, *args]) + r'''; printf '\n__DVT_EXIT_%s__\n' "$?"''' + '\n'
        os.write(self.fd, command.encode())
        output = self.read_prompt()
        status = re.search(r'__DVT_EXIT_(\d+)__', output)
        assert status and int(status[1]) == expected, (args[:2], 'unexpected terminal exit status')
        return output

    def data(self, *args, expected=0):
        output = self.command(*args, expected=expected)
        return next(json.loads(line.strip()) for line in output.splitlines() if line.lstrip().startswith('{'))['data' if expected == 0 else 'error']

    def close(self):
        # Closing this PTY sends SIGHUP to its own shell/jobs. Retain the original
        # unreaped child identity until wait; never signal a PID from metadata.
        os.close(self.fd)
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline:
            if os.waitpid(self.pid, os.WNOHANG)[0]:
                return
            time.sleep(.01)
        os.kill(self.pid, signal.SIGKILL)
        os.waitpid(self.pid, 0)


api('init', '--profile', 'two-terminal')
api('vault', 'init', '--backend', 'passphrase', '--passphrase-file', str(passfile))
api('sec', 'set', 'TOKEN', '--file', str(secretfile))
data = Path(api('project', 'inspect')['paths']['data'])
directory = data / 'vault'
endpoint_path = directory / 'session.json'
terminals = []
daemon = None


def start_owned_daemon():
    previous = json.loads(endpoint_path.read_text())['session_id']
    child = subprocess.Popen([binary, '__vault-session', str(directory)], cwd=project, env=env,
                             stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    deadline = time.monotonic() + 5
    try:
        while True:
            if child.poll() is not None:
                raise AssertionError('Owned daemon exited during startup')
            if json.loads(endpoint_path.read_text())['session_id'] != previous:
                return child
            if time.monotonic() >= deadline:
                raise AssertionError('Owned daemon did not publish its endpoint')
            time.sleep(.01)
    except BaseException:
        if child.poll() is None:
            child.kill()
        child.wait(timeout=3)
        raise


try:
    first = Terminal()
    terminals.append(first)
    second = Terminal()
    terminals.append(second)
    assert first.data('vault', 'status')['item']['state'] == 'unlocked'
    assert '[REDACTED]' in second.command('run', '--output', 'redacted', '--', 'sh', '-c', 'printf %s "$TOKEN"')
    second.data('vault', 'lock')
    assert first.data('vault', 'status')['item']['state'] == 'locked_manual'
    assert first.data('sec', 'set', 'TOKEN', '--file', str(secretfile), expected=3)['code'] == 'vault_locked'
    retire_session(endpoint_path)
    with (directory / 'session-daemon.lock').open('rb') as lease:
        deadline = time.monotonic() + 5
        while True:
            try:
                fcntl.flock(lease, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                assert time.monotonic() < deadline, 'Owned daemon did not drain'
                time.sleep(.01)
        fcntl.flock(lease, fcntl.LOCK_UN)
    daemon = start_owned_daemon()
    assert first.data('vault', 'status')['item']['state'] == 'locked_manual'
    second.data('vault', 'unlock', '--passphrase-file', str(passfile))
    assert '[REDACTED]' in first.command('run', '--output', 'redacted', '--', 'sh', '-c', 'printf %s "$TOKEN"')
    second.data('vault', 'lock')
    # This is the direct Popen child retained above, not a stored daemon PID.
    daemon.kill()
    daemon.wait(timeout=5)
    daemon = None
    assert first.data('vault', 'status')['item']['state'] == 'locked_manual'
    daemon = start_owned_daemon()
    assert second.data('vault', 'status')['item']['state'] == 'locked_manual'
    first.data('vault', 'unlock', '--passphrase-file', str(passfile))
    assert '[REDACTED]' in second.command('run', '--output', 'redacted', '--', 'sh', '-c', 'printf %s "$TOKEN"')
    print(json.dumps({'two_real_ptys': 'pass', 'owned_daemon_crash_manual_latch': 'pass',
                      'explicit_unlock_reuse': 'pass', 'kernel': os.uname().release,
                      'distro_restart': 'NOT RUN'}))
finally:
    for terminal in terminals:
        terminal.close()
    if daemon is not None and daemon.poll() is None:
        daemon.terminate()
        try:
            daemon.wait(timeout=5)
        except subprocess.TimeoutExpired:
            daemon.kill()
            daemon.wait(timeout=5)
