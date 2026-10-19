"""Actual signed installation/update and native Keychain CLI access in CI."""
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile

import package as release_package
from vault_test_support import retire_vaults

SECRET = 'synthetic-installed-keychain-secret'


def checked(*args, env=None, input=None):
    result = subprocess.run(args, env=env, input=input, capture_output=True, timeout=120)
    if SECRET.encode() in result.stdout + result.stderr:
        raise RuntimeError('Synthetic secret reached native fixture output')
    if result.returncode:
        raise RuntimeError('Native installed fixture command failed: ' + Path(args[0]).name)
    return result.stdout


def main():
    if sys.platform != 'darwin' or os.environ.get('DEVTOOLS_DISPOSABLE_HOST') != '1':
        raise RuntimeError('Requires an explicitly disposable macOS host')
    keychain = Path(os.environ.get('DEVTOOLS_TEST_KEYCHAIN', ''))
    if not keychain.is_absolute() or keychain.name != 'synthetic.keychain-db' or not keychain.parent.name.startswith('devtools-native-keychain-'):
        raise RuntimeError('Requires the wrapper-owned synthetic keychain')
    expected = checked('/usr/bin/security', 'default-keychain', '-d', 'user').decode().strip().strip('"')
    if Path(expected).resolve() != keychain.resolve():
        raise RuntimeError('Synthetic keychain is not the selected default')
    with tempfile.TemporaryDirectory(prefix='devtools-native-cli-') as temporary:
        root = Path(temporary).resolve()
        home = root / 'home'
        home.mkdir(mode=0o700)
        env = dict(os.environ, HOME=str(home), XDG_CONFIG_HOME=str(home / 'config'),
                   XDG_DATA_HOME=str(home / 'data'), XDG_CACHE_HOME=str(home / 'cache'),
                   XDG_RUNTIME_DIR=str(home / 'runtime'))
        env.pop('DEVTOOLS_ISOLATED_ROOT', None)
        env.pop('DEVTOOLS_TASK_CONTEXT', None)
        # Keychain selection must remain the owned OS user's temporary keychain
        # even though devtools' filesystem paths use a synthetic HOME.
        (home / 'Library/Keychains').mkdir(parents=True, mode=0o700)
        (home / 'Library/Preferences').mkdir(parents=True, mode=0o700)
        checked('/usr/bin/security', 'list-keychains', '-d', 'user', '-s', str(keychain), env=env)
        checked('/usr/bin/security', 'default-keychain', '-d', 'user', '-s', str(keychain), env=env)
        selection = checked('/usr/bin/security', 'default-keychain', '-d', 'user', env=env).decode().strip().strip('"')
        if Path(selection).resolve() != keychain.resolve():
            raise RuntimeError('Synthetic HOME changed keychain selection')
        releases = root / 'releases'
        signatures = []
        for version in ('0.0.0-native.1', '0.0.0-native.2'):
            build = dict(os.environ, VERSION=version, COMMIT='native-keychain-fixture', OUTPUT_DIR=str(releases))
            archive = release_package.package(env=build)
            image = root / ('image-' + version)
            image.write_bytes(release_package.read_archive(archive, 'darwin'))
            image.chmod(0o700)
            # Ad-hoc signatures require no external identity/certificate. These
            # are synthetic test packages, never uploaded as release artifacts.
            checked('/usr/bin/codesign', '--force', '--sign', '-', '--identifier', 'devtools-native-vault-fixture', str(image))
            checked('/usr/bin/codesign', '--verify', '--strict', str(image))
            signature = subprocess.run(['/usr/bin/codesign', '--display', '--verbose=4', str(image)],
                                       capture_output=True, text=True, timeout=15)
            match = re.search(r'^CDHash=([0-9a-f]+)$', signature.stderr, re.MULTILINE)
            if signature.returncode or match is None:
                raise RuntimeError('Cannot inspect actual signed code identity')
            signatures.append(match[1])
            archive.unlink()
            release_package.archive_binary(archive, image, 'darwin')
            archive.with_name(archive.name + '.sha256').write_text(hashlib.sha256(archive.read_bytes()).hexdigest() + '  ' + archive.name + '\n')
        binary = home / '.local/bin/devtools'
        directory = None
        def api(*args, input=None, denied=False):
            selected = [*args, '--profile', 'native-keychain-fixture'] if args[0] in ('init', 'sec', 'var', 'project') else list(args)
            result = subprocess.run([str(binary), *selected], env=env,
                                    cwd=home, input=input, capture_output=True, timeout=40)
            if SECRET.encode() in result.stdout + result.stderr:
                raise RuntimeError('Synthetic secret reached CLI output')
            if denied:
                if result.returncode == 0:
                    raise RuntimeError('Locked Keychain allowed native unlock')
                code = json.loads(result.stderr)['error']['code']
                if code not in ('key_interaction_required', 'key_access_denied'):
                    raise RuntimeError('Unclassified locked Keychain response')
                return code
            if result.returncode or result.stderr:
                try:
                    code = json.loads(result.stderr)['error']['code']
                    if not re.fullmatch('[a-z_]{1,64}', code):
                        code = 'unclassified'
                except (ValueError, KeyError, TypeError):
                    code = 'unstructured_response'
                raise RuntimeError('Installed native CLI failed: ' + '/'.join(args[:2]) + ': ' + code)
            return json.loads(result.stdout)['data']
        def run():
            result = subprocess.run([str(binary), 'run', '--profile', 'native-keychain-fixture', '--',
                                     '/bin/sh', '-c', 'printf %s "$TOKEN"'], cwd=home, env=env,
                                    capture_output=True, timeout=40)
            if result.returncode or result.stdout != b'[REDACTED]' or result.stderr:
                raise RuntimeError('Native installed injected run failed')
        try:
            checked('sh', 'scripts/install.sh', 'install', '--version', '0.0.0-native.1', '--source', str(releases), env=env)
            checked('/usr/bin/codesign', '--verify', '--strict', str(binary))
            api('init')
            directory = Path(api('project', 'inspect')['paths']['data']) / 'vault'
            api('vault', 'init')
            api('sec', 'set', 'TOKEN', '--stdin', input=SECRET.encode())
            original = api('vault', 'status')['item']
            if original['backend'] != 'keychain':
                raise RuntimeError('Native Keychain was not selected')
            run()
            old_session = json.loads((directory / 'session.json').read_text())['session_id']
            retire_vaults(directory)
            # No vault unlock or UI input: native access must reopen a cold
            # session automatically while the OS collection remains unlocked.
            run()
            if json.loads((directory / 'session.json').read_text())['session_id'] == old_session:
                raise RuntimeError('Implicit native reopen reused the retired session')
            api('vault', 'lock')
            checked('/usr/bin/security', 'lock-keychain', str(keychain))
            denial = api('vault', 'unlock', denied=True)
            checked('/usr/bin/security', 'unlock-keychain', '-p', '', str(keychain))
            api('vault', 'unlock')
            run()
            # Retire the old cached key before updating. A warm old process may
            # not conceal failure to access the OS item from the new signature.
            api('vault', 'lock')
            retire_vaults(directory)
            updated = api('update', '--version', '0.0.0-native.2', '--source', str(releases))
            if not updated['changed'] or api('version')['version'] != '0.0.0-native.2':
                raise RuntimeError('Installed update did not apply')
            checked('/usr/bin/codesign', '--verify', '--strict', str(binary))
            update_denial = api('vault', 'unlock', denied=True)
            # Preserve the OS creator-app policy. A changed signature needs
            # explicit user authorization; prove denial did not lose the data.
            api('vault', 'lock')
            retire_vaults(directory)
            recovered = api('update', '--version', '0.0.0-native.1', '--source', str(releases))
            if not recovered['changed'] or api('version')['version'] != '0.0.0-native.1':
                raise RuntimeError('Explicit previous-version recovery did not apply')
            checked('/usr/bin/codesign', '--verify', '--strict', str(binary))
            api('vault', 'unlock')
            run()
            final = api('vault', 'status')['item']
            if final['vault_id'] != original['vault_id'] or final['backend'] != 'keychain':
                raise RuntimeError('Update replaced native protection')
        finally:
            checked('/usr/bin/security', 'unlock-keychain', '-p', '', str(keychain))
            if directory is not None:
                retire_vaults(directory)
    evidence = dict(native_installed_keychain='pass', signed_test_packages='adhoc',
                    signatures_differ=signatures[0] != signatures[1], locked_denial=denial,
                    silent_native_reopen_after_session_retirement=True,
                    old_key_session_retired_before_update=True, synthetic_home_removed=True,
                    changed_signature_authorization_required=update_denial,
                    explicit_previous_version_recovery='pass', existing_creator_app_policy_preserved=True)
    if not evidence['signatures_differ']:
        raise RuntimeError('Two versions did not have different signed identities')
    Path('vault-keychain-installed.json').write_text(json.dumps(evidence, indent=2) + '\n')
    print(json.dumps(evidence))


if __name__ == '__main__':
    main()
