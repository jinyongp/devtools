"""Run real GNOME Secret Service tests on an explicitly disposable private bus."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time


def main():
    if not sys.platform.startswith('linux') or os.environ.get('DEVTOOLS_DISPOSABLE_HOST') != '1':
        raise RuntimeError('Requires an explicitly disposable Linux host')
    if '--inside-private-bus' not in sys.argv:
        # The bus's activation environment must also use this synthetic HOME,
        # even if a regression accidentally tries service auto-activation.
        cache = subprocess.check_output(['go', 'env', 'GOCACHE', 'GOMODCACHE'], text=True).splitlines()
        with tempfile.TemporaryDirectory(prefix='devtools-native-secret-') as temporary:
            root = Path(temporary)
            runtime = root / 'runtime'
            runtime.mkdir(mode=0o700)
            env = dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root / 'config'),
                       XDG_DATA_HOME=str(root / 'data'), XDG_CACHE_HOME=str(root / 'cache'),
                       XDG_RUNTIME_DIR=str(runtime), GOCACHE=cache[0], GOMODCACHE=cache[1])
            subprocess.run(['dbus-run-session', '--', sys.executable, '-B', __file__, '--inside-private-bus'],
                           env=env, check=True, timeout=240)
        print(json.dumps({'synthetic_home_removed': True}))
        return
    if not Path.home().name.startswith('devtools-native-secret-'):
        raise RuntimeError('Private bus requires the dedicated synthetic HOME')
    env = dict(os.environ)

    def tests(pattern, log, expected, **flags):
        # go -json describes stdout only; dependency downloads go to stderr.
        # Preserve both streams without treating tool diagnostics as test events.
        with open(log, 'w') as output, open(log + '.stderr', 'w') as diagnostics:
            result = subprocess.run(['go', 'test', '-json', '-count=1', '-timeout', '90s',
                                     './internal/vault', '-run', pattern], env=dict(env, **flags),
                                    stdout=output, stderr=diagnostics, timeout=110)
        if result.returncode:
            raise RuntimeError('Native Secret Service tests failed; inspect JSON evidence')
        events = [json.loads(line) for line in Path(log).read_text().splitlines()]
        passed = {event.get('Test') for event in events if event.get('Action') == 'pass'}
        if not set(expected) <= passed:
            raise RuntimeError('Required native Secret Service tests did not actually pass')

    absent = ['TestNativeSecretServiceUnavailableDoesNotActivate']
    tests('^TestNativeSecretServiceUnavailableDoesNotActivate$', 'vault-secret-service-absent.jsonl', absent,
          DEVTOOLS_TEST_NATIVE_SECRET_SERVICE_ABSENT='1')
    daemon = subprocess.Popen(['gnome-keyring-daemon', '--foreground', '--unlock', '--components=secrets'],
                              env=env, stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        daemon.stdin.write(b'synthetic-gnome-keyring-test-password')
        daemon.stdin.close()
        deadline = time.monotonic() + 10
        while True:
            ready = subprocess.run(['dbus-send', '--session', '--print-reply', '--dest=org.freedesktop.DBus',
                                    '/org/freedesktop/DBus', 'org.freedesktop.DBus.NameHasOwner',
                                    'string:org.freedesktop.secrets'], env=env, capture_output=True, timeout=3)
            if daemon.poll() is not None:
                raise RuntimeError('Synthetic native daemon exited during setup')
            if ready.returncode == 0 and b'boolean true' in ready.stdout:
                break
            if time.monotonic() >= deadline:
                raise RuntimeError('Synthetic native daemon did not own Secret Service')
            time.sleep(.05)
        tests('^TestNativeSecretService(SyntheticVault|LockedCollection)$', 'vault-secret-service-native.jsonl',
              ['TestNativeSecretServiceSyntheticVault', 'TestNativeSecretServiceLockedCollection'],
              DEVTOOLS_TEST_NATIVE_SECRET_SERVICE='1')
    finally:
        if daemon.poll() is None:
            daemon.terminate()
            try:
                daemon.wait(timeout=5)
            except subprocess.TimeoutExpired:
                daemon.kill()
                daemon.wait(timeout=5)
    tests('^TestNativeSecretServiceUnavailableDoesNotActivate$', 'vault-secret-service-stopped.jsonl', absent,
          DEVTOOLS_TEST_NATIVE_SECRET_SERVICE_ABSENT='1')
    print(json.dumps({'native_secret_service': 'pass', 'locked_collection': 'pass',
                      'absent_and_stopped_no_activation': 'pass'}))


if __name__ == '__main__':
    main()
