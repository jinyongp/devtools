"""Installed presentation selection over real Unix PTYs and redirected streams."""
import errno
import fcntl
import json
import os
from pathlib import Path
import re
import select
import signal
import struct
import subprocess
import termios
import threading
import time
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from init_fixture import run as check_init, unix_terminal


binary = str(Path.home() / '.local/bin/devtools')
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install',
                '--version', '0.0.0-test.1', '--source', os.environ['DEVTOOLS_TEST_RELEASES']],
               check=True, capture_output=True, timeout=30)
# This scenario deliberately removes the harness's JSON preference and inherited
# agent signals; all subsequent mutations stay inside its runner-owned home.
agents = ('AI_AGENT', 'CODEX_THREAD_ID', 'CODEX_CI', 'CLAUDECODE', 'CLAUDE_CODE',
          'CURSOR_AGENT', 'GEMINI_CLI', 'OPENCODE', 'OPENCODE_CLIENT')
environment = {k: v for k, v in os.environ.items() if k not in agents
               and k not in ('DEVTOOLS_FORMAT', 'NO_COLOR')}
environment.update(TERM='xterm-256color', PATH=str(Path(binary).parent) + os.pathsep + environment['PATH'])
assert check_init(binary, Path.home() / 'init-contract', environment, unix_terminal)['result'] == 'pass'
project = Path.home() / '프로젝트 공백'
project.mkdir()
(project / 'devtools.toml').write_text('profile="presentation-fixture"\n[commands.echo]\nexec=["sh","-c","printf child-native"]\ninject=false\n', encoding='utf-8')
ansi = re.compile(r'\x1b\[[0-9;]*m')


def call(*args, extra=None, tty_out=False, tty_err=False, width=80, interrupt_after=None):
    env = dict(environment, **(extra or {}))
    descriptors = []
    readers = {}
    streams = {}
    for name, terminal in [('stdout', tty_out), ('stderr', tty_err)]:
        if terminal:
            master, slave = os.openpty()
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 24, width, 0, 0))
            descriptors.extend([master, slave])
            readers[master] = name
            streams[name] = slave
        else:
            streams[name] = subprocess.PIPE
    process = subprocess.Popen([binary, *args], cwd=project, env=env,
                               stdin=subprocess.DEVNULL, **streams)
    for slave in streams.values():
        if isinstance(slave, int) and slave >= 0:
            os.close(slave)
            descriptors.remove(slave)
    for name in ('stdout', 'stderr'):
        stream = getattr(process, name)
        if stream is not None:
            readers[stream.fileno()] = name
    output = {'stdout': bytearray(), 'stderr': bytearray()}
    deadline = time.monotonic() + 15
    interrupt_at = time.monotonic() + interrupt_after if interrupt_after is not None else None
    try:
        while readers:
            assert time.monotonic() < deadline, 'presentation fixture timed out'
            if interrupt_at is not None and time.monotonic() >= interrupt_at:
                process.send_signal(signal.SIGINT)
                interrupt_at = None
            for fd in select.select(list(readers), [], [], .1)[0]:
                try:
                    part = os.read(fd, 65536)
                except OSError as error:
                    if error.errno != errno.EIO:
                        raise
                    part = b''
                if part:
                    output[readers[fd]].extend(part)
                else:
                    del readers[fd]
        code = process.wait(timeout=2)
        return code, *(output[name].decode('utf-8').replace('\r\n', '\n') for name in ('stdout', 'stderr'))
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        for fd in descriptors:
            os.close(fd)
        for stream in (process.stdout, process.stderr):
            if stream is not None:
                stream.close()


code, plain, error = call('version')
assert code == 0 and error == '' and json.loads(plain)['data']['protocol_version'] == 25
code, terminal, error = call('version', tty_out=True)
assert code == 0 and error == '' and not ansi.sub('', terminal).lstrip().startswith('{')
assert '0.0.0-test.1' in terminal and '\x1b[1m' in terminal
for name in agents:
    code, output, error = call('version', extra={name: '1'}, tty_out=True)
    assert code == 0 and error == '' and json.loads(output)['ok'], name
