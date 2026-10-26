"""Installed presentation selection over real Unix PTYs and redirected streams."""
import errno
import fcntl
import json
import os
from pathlib import Path
import re
import select
import struct
import subprocess
import termios
import time


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
project = Path.home() / '프로젝트 공백'
project.mkdir()
(project / 'devtools.toml').write_text('profile="presentation-fixture"\n[commands.echo]\nexec=["sh","-c","printf child-native"]\ninject=false\n', encoding='utf-8')
ansi = re.compile(r'\x1b\[[0-9;]*m')


def call(*args, extra=None, tty_out=False, tty_err=False, width=80):
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
    try:
        while readers:
            assert time.monotonic() < deadline, 'presentation fixture timed out'
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
assert code == 0 and error == '' and json.loads(plain)['data']['protocol_version'] == 15
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
assert code == 2 and not output and 'unknown_command' in error and '\x1b[' not in error
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

# Execute the exact standalone Next line; the path requires shell literal quoting.
code, output, error = call('--format', 'text', 'doctor', 'not-declared', '--dir', str(project))
assert code == 0 and not error
lines = [line for line in output.splitlines() if line.startswith('devtools ') and 'command list' in line]
assert lines, output
for line in lines:
    assert '\x1b' not in line and not line.startswith((' ', '$', '>'))
    result = subprocess.run(['sh', '-c', line], cwd=project, env=dict(environment, DEVTOOLS_FORMAT='json'),
                            text=True, capture_output=True, timeout=15)
    assert result.returncode == 0 and json.loads(result.stdout)['data']['items'][0]['name'] == 'echo'
print('Installed clean/agent PTY, pipe/error streams, format precedence, native boundaries, widths and copyable Next passed')
