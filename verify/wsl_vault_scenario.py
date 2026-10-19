"""Synthetic persisted-vault assertions inside an owned disposable WSL2 distro."""
import json
import os
from pathlib import Path
import subprocess
import sys


def main():
    home = Path.home()
    if os.getuid() == 0 or home != Path('/home/dvtfixture'):
        raise RuntimeError('Requires the dedicated non-root WSL fixture user')
    kernel = os.uname().release
    if 'microsoft' not in kernel.lower() or 'wsl2' not in kernel.lower():
        raise RuntimeError('Actual WSL2 kernel is required')
    filesystem = subprocess.check_output(['stat', '-f', '-c', '%T', str(home)], text=True).strip()
    if filesystem not in ('ext2/ext3', 'ext4'):
        raise RuntimeError('Fixture data must use the Linux filesystem')
    binary = '/usr/local/bin/devtools'
    project = home / 'project'
    secret = 'synthetic-wsl-restart-secret'
    password = home / 'passphrase'

    def command(*args, expected=0):
        result = subprocess.run([binary, *args], cwd=project, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, timeout=40)
        if result.returncode != expected or secret in result.stdout + result.stderr:
            raise RuntimeError('WSL fixture command failed or disclosed its canary')
        return result

    def api(*args, expected=0):
        result = command(*args, expected=expected)
        return json.loads(result.stderr if expected else result.stdout)['error' if expected else 'data']

    def redacted():
        result = command('run', '--', 'sh', '-c', 'printf %s "$TOKEN"')
        if result.stdout != '[REDACTED]':
            raise RuntimeError('Restarted WSL execution was not masked')

    phase = sys.argv[1]
    if phase == 'prepare':
        project.mkdir()
        password.write_text('synthetic-wsl-restart-passphrase\n')
        password.chmod(0o600)
        secretfile = home / 'secret'
        secretfile.write_text(secret)
        secretfile.chmod(0o600)
        api('init', '--profile', 'wsl-restart')
        api('vault', 'init', '--backend', 'passphrase', '--passphrase-file', str(password))
        api('sec', 'set', 'TOKEN', '--file', str(secretfile))
        redacted()
        api('vault', 'lock')
        if api('vault', 'status')['item']['state'] != 'locked_manual':
            raise RuntimeError('Manual lock was not persisted before distro termination')
    elif phase in ('after-manual-restart', 'after-unlocked-restart'):
        desired = 'locked_manual' if phase == 'after-manual-restart' else 'locked'
        if api('vault', 'status')['item']['state'] != desired:
            raise RuntimeError('Unexpected passive state after distro restart')
        if api('sec', 'set', 'TOKEN', '--file', str(home / 'secret'), expected=3)['code'] != 'vault_locked':
            raise RuntimeError('Restart reopened passphrase protection without explicit input')
        api('vault', 'unlock', '--passphrase-file', str(password))
        redacted()
        redacted()  # A second process reuses the unlocked session.
    else:
        raise RuntimeError('Unknown restart phase')
    print(json.dumps({'phase': phase, 'result': 'pass', 'kernel': kernel,
                      'filesystem': filesystem, 'uid': os.getuid(),
                      'binary_identity': api('version')}))


if __name__ == '__main__':
    main()