code, output, error = call('version', extra={'AI_AGENT': 'false', 'CI': 'true'}, tty_out=True)
assert code == 0 and not output.lstrip().startswith('{')
code, output, error = call('--format', 'text', 'version', extra={'DEVTOOLS_FORMAT': 'invalid', 'AI_AGENT': '1'})
assert code == 0 and '0.0.0-test.1' in output and '\x1b[' not in output
code, output, error = call('--format', 'auto', 'version', extra={'DEVTOOLS_FORMAT': 'json'}, tty_out=True)
assert code == 0 and not output.lstrip().startswith('{')
code, output, error = call('version', extra={'DEVTOOLS_FORMAT': 'json'}, tty_out=True)
assert code == 0 and json.loads(output)['ok']
code, output, error = call('--format=invalid', 'version', tty_out=True, tty_err=True)
assert code == 2 and not output and json.loads(error)['error']['code'] == 'invalid_argument'
code, output, error = call('no-such-command', tty_err=True)
assert code == 2 and not output and json.loads(error)['error']['details']['reason'] == 'unknown_command'
code, output, error = call('no-such-command', tty_out=True)
assert code == 2 and not output and 'Unknown command' in error and '\x1b[' not in error
for args in [('schema', 'version'), ('config', 'schema')]:
    code, output, error = call(*args, extra={'DEVTOOLS_FORMAT': 'text'}, tty_out=True)
    assert code == 0 and error == '' and json.loads(output)['ok']
code, output, error = call('--format', 'text', 'schema', 'version')
assert code == 2 and not output and json.loads(error)['error']['code'] == 'invalid_argument'
code, output, error = call('--format', 'json', '--help', tty_out=True)
assert code == 0 and 'Usage:' in output and error == ''
code, output, error = call('--format', 'text', 'completion', 'bash')
assert code == 0 and '_devtools()' in output and error == ''
code, output, error = call('--format', 'json', 'run', 'echo', '--format', 'text', '--output', 'raw')
assert code == 0 and output == 'child-native' and error == ''

for width in (40, 79, 80, 120):
    code, output, error = call('command', 'list', '--dir', str(project), tty_out=True, width=width)
    assert code == 0 and error == '' and 'echo' in output
    code, mono, error = call('command', 'list', '--dir', str(project), tty_out=True, width=width,
                             extra={'NO_COLOR': '1'})
    assert code == 0 and ansi.sub('', output) == ansi.sub('', mono)
code, output, error = call('version', tty_out=True, extra={'TERM': 'dumb'})
assert code == 0 and '\x1b[' not in output and '0.0.0-test.1' in output

# Hold a real storage lock while the installed CLI runs through its normal
# terminal observer. Progress must stop before the final diagnostic, including
# on SIGINT; machine output must remain exactly one JSON envelope.
progress_root = Path.home() / 'progress-contract'
progress_root.mkdir(mode=0o700)
(progress_root / 'data').mkdir(mode=0o700)
progress_args = ('doctor', '--storage', '--fix', '--isolated-root', str(progress_root))
code, output, error = call('--format', 'json', *progress_args)
assert code == 0 and not error and json.loads(output)['data']['ready']
with (progress_root / 'data/.maintenance/lock').open('r+b') as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    for extra, tty, expected_feedback in [({}, True, True), ({'TERM': 'dumb'}, True, True),
                                            ({}, False, True), ({'AI_AGENT': '1'}, True, False)]:
        code, output, error = call('--format', 'text', *progress_args, '--timeout', '700ms',
                                   extra=extra, tty_out=tty, tty_err=tty, width=110)
        assert code != 0 and not output, (code, output, error)
        assert ('Waiting for storage access' in error) == expected_feedback, error
        if tty and not extra:
            assert '\r\x1b[2K' in error and 'Error:' in error.rsplit('\r\x1b[2K', 1)[1], error
        elif not tty or extra.get('TERM') == 'dumb':
            assert '\x1b' not in error and '\r' not in error, error
    code, output, error = call('--format', 'json', *progress_args, '--timeout', '700ms',
                               tty_out=True, tty_err=True)
    assert code != 0 and not output and not json.loads(error)['ok'] and '\x1b' not in error, error
    code, output, error = call('--format', 'text', *progress_args, '--timeout', '5s',
                               tty_out=True, tty_err=True, interrupt_after=.7)
    assert code == 130 and not output and 'Waiting for storage access' in error, (code, error)
    assert 'Error:' in error.rsplit('\r\x1b[2K', 1)[1], error

    # Ordinary collection commands use the same progress/lock observer and
    # cancellation path, even though they have no command-specific progress UI.
    code, output, error = call('--format', 'text', 'profile', 'list', '--isolated-root', str(progress_root),
                               tty_out=True, tty_err=True, interrupt_after=.7)
    assert code == 130 and not output and 'Waiting for storage access' in error, (code, error)
    assert 'Error:' in error.rsplit('\r\x1b[2K', 1)[1], error

