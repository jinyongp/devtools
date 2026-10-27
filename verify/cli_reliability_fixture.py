"""Portable public-CLI checks. The caller owns root and session retirement."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import uuid


def check_cli_reliability(binary, root, environment=None, expected_commit=None, terminal=None):
    # Select the physical spelling before the first session is created. Windows
    # temp roots may otherwise bind init to an 8.3 alias and handoff to a long path.
    binary, root = str(Path(binary).absolute()), Path(root).resolve()
    root.mkdir(mode=0o700)
    project = root / 'project'
    project.mkdir(mode=0o700)
    source, destination = root / 'source', root / 'destination'
    env = {k: v for k, v in (environment or os.environ).items()
           if k not in ('DEVTOOLS_ISOLATED_ROOT', 'DEVTOOLS_TASK_CONTEXT')}
    env['DEVTOOLS_FORMAT'] = 'json'
    secret, password = 'synthetic-reliability-secret', 'synthetic-reliability-unlock'
    steps = []

    def argv(words, selected=source, format='json'):
        return [binary, '--isolated-root', str(selected), '--format', format, *map(str, words)]

    def call(*words, selected=source, input='', error=None):
        p = subprocess.run(argv(words, selected), cwd=project, env=env, input=input,
                           capture_output=True, text=True, encoding='utf-8', timeout=30)
        assert secret not in p.stdout + p.stderr and password not in p.stdout + p.stderr, 'Private input reached output'
        if error:
            assert p.returncode != 0 and not p.stdout, 'Expected public failure'
            result = json.loads(p.stderr)['error']
            assert result['code'] == error, (words[:2], result['code'])
            return result
        if p.returncode or p.stderr:
            try:
                error=json.loads(p.stderr)['error']
                code=error['code'];details=error.get('details',{})
                safe={key:details.get(key) for key in ('phase','reason','effects')}
                safe['facts']={key:details.get('facts',{}).get(key) for key in ('domain','stage','resource','os_code')}
            except (ValueError,KeyError,TypeError): code,safe='unstructured_failure',{}
            raise RuntimeError('Public CLI operation failed: '+'/'.join(words[:2])+': '+str(code)+' '+json.dumps(safe))
        return json.loads(p.stdout)['data']

    identity = call('version')
    assert identity['protocol_version'] == 16
    if expected_commit:
        assert identity['commit'] == expected_commit, 'Candidate SHA mismatch'
    call('vault', 'init', '--backend', 'passphrase', '--passphrase-stdin', input=password+'\n')
    call('env', 'create', 'local', '--profile', 'reliable')
    revision = call('values', 'revision', '--profile', 'reliable')['revision']
    value_file, dotenv, passphrase = project/'secret', project/'input.env', project/'passphrase'
    for path, body in ((value_file, secret), (dotenv, 'TOKEN='+secret+'\n'), (passphrase, password+'\n')):
        path.write_text(body, encoding='utf-8'); path.chmod(0o600)
    call('vault', 'lock')
    status = call('vault', 'status')
    remedy = None
    for kind in ('var', 'sec'):
        for source_kind in ('stdin', 'file'):
            for guarded in (False, True):
                args = [kind, 'set', 'TOKEN', '--profile', 'reliable', '--env', 'local', '--'+source_kind]
                if source_kind == 'file': args.append(str(value_file))
                if guarded: args += ['--revision', revision, '--request-id', str(uuid.uuid4())]
                error = call(*args, input=secret, error='vault_locked')
                details = error['details']
                assert details['retry'] == 'after_dependency_change' and details['effects'] == 'none'
                remedy = details['remedies'][0]
                assert '--passphrase-stdin' in remedy['argv'] and remedy['required_inputs'] == ['stdin']
                assert remedy['argv'][remedy['argv'].index('--isolated-root')+1] == str(source)
    for args in (['import', '--profile', 'reliable', '--file', str(dotenv), '--dry-run'],
                 ['values', 'request', 'show', str(uuid.uuid4()), '--profile', 'reliable']):
        assert call(*args, error='vault_locked')['details']['retry'] == 'after_dependency_change'
    assert call('vault', 'status') == status, 'Locked lookup changed session'
    unlock = subprocess.run([binary, '--format', 'json', *remedy['argv'][1:]], cwd=project,
                            env=env, input=password+'\n', text=True, encoding='utf-8', capture_output=True, timeout=30)
    assert unlock.returncode == 0 and not unlock.stderr, 'Published remedy did not execute'
    request = str(uuid.uuid4())
    args = ['sec', 'set', 'TOKEN', '--profile', 'reliable', '--env', 'local', '--stdin',
            '--revision', revision, '--request-id', request]
    first = call(*args, input=secret)
    assert not first['replayed'] and call(*args, input=secret)['replayed']
    idle = call('vault', 'status')['item']['idle_expires_at']
    assert call('values', 'request', 'show', request, '--profile', 'reliable')['item']['state'] == 'completed'
    assert call('vault', 'status')['item']['idle_expires_at'] == idle
    call('vault', 'lock')
    call('vault', 'unlock', '--passphrase-file', str(passphrase))
    steps.append('locked_input_matrix_scoped_remedy_file_stdin_unlock_replay_passive_lookup')

    # Prepared and attempted execution metadata remain ineligible for cleanup.
    call('values', 'execution', 'prepare', '--profile', 'reliable', '--stdin', '--request-id', str(uuid.uuid4()),
         input=json.dumps({'external_execution_id':'reliability-job','env':'local','selection':{'keys':['TOKEN']}}))
    inventory = call('cleanup', 'preview', '--profile', 'reliable')['retention']
    assert inventory['verification'] == 'verified'
    assert {'execution_record','execution_payload','execution_receipt','value_receipt'} <= {b['kind'] for b in inventory['buckets']}
    assert all(not b['cleanup_eligible'] and b['bytes'] > 0 for b in inventory['buckets'])
    call('vault', 'lock')
    assert call('cleanup', 'preview')['retention'] == {'verification':'verification_pending'}
    call('vault', 'unlock', '--passphrase-file', str(passphrase))
    steps.append('authenticated_inventory_and_locked_no_partial_totals')

    # The raw managed import limit must reject before parsing or creating receipts.
    oversized = project/'oversized.env'
    oversized.write_text('#'+'x'*500001, encoding='utf-8')
    before = call('values', 'revision', '--profile', 'reliable')['revision']
    error = call('import', '--profile', 'reliable', '--file', str(oversized), '--bundle', 'web', '--dry-run', error='invalid_argument')
    assert error['details']['facts']['limit'] == 500000
    assert call('values', 'revision', '--profile', 'reliable')['revision'] == before
    call('proxy', 'configure', '--https-address', '127.0.0.1:9443', '--disable-https', '--request-id', str(uuid.uuid4()), error='invalid_argument')
    call('expose', 'start', 'missing', '--global', '--provider', 'lan', '--serve-port', '443', '--auth-stdin', '--request-id', str(uuid.uuid4()), error='invalid_argument')
    steps.append('managed_input_budget_and_static_preflight_without_effects')

    for n in range(34):
        call('task','add','--profile','cursor','--title','row-'+str(n),'--request-id',str(uuid.uuid4()))
    first_page=call('task','list','--profile','cursor','--limit','32')
    call('task','add','--profile','cursor','--title','later','--request-id',str(uuid.uuid4()))
    last_page=call('task','list','--profile','cursor','--limit','32','--cursor',first_page['next_cursor'])
    assert len(first_page['items'])==32 and len(last_page['items'])==2 and last_page['next_cursor'] is None
    assert last_page['revision']==first_page['revision']
    steps.append('indexed_cursor_chunk_boundary_frozen_revision')

    # An open, empty pipe must be interruptible without an external provider.
    if os.name != 'nt':
        call('proxy', 'add', 'cancel', '3000', '--global', '--host', 'cancel.localhost', '--request-id', str(uuid.uuid4()))
        p = subprocess.Popen(argv(['expose','start','cancel','--global','--provider','lan','--auth-stdin','--request-id',str(uuid.uuid4())]),
                             cwd=project,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            time.sleep(.2)
            assert p.poll() is None, 'Auth pipe ended before cancellation'
            p.send_signal(signal.SIGINT)
            assert p.wait(timeout=5) == 130
            assert json.loads(p.stderr.read())['error']['code'] == 'canceled'
            assert not p.stdout.read() and not call('expose','list')['items']
        finally:
            if p.poll() is None: p.kill(); p.wait()
            for stream in (p.stdin,p.stdout,p.stderr): stream.close()
        steps.append('sigint_open_auth_pipe_no_exposure')
    elif terminal:
        call('proxy', 'add', 'cancel', '3000', '--global', '--host', 'cancel.localhost', '--request-id', str(uuid.uuid4()))
        call('expose','start','cancel','--global','--provider','lan','--auth-stdin','--request-id',str(uuid.uuid4()),error='authentication_required')
        # Console control delivery and redirected credential EOF are different
        # native contracts; input cancellation itself also has Windows pipe tests.
        code,_=terminal(argv(['run','--profile','reliable','--output','redacted','--',sys.executable,'-c','import time;time.sleep(30)']),env,project,interrupt_after=.5)
        assert code==130, 'Native console Ctrl+C did not stop foreground execution'
        assert not call('expose','list')['items']
        steps.append('native_conpty_ctrl_c_and_redirected_auth_eof_no_exposure')

    # The same masking failure is compared under the same output protection mode.
    call('sec','set','LARGE','--profile','mask','--stdin',input='가'*22000)
    mask = ['run','--profile','mask','--output','redacted','--',sys.executable,'-c','pass']
    error = call(*mask,error='redaction_unavailable')
    assert error['details']['facts']['observed'] == 66000 and error['details']['facts']['limit'] == 65536
    text = subprocess.run(argv(mask,format='text'),cwd=project,env=env,capture_output=True,text=True,encoding='utf-8',timeout=30)
    assert text.returncode == 3 and not text.stdout and '66000' in text.stderr and '65536' in text.stderr and 'Command []' not in text.stderr
    if terminal:
        code, output = terminal(argv(mask,format='text'), env, project)
        output = re.sub(r'\x1b\[[0-9;]*m','',output)
        assert code == 3 and '66000' in output and '65536' in output and 'Command []' not in output, ('terminal_mask_failure',code,len(output),'66000' in output,'65536' in output)
        steps.append('actual_terminal_same_mask_failure_facts')
    steps.append('json_text_same_mask_failure_facts')

    # Full encrypted backup journey through a different root and exact UUID replay.
    identity_file, recipient = project/'backup.identity', project/'backup.recipient'
    call('backup','keygen','--identity-file',str(identity_file),'--recipient-file',str(recipient))
    archive = call('backup','create','--profile','reliable','--recipient-file',str(recipient),'--output',str(project/'backup.age'))['item']['path']
    call('backup','inspect','--file',archive,'--identity-file',str(identity_file))
    call('vault','init','--backend','passphrase','--passphrase-stdin',selected=destination,input=password+'\n')
    restore = ['backup','restore','--file',archive,'--identity-file',str(identity_file),'--profile','reliable']
    preview = call(*restore,selected=destination)
    request = str(uuid.uuid4())
    applied = call(*restore,'--apply',preview['digest'],'--request-id',request,selected=destination)
    assert applied['applied'] and not applied['replayed']
    assert call(*restore,'--apply',preview['digest'],'--request-id',request,selected=destination)['replayed']
    steps.append('backup_create_inspect_preview_other_root_restore_uuid_retry')
    return {'result':'pass','binary_identity':identity,'binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            'platform':sys.platform,'checks':steps}


def unix_terminal(argv, env, cwd):
    import errno
    import select
    master, slave = os.openpty()
    p = subprocess.Popen(argv,cwd=cwd,env=env,stdin=subprocess.DEVNULL,stdout=slave,stderr=slave)
    os.close(slave)
    body = bytearray(); deadline = time.monotonic()+30
    try:
        while True:
            assert time.monotonic()<deadline, 'PTY check timed out'
            if not select.select([master],[],[],.1)[0]: continue
            try: block=os.read(master,65536)
            except OSError as error:
                if error.errno != errno.EIO: raise
                break
            if not block: break
            body.extend(block)
        return p.wait(timeout=5),body.decode('utf-8')
    finally:
        if p.poll() is None: p.kill();p.wait()
        os.close(master)


def main():
    ap=argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--binary',type=Path,required=True)
    ap.add_argument('--root',type=Path,required=True)
    ap.add_argument('--commit')
    ap.add_argument('--evidence',type=Path,required=True)
    args=ap.parse_args()
    terminal=unix_terminal if os.name!='nt' else None
    result=check_cli_reliability(args.binary,args.root,expected_commit=args.commit,terminal=terminal)
    args.evidence.write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(result))


if __name__=='__main__': main()
