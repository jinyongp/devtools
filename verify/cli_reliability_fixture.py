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

from operation_reliability_fixture import check_operations


def build_diagnostic_validator(output, environment=None, target=None):
    repository = Path(__file__).resolve().parents[1]
    env = dict(environment or os.environ, CGO_ENABLED='0')
    if target is None:
        host = subprocess.check_output(['go', 'env', 'GOHOSTOS', 'GOHOSTARCH'], cwd=repository, env=env, text=True).splitlines()
        target = host
    env.update(GOOS=target[0], GOARCH=target[1])
    subprocess.run(['go', 'build', '-o', str(output), './verify/diagnostic-contract'], cwd=repository,
                   env=env, check=True, capture_output=True, timeout=120)
    return output


def check_cli_reliability(binary, root, environment=None, expected_commit=None, terminal=None, validator=None):
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
    documents = []
    validator = validator or env.get('DEVTOOLS_TEST_DIAGNOSTIC_VALIDATOR')
    if not validator:
        validator = build_diagnostic_validator(root / ('diagnostic-validator.exe' if os.name == 'nt' else 'diagnostic-validator'), env)

    def argv(words, selected=source, format='json'):
        return [binary, '--isolated-root', str(selected), '--format', format, *map(str, words)]

    def call(*words, selected=source, input='', error=None):
        p = subprocess.run(argv(words, selected), cwd=project, env=env, input=input,
                           capture_output=True, text=True, encoding='utf-8', timeout=30)
        assert secret not in p.stdout + p.stderr and password not in p.stdout + p.stderr, 'Private input reached output'
        if error:
            assert p.returncode != 0 and not p.stdout, 'Expected public failure'
            document = json.loads(p.stderr)
            documents.append(document)
            result = document['error']
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
    assert identity['protocol_version'] == 18
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
    assert error['details']['facts']['observed_exact'] is True
    assert call('values', 'revision', '--profile', 'reliable')['revision'] == before
    call('proxy', 'configure', '--https-address', '127.0.0.1:9443', '--disable-https', '--request-id', str(uuid.uuid4()), error='invalid_argument')
    call('expose', 'start', 'missing', '--global', '--provider', 'lan', '--serve-port', '443', '--auth-stdin', '--request-id', str(uuid.uuid4()), error='invalid_argument')
    steps.append('managed_input_budget_and_static_preflight_without_effects')

    # Presence conflicts must win before any project lookup. False is explicit.
    for words in (['command','run'], ['process','start'], ['project','up']):
        args = words + ['--proxy=false','--no-proxy=false']
        if words[0] != 'command': args += ['--request-id',str(uuid.uuid4())]
        error = call(*args,'serve',error='invalid_argument')
        assert error['details']['facts']['rule_id'].endswith('proxy-selection')
        assert error['details']['facts']['options'] == ['proxy','no-proxy']
    steps.append('proxy_presence_conflict_before_project_lookup')

    # Compare exact file size with the observation from a bounded stdin read.
    raw = project / 'raw-limit-input'
    with raw.open('wb') as stream: stream.truncate((128 << 20) + 1000)
    before = call('values','revision','--profile','reliable')['revision']
    error = call('var','set','LIMIT_PROBE','--profile','reliable','--file',str(raw),error='vault_input_too_large')
    assert error['details']['facts']['observed_exact'] is True
    with raw.open('rb') as stream:
        streamed = subprocess.run(argv(['var','set','LIMIT_PROBE','--profile','reliable','--stdin']),
                                  cwd=project,env=env,stdin=stream,capture_output=True,text=True,encoding='utf-8',timeout=30)
    assert streamed.returncode == 3 and not streamed.stdout
    document = json.loads(streamed.stderr); documents.append(document)
    assert document['error']['details']['facts']['observed_exact'] is False
    assert document['error']['details']['facts']['observed'] == (128 << 20) + 1
    assert call('values','revision','--profile','reliable')['revision'] == before
    raw.unlink()
    steps.append('exact_file_and_bounded_stdin_observation_without_mutation')

    workstream = call('task','workstream','create','--profile','diagnostic','--title','Diagnostic fixture','--request-id',str(uuid.uuid4()))
    wid, revision = workstream['item']['id'], str(workstream['revision'])
    before = call('task','workstream','plan','show',wid,'--profile','diagnostic')
    terminal_edit = None
    # Lower-level tests cover the full matrix; native journeys cover one of each.
    for index, kind in enumerate(('operation','type','reference')):
        operations = [{'op':'task.add','value':{'title':'valid'}} for _ in range(3)]
        bad = {'operation':{'op':'unknown.operation'},
               'type':{'op':'task.add','value':{'title':42,'description':secret}},
               'reference':{'op':'task.update','id':'00000000-0000-4000-8000-000000000001','value':{'title':'changed'}}}[kind]
        operations[index] = bad
        body = json.dumps({'reason':'Diagnostic fixture','operations':operations})
        args = ['task','workstream','edit',wid,'--profile','diagnostic','--stdin','--if-revision',revision,'--request-id',str(uuid.uuid4())]
        error = call(*args,input=body,error='not_found' if kind == 'reference' else 'invalid_argument')
        details = error['details']
        path = ['operations',str(index)] + {'operation':['op'],'type':['value','title'],'reference':['id']}[kind]
        assert details['sources'][0]['path'] == path and details['facts']['operation_index'] == index
        if kind == 'type':
            assert details['facts']['expected_type'] == 'string' and details['facts']['observed_type'] == 'number'
            terminal_edit = (args, body)
        location = 'operations['+str(index)+'].'+'.'.join(path[2:])
        text = subprocess.run(argv(args,format='text'),cwd=project,env=env,input=body,capture_output=True,text=True,encoding='utf-8',timeout=30)
        assert text.returncode in (2,3) and location in text.stderr and secret not in text.stderr
        scoped = False
        for remedy in details['remedies']:
            assert remedy['effect'] == 'read' and not remedy['required_inputs']
            result = subprocess.run([binary,'--format','json',*remedy['argv'][1:]],cwd=project,env=env,
                                    capture_output=True,text=True,encoding='utf-8',timeout=30)
            assert result.returncode == 0 and not result.stderr, ('Diagnostic remedy failed', remedy['argv'], result.returncode)
            if remedy['argv'][1:5] == ['task','workstream','plan','show']:
                assert wid in remedy['argv'] and str(source) in remedy['argv']
                scoped = True
        assert kind != 'reference' or scoped, 'Reference recovery lost original workstream'
        assert call('task','workstream','plan','show',wid,'--profile','diagnostic') == before
    error = call('task','workstream','activate',wid,'--profile','diagnostic','--if-revision',revision,'--request-id',str(uuid.uuid4()),error='validation_required')
    assert len(error['details']['checks']) == 3 and all(c['reason'] == 'coverage' for c in error['details']['checks'])
    steps.append('edit_locations_scoped_remedies_and_coverage_reasons')

    # Published schema consumers must agree with and without format assertions.
    def edit_body(operations):
        return {'reason':'Reference fixture','operations':operations}
    operations = [{'op':'task.add','ref':key,'value':{'title':key}} for key in ('a','b')]
    operations += [{'op':'task.depends.set','id':'@b','depends_on':['@a']},
                   {'op':'task.move','id':'@b','after_id':'@a'}]
    cases = [{'value':edit_body(operations),'valid':True}]
    fixture_id = '00000000-0000-4000-8000-000000000001'
    for ref, valid in ((fixture_id,True),('@a',True),('',False),('not-an-id',False),(fixture_id+'\n',False),('@a\n',False)):
        cases.append({'value':edit_body([{'op':'task.move','id':fixture_id,'after_id':ref}]),'valid':valid})
    published = call('schema','task','workstream','edit')['body_schema']
    validated = subprocess.run([str(validator),'--body-schema'],input=json.dumps({'schema':published,'cases':cases}),
                               capture_output=True,text=True,encoding='utf-8',timeout=30)
    assert validated.returncode == 0, 'Published reference schema disagrees with independent cases: '+validated.stderr
    reference_schema = json.loads(validated.stdout)
    preview = call('task','workstream','edit',wid,'--profile','diagnostic','--stdin','--if-revision',revision,'--dry-run',input=json.dumps(edit_body(operations)))
    assert preview['would_change'] and preview['revision'] == before['revision']
    committed = call('task','workstream','edit',wid,'--profile','diagnostic','--stdin','--if-revision',revision,'--request-id',str(uuid.uuid4()),input=json.dumps(edit_body(operations)))
    a, b = committed['created_refs']['a'], committed['created_refs']['b']
    other = call('task','workstream','create','--profile','diagnostic','--title','Other','--request-id',str(uuid.uuid4()))['item']['id']
    before = call('task','workstream','plan','show',wid,'--profile','diagnostic')
    revision = str(before['revision'])
    reference_terminal = None
    for ops, path, target in (
        ([{'op':'task.remove','id':a},{'op':'task.move','id':b,'after_id':a}], ['operations','1','after_id'], a),
        ([{'op':'task.depends.set','id':b,'depends_on':[a,fixture_id]}], ['operations','0','depends_on','1'], fixture_id),
        ([{'op':'validation.add','value':{'title':'Check','method':'Test','workstream_id':other}}], ['operations','0','value','workstream_id'], other),
    ):
        body = json.dumps(edit_body(ops))
        args = ['task','workstream','edit',wid,'--profile','diagnostic','--stdin','--if-revision',revision,'--request-id',str(uuid.uuid4())]
        details = call(*args,input=body,error='invalid_argument')['details']
        assert details['sources'][0]['path'] == path and details['facts']['reference_id'] == target
        assert details['facts']['operation_index'] == int(path[1])
        scoped = False
        for remedy in details['remedies']:
            result = subprocess.run([binary,'--format','json',*remedy['argv'][1:]],cwd=project,env=env,
                                    capture_output=True,text=True,encoding='utf-8',timeout=30)
            assert result.returncode == 0 and not result.stderr
            if remedy['argv'][1:5] == ['task','workstream','plan','show']:
                assert wid in remedy['argv'] and str(source) in remedy['argv']
                scoped = True
        assert scoped and call('task','workstream','plan','show',wid,'--profile','diagnostic') == before
        if path[-1] == 'after_id': reference_terminal = (args, body)
    if terminal:
        args, body = reference_terminal
        file = project/'invalid-anchor.json'; file.write_text(body,encoding='utf-8')
        args = list(args); args[args.index('--stdin')] = '--file'; args.insert(args.index('--file')+1,str(file))
        code, output = terminal(argv(args,format='text'),env,project)
        assert code == 2 and 'operations[1].after_id' in output and secret not in output
        steps.append('actual_terminal_edit_reference_location')
    steps.append('reference_schema_formats_and_exact_edit_provenance')

    # Exercise actual probe processes on every native platform, with private
    # output canaries and an independent fixture timeout above the probe budget.
    doctor_dir = project/'doctor'; doctor_dir.mkdir()
    def doctor_config(script):
        config = ('profile="reliable"\n[tools.probe]\nexecutable='+json.dumps(sys.executable)+
                  '\nversion="1.2.3"\nversion_args='+json.dumps(['-c',script])+
                  '\n[defaults]\ntools=["probe"]\n[commands.check]\ninject=false\nexec='+json.dumps([sys.executable,'-c','pass'])+'\n')
        (doctor_dir/'devtools.toml').write_text(config,encoding='utf-8')
    def probe_check(report):
        return next(c for c in report['commands'][0]['checks'] if c['id'] == 'tool:probe')
    doctor_config('import time; time.sleep(1); print("1.2.3")')
    doctor_args = ['doctor','check','--dir',str(doctor_dir),'--timeout','50ms']
    check = probe_check(call(*doctor_args))
    assert check['status'] == 'unknown' and check['reason'] == 'deadline_exceeded'
    assert check['remedies'][0]['effect'] == 'read' and 'overall timeout' in check['remedies'][0]['message']
    check = probe_check(call(*doctor_args,'--check',error='requirements_failed')['details']['report'])
    assert check['reason'] == 'deadline_exceeded' and check['remedies'][0]['effect'] == 'read'
    text = subprocess.run(argv(doctor_args,format='text'),cwd=project,env=env,capture_output=True,text=True,encoding='utf-8',timeout=30)
    assert text.returncode == 0 and 'overall timeout' in text.stdout and secret not in text.stdout+text.stderr
    if terminal:
        code, output = terminal(argv(doctor_args,format='text'),env,project)
        assert code == 0 and 'overall timeout' in output
        steps.append('actual_terminal_doctor_reason_recovery')
    for script, reason in (
        ('print('+repr(secret)+')','version_unreadable'),
        ('import sys; print('+repr(secret)+'); sys.exit(1)','probe_failed'),
        ('print("x"*20000)','output_limit'),
        ('import time; time.sleep(5); print("1.2.3")','timeout'),
    ):
        doctor_config(script)
        check = probe_check(call('doctor','check','--dir',str(doctor_dir),'--timeout','10s'))
        assert check['status'] == 'fail' and check['reason'] == reason and check['remedies'][0]['effect'] == 'read'
    doctor_config('print("1.2.3")')
    assert call('doctor','check','--dir',str(doctor_dir),'--timeout','10s')['ready']
    doctor_config('print("9.0.0")')
    check = probe_check(call('doctor','check','--dir',str(doctor_dir),'--timeout','10s'))
    assert check['reason'] == 'version_mismatch' and check['remedies'][0]['effect'] == 'system_write'
    steps.append('doctor_deadline_probe_reasons_and_recovery_effects')

    call('init','--profile','reliable')
    config_before = (project/'devtools.toml').read_bytes()
    bad_config = json.dumps({'operations':[{'op':'command.add','name':'other','value':{'exec':[42]}}]})
    args = ['config','edit','--revision','0'*64,'--stdin','--dry-run']
    error = call(*args,input=bad_config,error='invalid_argument')
    assert error['details']['sources'][0]['path'] == ['operations','0','value','exec','0']
    assert error['details']['facts']['expected_type'] == 'string' and error['details']['facts']['observed_type'] == 'number'
    assert (project/'devtools.toml').read_bytes() == config_before
    if terminal:
        edit_args, edit_body = terminal_edit
        edit_file = project/'invalid-edit.json'; edit_file.write_text(edit_body,encoding='utf-8')
        edit_args = [*edit_args]; edit_args[edit_args.index('--stdin')] = '--file'; edit_args.insert(edit_args.index('--file')+1,str(edit_file))
        edit_args[edit_args.index('--if-revision')+1] = revision
        code, output = terminal(argv(edit_args,format='text'),env,project)
        assert code == 2 and 'operations[1].value.title' in output and secret not in output
        config_file = project/'invalid-config.json'; config_file.write_text(bad_config,encoding='utf-8')
        code, output = terminal(argv(['config','edit','--revision','0'*64,'--file',str(config_file),'--dry-run'],format='text'),env,project)
        assert code == 2 and 'operations[0].value.exec[0]' in output and 'received number' in output
        steps.append('actual_terminal_edit_and_config_json_locations')
    steps.append('config_discriminator_type_facts_and_unchanged_bytes')

    control = json.dumps({'external_execution_id':'diagnostic-prepare','env':'local','selection':{'keys':['TOKEN']}})
    control_file = project/'control.json'; control_file.write_text(control,encoding='utf-8')
    request = str(uuid.uuid4())
    prepared = call('values','execution','prepare','--profile','reliable','--file',str(control_file),'--stdin=false','--request-id',request)
    replayed = call('values','execution','prepare','--profile','reliable','--stdin','--request-id',request,input=control)
    assert not prepared['replayed'] and replayed['replayed'] and replayed['item'] == prepared['item']
    call('values','execution','prepare','--profile','reliable','--file=','--stdin','--request-id',str(uuid.uuid4()),input=control,error='invalid_argument')
    steps.append('inactive_stdin_file_selection_and_identical_request_replay')

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
    operations = check_operations(binary, root/'operations', source, env, documents)
    steps.append('operation_order_terminal_replay_and_lock_contracts')
    checked = subprocess.run([str(validator)],input=json.dumps(documents),capture_output=True,text=True,encoding='utf-8',timeout=30)
    assert checked.returncode == 0, 'Installed diagnostic violates public Failure schema: '+checked.stderr
    schema_result = json.loads(checked.stdout)
    assert schema_result['validated'] == len(documents) and schema_result['protocol_version'] == identity['protocol_version']
    broken = json.loads(json.dumps(documents[0])); broken['error']['details']['facts']['observed_exact'] = 'false'
    negative = subprocess.run([str(validator)],input=json.dumps([broken]),capture_output=True,text=True,encoding='utf-8',timeout=30)
    assert negative.returncode != 0, 'Diagnostic schema validator accepted a wrongly typed field'
    steps.append('installed_failure_schema_and_independent_negative_control')
    return {'result':'pass','binary_identity':identity,'binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            'diagnostic_schema':schema_result,'reference_schema':reference_schema,'operations':operations,
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
    ap.add_argument('--validator',type=Path)
    args=ap.parse_args()
    terminal=unix_terminal if os.name!='nt' else None
    result=check_cli_reliability(args.binary,args.root,expected_commit=args.commit,terminal=terminal,validator=args.validator)
    args.evidence.write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(result))


if __name__=='__main__': main()
