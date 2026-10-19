"""Real PAM/logind logout with a newly owned account on disposable CI only."""
import json
import os
from pathlib import Path
import pwd
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import time
import uuid

PASSWORD = b'synthetic-logout-gnome-password'
SECRET = 'synthetic-logout-cli-secret'


def command(*args, **options):
    result = subprocess.run(args, capture_output=True, timeout=40, **options)
    if result.returncode:
        # Child diagnostics can contain captured environment values. Keep them
        # out of journals and public evidence even when an assertion fails.
        raise RuntimeError('Fixture command failed: ' + Path(args[0]).name)
    return result.stdout


def wait_for(predicate, message, seconds=20):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(.1)
    raise RuntimeError(message)


def phase(root, name):
    home = Path.home()
    if os.getuid() == 0 or not home.is_relative_to(root) or not os.environ.get('XDG_SESSION_ID'):
        raise RuntimeError('Expected the owned non-root PAM session')
    runtime = Path(os.environ['XDG_RUNTIME_DIR'])
    if runtime != Path('/run/user') / str(os.getuid()):
        raise RuntimeError('PAM runtime directory mismatch')
    env = dict(os.environ, DBUS_SESSION_BUS_ADDRESS='unix:path=' + str(runtime / 'bus'))
    control = runtime / 'keyring'
    control.mkdir(mode=0o700, exist_ok=True)
    env['GNOME_KEYRING_CONTROL'] = str(control)
    wait_for(lambda: (runtime / 'bus').exists(), 'PAM user bus did not become ready')
    argv = ['gnome-keyring-daemon', '--foreground', '--components=secrets', '--control-directory', str(control)]
    if name == 'before':
        argv.append('--unlock')
    daemon = subprocess.Popen(argv, env=env, stdin=subprocess.PIPE,
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        daemon.stdin.write(PASSWORD if name == 'before' else b'')
        daemon.stdin.close()
        def available():
            if daemon.poll() is not None:
                raise RuntimeError('Owned native daemon exited')
            reply = command('dbus-send', '--session', '--print-reply', '--dest=org.freedesktop.DBus',
                            '/org/freedesktop/DBus', 'org.freedesktop.DBus.NameHasOwner',
                            'string:org.freedesktop.secrets', env=env)
            return b'boolean true' in reply
        wait_for(available, 'Native service did not become ready')
        binary = str(root / 'devtools')
        def api(*args, input=None, error=None):
            result = subprocess.run([binary, *args, '--profile', 'logout-fixture'], env=env,
                                    cwd=home, input=input, capture_output=True, text=True, timeout=20)
            if SECRET in result.stdout + result.stderr:
                raise RuntimeError('Synthetic secret reached API output')
            if error:
                response = json.loads(result.stderr)
                if result.returncode == 0 or response['error']['code'] != error:
                    raise RuntimeError('Expected protected operation denial')
                return response['error']
            if result.returncode or result.stderr:
                raise RuntimeError('Native CLI operation failed: ' + args[0])
            return json.loads(result.stdout)['data']
        def run():
            result = subprocess.run([binary, 'run', '--profile', 'logout-fixture', '--',
                                     'sh', '-c', 'printf %s "$TOKEN"'], env=env, cwd=home,
                                    capture_output=True, timeout=20)
            if result.returncode or result.stdout != b'[REDACTED]' or result.stderr:
                raise RuntimeError('Native injected run was not protected')
        if name == 'before':
            api('init')
            api('vault', 'init')
            api('sec', 'set', 'TOKEN', '--stdin', input=SECRET)
            run()
        else:
            # A previous warm devtools session must not survive OS logout and
            # bypass the now locked native collection.
            api('sec', 'set', 'TOKEN', '--stdin', input=SECRET, error='key_backend_locked')
            with socket.socket(socket.AF_UNIX) as connection:
                connection.settimeout(10)
                connection.connect(str(control / 'control'))
                _, uid, _ = struct.unpack('3i', connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
                if uid != os.getuid():
                    raise RuntimeError('Foreign native control peer')
                connection.sendall(b'\0' + struct.pack('>III', 12 + len(PASSWORD), 1, len(PASSWORD)) + PASSWORD)
                reply = bytearray()
                while len(reply) < 8:
                    part = connection.recv(8-len(reply))
                    if not part:
                        raise RuntimeError('Short native unlock reply')
                    reply.extend(part)
                if bytes(reply) != struct.pack('>II', 8, 0):
                    raise RuntimeError('Explicit native unlock failed')
            api('vault', 'unlock')
            run()
        data = Path(api('project', 'inspect')['paths']['data'])
        endpoint = json.loads((data / 'vault/session.json').read_text())
        ready = {'uid': os.getuid(), 'login_session': os.environ['XDG_SESSION_ID'],
                 'vault_session': endpoint['session_id'], 'backend': api('vault', 'status')['item']['backend']}
        # Private fixture state; only comparison booleans go into the artifact.
        target = home / (name + '.json')
        with target.open('x') as output:
            target.chmod(0o600)
            json.dump(ready, output)
        while True:
            time.sleep(1)  # Parent ends this actual verified logind session.
    finally:
        if daemon.poll() is None:
            daemon.terminate()
            try:
                daemon.wait(timeout=5)
            except subprocess.TimeoutExpired:
                daemon.kill()
                daemon.wait(timeout=5)


def terminate_owned_user(uid):
    # logind may already have removed the record after the last session ended.
    # Treat that as complete only when its runtime has also disappeared.
    runtime = Path('/run/user') / str(uid)
    found = subprocess.run(['loginctl', 'show-user', str(uid)], capture_output=True, timeout=10)
    if found.returncode == 0 or runtime.exists():
        command('loginctl', 'terminate-user', str(uid))
    wait_for(lambda: not runtime.exists(), 'Owned logout runtime survived')


def privileged(binary):
    if os.geteuid() != 0:
        raise RuntimeError('Disposable account fixture needs root')
    root = Path(tempfile.mkdtemp(prefix='devtools-logout-')).resolve()
    root.chmod(0o755)
    account = 'dvt' + uuid.uuid4().hex[:16]
    pam = Path('/etc/pam.d') / account
    home = root / 'home'
    owned_uid = None
    units = []
    pam_owned = False
    success = False
    records = []
    try:
        try:
            pwd.getpwnam(account)
        except KeyError:
            pass
        else:
            raise RuntimeError('Refusing existing account')
        command('useradd', '--no-create-home', '--home-dir', str(home), '--shell', '/bin/sh', account)
        owned_uid = pwd.getpwnam(account).pw_uid
        if owned_uid == 0:
            raise RuntimeError('Unexpected root fixture identity')
        home.mkdir(mode=0o700)
        os.chown(home, owned_uid, pwd.getpwnam(account).pw_gid)
        shutil.copyfile(binary, root / 'devtools')
        (root / 'devtools').chmod(0o755)
        shutil.copyfile(__file__, root / 'fixture.py')
        # This unique service cannot authenticate anyone. systemd opens only a
        # session; account validation is restricted to this newly created user.
        with pam.open('x') as output:
            pam_owned = True
            output.write('auth requisite pam_deny.so\naccount requisite pam_localuser.so\n'
                         'account requisite pam_succeed_if.so user = ' + account + '\n'
                         'session required pam_systemd.so\n')
        for name in ('before', 'after'):
            unit = account + '-' + name
            command('systemd-run', '--quiet', '--unit', unit, '--property', 'User=' + account,
                    '--property', 'PAMName=' + account, '--property', 'RuntimeMaxSec=90',
                    '--property', 'Environment=HOME=' + str(home) + ' DEVTOOLS_DISPOSABLE_HOST=1',
                    '/usr/bin/python3', '-B', str(root / 'fixture.py'), '--phase', str(root), name)
            units.append(unit)
            ready = home / (name + '.json')
            try:
                wait_for(ready.exists, 'Actual PAM session phase did not finish')
            except RuntimeError:
                # Only this successfully created unit, never system-wide logs.
                journal = subprocess.run(['journalctl', '--unit', unit, '--no-pager', '-n', '40', '-o', 'cat'],
                                         capture_output=True, timeout=10)
                diagnostics = journal.stdout[-12000:].replace(PASSWORD, b'[redacted fixture password]').replace(SECRET.encode(), b'[redacted fixture secret]')
                sys.stderr.buffer.write(b'Owned PAM fixture phase ' + name.encode() + b' failed:\n' + diagnostics)
                raise
            record = json.loads(ready.read_text())
            sid = record['login_session']
            properties = command('loginctl', 'show-session', sid, '--property=User', '--property=Service',
                                 '--property=Name').decode().splitlines()
            if set(properties) != {'User=' + str(owned_uid), 'Service=' + account, 'Name=' + account} or record['uid'] != owned_uid:
                raise RuntimeError('Refusing to terminate an unowned logind session')
            records.append(record)
            command('loginctl', 'terminate-session', sid)
            # Stop the newly owned user's user manager and runtime as a real
            # logout does, including detached children and the warm key session.
            terminate_owned_user(owned_uid)
            wait_for(lambda: not (Path('/run/user') / str(owned_uid)).exists(), 'Owned logout runtime survived')
        if records[0]['login_session'] == records[1]['login_session'] or records[0]['vault_session'] == records[1]['vault_session']:
            raise RuntimeError('Logout did not create new OS and vault sessions')
        if any(record['backend'] != 'secret-service' for record in records):
            raise RuntimeError('Expected actual native protection in both sessions')
        success = True
    finally:
        # Never enumerate and kill unrelated users or terminate the runner's own
        # login. Revalidate ownership before addressing the only created account.
        if owned_uid is not None:
            entry = pwd.getpwnam(account)
            if entry.pw_uid != owned_uid or entry.pw_dir != str(home):
                raise RuntimeError('Owned account changed; cleanup stopped')
            terminate_owned_user(owned_uid)
        for unit in units:
            subprocess.run(['systemctl', 'stop', unit], capture_output=True, timeout=20)
            subprocess.run(['systemctl', 'reset-failed', unit], capture_output=True, timeout=20)
        if owned_uid is not None:
            command('userdel', account)
        if pam_owned:
            pam.unlink()
        shutil.rmtree(root)
    if success:
        print(json.dumps({'real_pam_logout': 'pass', 'native_backend': 'secret-service',
                          'warm_session_lost': True, 'locked_native_access_denied': True,
                          'explicit_unlock_preserved_values': True, 'owned_account_and_home_removed': True}))


def main():
    if sys.platform != 'linux' or os.environ.get('DEVTOOLS_DISPOSABLE_HOST') != '1':
        raise RuntimeError('Requires an explicitly disposable Linux CI host')
    if len(sys.argv) == 4 and sys.argv[1] == '--phase':
        phase(Path(sys.argv[2]), sys.argv[3])
    elif len(sys.argv) == 3 and sys.argv[1] == '--privileged':
        privileged(sys.argv[2])
    elif len(sys.argv) == 1:
        with tempfile.TemporaryDirectory(prefix='devtools-logout-build-') as temporary:
            binary = Path(temporary) / 'devtools'
            subprocess.run(['go', 'build', '-o', str(binary), './cmd/devtools'], check=True, timeout=120)
            with Path('vault-secret-service-logout.stderr').open('wb') as diagnostics:
                process = subprocess.run(['sudo', '-n', 'env', 'DEVTOOLS_DISPOSABLE_HOST=1', sys.executable,
                                          '-B', str(Path(__file__).resolve()), '--privileged', str(binary)],
                                         stdout=subprocess.PIPE, stderr=diagnostics, timeout=260)
            if process.returncode:
                raise RuntimeError('Actual PAM logout fixture failed; inspect native evidence')
            result = process.stdout
            evidence = json.loads(result)
            Path('vault-secret-service-logout.json').write_text(json.dumps(evidence, indent=2) + '\n')
            print(json.dumps(evidence))
    else:
        raise RuntimeError('Unexpected fixture arguments')


if __name__ == '__main__':
    main()
