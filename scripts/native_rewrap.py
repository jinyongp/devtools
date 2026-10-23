"""Public protection-change checks for explicitly disposable native fixtures."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid


def check_native_rewrap(binary, env, cwd, profile, directory, *, lock_backend=None, unlock_backend=None):
    if env.get('DEVTOOLS_DISPOSABLE_HOST') != '1' or env.get('DEVTOOLS_ISOLATED_ROOT'):
        raise RuntimeError('Native rewrap requires the owned disposable host store')
    directory, cwd = Path(directory), Path(cwd)
    password = cwd / 'native-rewrap-passphrase'
    wrong = cwd / 'native-rewrap-wrong-passphrase'
    canary = 'synthetic-native-rewrap-value'
    password.write_text('synthetic-native-rewrap-password\n')
    wrong.write_text('synthetic-wrong-native-rewrap-password\n')
    for path in (password, wrong):
        path.chmod(0o600)

    def api(*args, input=None, error=None):
        p = subprocess.run([str(binary), *map(str, args)], cwd=cwd, env=env, input=input,
                           capture_output=True, text=True, timeout=45)
        for secret in (canary, 'synthetic-native-rewrap-password', 'synthetic-wrong-native-rewrap-password'):
            if secret in p.stdout + p.stderr:
                raise RuntimeError('Synthetic rewrap material reached CLI output')
        if error:
            assert p.returncode != 0 and not p.stdout
            code = json.loads(p.stderr)['error']['code']
            assert code in error, code
            return code
        if p.returncode:
            code = json.loads(p.stderr)['error']['code']
            raise RuntimeError('Public native rewrap failed: ' + '/'.join(args[:2]) + ': ' + code)
        assert not p.stderr
        return json.loads(p.stdout)['data']

    def change(source, target, request=None, **inputs):
        argv = ['vault', 'rewrap', '--from-backend', source, '--backend', target,
                '--request-id', request or str(uuid.uuid4())]
        for name, value in inputs.items():
            argv += ['--' + name.replace('_', '-'), str(value)]
        return argv

    def consume():
        digest = hashlib.sha256(canary.encode()).hexdigest()
        code = ('import os,hashlib;assert hashlib.sha256(os.environ["REWRAP_CANARY"].encode()).hexdigest()=='
                + repr(digest) + ';print("rewrap-ok",end="")')
        p = subprocess.run([str(binary), 'run', '--profile', profile, '--', sys.executable, '-c', code],
                           cwd=cwd, env=env, capture_output=True, timeout=45)
        assert p.returncode == 0 and p.stdout == b'rewrap-ok' and not p.stderr

    def profile_bytes():
        return {str(p.relative_to(directory.parent / 'profiles')): hashlib.sha256(p.read_bytes()).hexdigest()
                for p in (directory.parent / 'profiles').rglob('*') if p.is_file()}

    initial = api('vault', 'status')['item']
    assert initial['backend'] in ('dpapi', 'secret-service', 'keychain')
    api('secret', 'set', 'REWRAP_CANARY', '--profile', profile, '--stdin', input=canary)
    consume()
    before = profile_bytes()
    slot = directory / 'key-slot.json'
    evidence = {'backend': initial['backend'], 'same_root_and_profile_bytes': False}

    if lock_backend is not None:
        previous = slot.read_bytes()
        lock_backend()
        try:
            evidence['locked_native_denial_with_warm_cache'] = api(
                *change('native', 'passphrase', new_passphrase_file=password),
                error={'key_backend_locked', 'key_interaction_required', 'key_access_denied'})
            assert slot.read_bytes() == previous
        finally:
            unlock_backend()

    first = change('native', 'passphrase', new_passphrase_file=password)
    one = api(*first)
    assert one['changed'] and not one['replayed'] and one['item']['backend'] == 'passphrase'
    assert api(*first)['replayed']  # The old OS item has already been removed.
    api('vault', 'unlock', '--passphrase-file', password)
    consume()
    unchanged = slot.read_bytes()
    api(*change('passphrase', 'native', current_passphrase_file=wrong), error={'integrity_failed'})
    assert slot.read_bytes() == unchanged
    second = change('passphrase', 'native', current_passphrase_file=password)
    two = api(*second)
    assert two['changed'] and two['item']['backend'] == initial['backend']
    assert api(*second)['replayed']
    api('vault', 'unlock')
    consume()
    unchanged = slot.read_bytes()
    noop = change('native', 'native')
    three = api(*noop)
    assert not three['changed'] and not three['replayed']
    replay = api(*noop)
    assert not replay['changed'] and replay['replayed'] and slot.read_bytes() == unchanged
    # Leave no external OS item behind. Protection changes preserve explicit
    # manual intent; only the following explicit unlock clears it.
    api('vault', 'lock')
    final = api(*change('native', 'passphrase', new_passphrase_file=password))
    assert final['item']['state'] == 'locked_manual'
    api('vault', 'unlock', '--passphrase-file', password)
    consume()
    assert api('vault', 'status')['item']['vault_id'] == initial['vault_id']
    assert profile_bytes() == before
    evidence.update(native_to_passphrase=True, passphrase_to_native=True,
                    replay_after_old_item_retirement=True, wrong_current_preserves_slot=True,
                    native_noop_and_replay=True, manual_lock_preserved=True,
                    child_consumed_secret=True, same_root_and_profile_bytes=True,
                    external_native_item_retired=True)
    return evidence