# A foreground program owns both streams for its whole lifetime. Its exit status
# and output remain native even when it is quiet for longer than the UI delay.
code, output, error = call('--format', 'text', 'run', '--', 'sh', '-c',
                           'printf native-start; sleep .7; printf native-end; printf native-error >&2; exit 17',
                           tty_out=True, tty_err=True)
assert code == 17 and output == 'native-startnative-end' and error == 'native-error', (code, output, error)

# Keep real password prompts open past the progress delay. No redraw or feedback
# may appear between a prompt and its terminating newline, and input stays secret.
vault_root = Path.home() / 'progress-vault'
code, transcript = unix_terminal(
    [binary, '--format', 'text', 'vault', 'init', '--backend', 'passphrase', '--isolated-root', str(vault_root)],
    environment, project,
    [('Vault passphrase: ', 'progress-passphrase-canary\n'),
     ('Confirm vault passphrase: ', 'progress-passphrase-canary\n')], input_delay=.7)
assert code == 0 and 'progress-passphrase-canary' not in transcript, (code, transcript)
for prompt in ('Vault passphrase: ', 'Confirm vault passphrase: '):
    after_prompt = transcript.split(prompt, 1)[1].split('\n', 1)[0]
    assert after_prompt == '', (prompt, after_prompt)

# The Unix installer captures a child JSON result. Its parent still reports an
# honest overall update stage while the new executable waits for storage access.
code, output, error = call('--format', 'json', 'project', 'inspect', '--profile', 'presentation-fixture')
assert code == 0 and not error
data_root = Path(json.loads(output)['data']['paths']['data'])
code, output, error = call('--format', 'json', 'doctor', '--storage', '--fix')
assert code == 0 and not error
before_image = Path(binary).read_bytes()
with (data_root / '.maintenance/lock').open('r+b') as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    release = threading.Timer(.9, lambda: fcntl.flock(lock, fcntl.LOCK_UN))
    release.start()
    try:
        code, output, error = call('--format', 'text', 'update', '--version', '0.0.0-test.1',
                                   '--source', os.environ['DEVTOOLS_TEST_RELEASES'], tty_out=True, tty_err=True)
    finally:
        release.join()
assert code == 0 and output and 'Updating the executable and preparing storage' in error, (code, output, error)
assert Path(binary).read_bytes() == before_image

# Execute the exact standalone Next line; the path requires shell literal quoting.
code, output, error = call('--format', 'text', 'doctor', 'not-declared', '--dir', str(project))
assert code == 0 and not error
lines = [line for line in output.splitlines() if line.startswith('  devtools ') and 'command list' in line]
assert lines, output
for line in lines:
    assert '\x1b' not in line and not line.lstrip().startswith(('$', '>'))
    result = subprocess.run(['sh', '-c', line], cwd=project, env=dict(environment, DEVTOOLS_FORMAT='json'),
                            text=True, capture_output=True, timeout=15)
    assert result.returncode == 0 and json.loads(result.stdout)['data']['items'][0]['name'] == 'echo'
print('Installed progress: locks/cancellation, secret prompts, quiet native child, captured installer wait, JSON/agent silence and copyable Next passed')
