"""Known secrets in real CLI pipes, PTYs, capture logs and blocked output."""
import errno
import json
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import time
import uuid

# Avoid this scenario directory shadowing the stdlib's tty module.
scenario_directory = os.path.dirname(os.path.abspath(__file__))
sys.path = [entry for entry in sys.path if os.path.abspath(entry or os.getcwd()) != scenario_directory]
import pty

root = Path.home() / 'project'
root.mkdir()
env = dict(os.environ, PATH=str(Path.home() / '.local/bin') + ':' + os.environ['PATH'])
subprocess.run(['sh', os.environ['DEVTOOLS_TEST_INSTALLER'], 'install', '--version', '0.0.0-test.1',
                '--source', os.environ['DEVTOOLS_TEST_RELEASES']], cwd=root, env=env, check=True)
binary = str(Path.home() / '.local/bin/devtools')
canary = 'synthetic-stream-secret'
(root / 'devtools.toml').write_text('''profile="stream-redaction"
[commands.capture]
exec=["python3","emit.py"]
inject=true
output="raw"
''')
(root / 'emit.py').write_text('''import os,sys
from pathlib import Path
value=os.environ['TOKEN'].encode()
if len(sys.argv)>1 and sys.argv[1]=='hold':
 Path('writer-ready').write_text('ready')
 while True: os.write(1,value*1024)
if len(sys.argv)>1 and sys.argv[1]=='binary': os.write(1,b'\\x00\\xff')
for stream in (1,2):
 for byte in value: os.write(stream,bytes([byte]))
if len(sys.argv)>1 and sys.argv[1]=='binary': os.write(1,b'\\x80')
raise SystemExit(23)
''')

def api(*args, input=None):
    result = subprocess.run([binary, *args], cwd=root, env=env, input=input,
                            capture_output=True, timeout=30)
    assert result.returncode == 0, (args[:2], result.stderr)
    assert canary.encode() not in result.stdout + result.stderr
    return json.loads(result.stdout)['data']

api('vault', 'init', '--backend', 'passphrase', '--passphrase-stdin', input=b'synthetic-test-vault-passphrase\n')
api('sec', 'set', 'TOKEN', '--stdin', input=canary.encode())

def run(*options, binary_output=False):
    return subprocess.run([binary, 'run', *options, '--', 'python3', 'emit.py',
                           *(['binary'] if binary_output else [])], cwd=root, env=env,
                          stdin=subprocess.DEVNULL, capture_output=True, timeout=20)

result = run()
assert result.returncode == 23 and result.stdout == result.stderr == b'[REDACTED]'
result = run('--stdout', 'raw', binary_output=True)
assert result.returncode == 23 and result.stdout == b'\x00\xff' + canary.encode() + b'\x80'
assert result.stderr == b'[REDACTED]', 'binary stdout exception unprotected stderr'
result = run('--output', 'raw')
assert result.returncode == 23 and result.stdout == result.stderr == canary.encode()

# Capture always protects before disk storage, including an explicit foreground
# raw policy in the command configuration. The effective reason is public.
record = api('process', 'start', 'capture', '--capture-logs', '--request-id', str(uuid.uuid4()))['item']
execution = record['id']
deadline = time.monotonic() + 5
while record['ended_at'] is None:
    assert time.monotonic() < deadline, 'captured process did not finish'
    record = api('process', 'status', execution)['item']
    time.sleep(.01)
assert record['output']['requested']['mode'] == 'raw'
assert record['output']['stdout'] == record['output']['stderr'] == 'redacted'
assert record['output']['stdout_reason'] == record['output']['stderr_reason'] == 'capture'
content = api('process', 'logs', execution)['content']
assert content.count('[REDACTED]') == 2 and canary not in content
for path in root.parent.rglob('processes/' + execution + '/*'):
    if path.is_file() and path.suffix in {'.json', '.log'}:
        assert canary.encode() not in path.read_bytes(), 'plaintext reached runtime disk'

# A PTY is not an identity check: auto preserves the documented terminal raw
# exception, whereas explicit redacted mediates the actual terminal streams.
def terminal(*options):
    pid, master = pty.fork()
    if pid == 0:
        os.chdir(root)
        os.execve(binary, [binary, 'run', *options, '--', 'python3', 'emit.py'], env)
    output = bytearray()
    deadline = time.monotonic() + 15
    waited = False
    try:
        while time.monotonic() < deadline:
            readable, _, _ = select.select([master], [], [], .1)
            if readable:
                try:
                    chunk = os.read(master, 4096)
                except OSError as error:
                    if error.errno == errno.EIO:
                        break
                    raise
                if not chunk:
                    break
                output.extend(chunk)
        else:
            raise AssertionError('PTY execution did not reach EOF')
        _, status = os.waitpid(pid, 0)
        waited = True
        assert os.waitstatus_to_exitcode(status) == 23
        return bytes(output)
    finally:
        os.close(master)
        if not waited:
            # This PID is the child just returned by our own fork, not stored
            # supervisor metadata or another user's process.
            try:
                os.kill(pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            os.waitpid(pid, 0)

assert canary.encode() in terminal()
protected = terminal('--output', 'redacted')
assert canary.encode() not in protected and protected.count(b'[REDACTED]') == 2

# Do not drain stdout before cancellation. A blocked destination must not make
# wrapper shutdown wait on the redactor mutex or flush its pending secret prefix.
(root / 'writer-ready').unlink(missing_ok=True)
child = subprocess.Popen([binary, 'run', '--', 'python3', 'emit.py', 'hold'], cwd=root,
                         env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
try:
    deadline = time.monotonic() + 5
    while not (root / 'writer-ready').exists():
        assert time.monotonic() < deadline, 'writer did not start'
        time.sleep(.01)
    assert select.select([child.stdout], [], [], 5)[0], 'no protected output reached the pipe'
    # This controlled pause is the slow-reader workload, not a completion wait.
    time.sleep(.05)
    child.send_signal(signal.SIGTERM)
    child.wait(timeout=8)
    assert child.returncode != 0
    output, diagnostic = child.communicate(timeout=2)
    assert canary.encode() not in output + diagnostic
finally:
    if child.poll() is None:
        child.kill()
        child.wait(timeout=3)
    child.stdout.close()
    child.stderr.close()
print('Installed pipe/PTY modes, binary stdout, stderr, capture-before-storage and blocked-output cancellation verified')
