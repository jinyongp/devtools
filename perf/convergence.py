#!/usr/bin/env python3
"""Measure actual managed value notification/application on owned Linux/WSL data."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile
import time
import uuid

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / 'scripts'))
from vault_test_support import retire_vaults

PASSWORD = 'synthetic-convergence-unlock'
SECRET = 'synthetic-convergence-secret'


def measure(binary, size, policy, deadline, storm, row):
    root = Path(tempfile.mkdtemp(prefix='devtools-convergence-')).resolve()
    root.chmod(0o700)
    env = {k: v for k, v in os.environ.items()
           if not k.startswith(('DEVTOOLS_', 'BENCH_VALUE_')) and k not in ('TOKEN', 'REV')}
    env['DEVTOOLS_ISOLATED_ROOT'] = str(root / 'state')
    cleaning = False

    def api(*args, input=None):
        remaining = 30 if cleaning else min(30, deadline - time.monotonic())
        if remaining <= 0:
            raise RuntimeError('Convergence run budget exhausted')
        p = subprocess.run([str(binary), *map(str, args)], cwd=root, env=env,
                           input=input, capture_output=True, text=True, timeout=remaining)
        if SECRET in p.stdout + p.stderr or PASSWORD in p.stdout + p.stderr:
            raise RuntimeError('Fixture credential disclosed')
        if p.returncode or p.stderr:
            raise RuntimeError('Convergence fixture command failed: ' + args[0])
        envelope = json.loads(p.stdout)
        if not envelope.get('ok') or envelope.get('schema_version') != 1:
            raise RuntimeError('Invalid CLI response')
        return envelope['data']

    def wait(check):
        until = min(deadline, time.monotonic() + 20)
        while time.monotonic() < until:
            result = check()
            if result:
                return result
            time.sleep(.05)
        raise RuntimeError('Convergence observation timed out')

    def observation(revision):
        try:
            value = json.loads((root / 'observed.json').read_text())
        except (OSError, ValueError):
            return False
        if value.get('revision') != revision:
            return False
        assert value == {'revision': revision, 'token_present': True, 'base_keys': size}
        return value

    def status(execution):
        return api('process', 'status', execution)

    config = f'''profile='convergence'
[commands.web]
exec={json.dumps([sys.executable, 'server.py'])}
inject=true
values_update='{policy}'
[commands.web.ready]
exec={json.dumps([sys.executable, 'ready.py'])}
timeout='5s'
'''
    (root / 'devtools.toml').write_text(config)
    (root / 'server.py').write_text('''import json,os,time
from pathlib import Path
p=Path('observed.json');t=p.with_suffix('.tmp')
t.write_text(json.dumps({'revision':os.environ['REV'],'token_present':'TOKEN' in os.environ,'base_keys':sum(k.startswith('BENCH_VALUE_') for k in os.environ)}));os.replace(t,p)
time.sleep(600)
''')
    (root / 'ready.py').write_text('''import json,os
from pathlib import Path
try: ready=json.loads(Path('observed.json').read_text())['revision']==os.environ['REV']
except (OSError,ValueError,KeyError): ready=False
raise SystemExit(0 if ready else 1)
''')
    rows = row['samples']
    try:
        api('vault', 'init', '--backend', 'passphrase', '--passphrase-stdin', input=PASSWORD+'\n')
        names = [f'BENCH_VALUE_{i:04d}' for i in range(size)]
        source = root / 'original.env'
        source.write_text(''.join(name+'=synthetic-public-'+str(i)+'\n' for i, name in enumerate(names)))
        source.chmod(0o600)
        api('import', '--file', source, *[x for name in names for x in ('--var', name)])
        api('sec', 'set', 'TOKEN', '--stdin', input=SECRET)
        api('var', 'set', 'REV', '--value', '0')
        current = api('process', 'start', 'web', '--request-id', str(uuid.uuid4()))['item']
        api('process', 'wait', current['id'], '--timeout', '10s')
        wait(lambda: observation('0'))
        assert len(current['input_version']['slots']) == size + 2
        for number in range(1, 4):
            previous = current['id']
            revision = str(number)
            started = time.perf_counter_ns()
            api('var', 'set', 'REV', '--value', revision)
            committed = time.perf_counter_ns()

            def converged():
                s = status(previous)
                if policy == 'notify':
                    if s['item'].get('value_watch', {}).get('state') == 'pending':
                        assert s['item']['state'] == 'running' and not s['item'].get('value_apply')
                        return s
                else:
                    update = s['item'].get('value_apply', {})
                    if update.get('state') in ('failed', 'paused'):
                        raise RuntimeError('Automatic application failed: '+update['reason'])
                    if update.get('state') == 'applied':
                        assert update['reason'] == 'readiness_passed'
                        successor = status(update['next_id'])
                        if observation(revision):
                            assert successor['item']['previous_id'] == previous
                            assert successor['item']['state'] == 'running' and successor['values']['state'] == 'current'
                            return successor
                return None

            result = wait(converged)
            observed = time.perf_counter_ns()
            manual_ns = None
            if policy == 'notify':
                assert observation(str(number-1))
                assert len(api('process', 'list')['items']) == number
                before_manual = time.perf_counter_ns()
                current = api('process', 'restart', previous, '--request-id', str(uuid.uuid4()))['item']
                api('process', 'wait', current['id'], '--timeout', '10s')
                wait(lambda: observation(revision))
                manual_ns = time.perf_counter_ns() - before_manual
            else:
                current = result['item']
            rows.append({'revision_index': number, 'commit_call_ns': committed-started,
                         'commit_return_to_observation_ns': observed-committed,
                         'commit_start_to_observation_ns': observed-started,
                         'outcome': 'watch_pending' if policy == 'notify' else 'applied_readiness_and_child',
                         'explicit_manual_apply_ns': manual_ns})
        storm_result = None
        if storm:
            before = len(api('process', 'list')['items'])
            api('var', 'set', 'REV', '--value', 'storm-four')
            def paused():
                update = status(current['id'])['item'].get('value_apply', {})
                if update.get('state') == 'paused':
                    assert update['reason'] == 'restart_storm'
                    return True
                return False
            wait(paused)
            api('var', 'set', 'REV', '--value', 'storm-five')
            # More than two configured periods; also require the actual latest watcher goal.
            if deadline - time.monotonic() < 4.5:
                raise RuntimeError('Insufficient run budget for storm observation')
            time.sleep(4.5)
            after = api('process', 'list')['items']
            assert before == len(after) == 4 and sum(r['ended_at'] is None for r in after) == 1
            assert paused() and observation('3')
            latest = status(current['id'])
            assert latest['values']['state'] == 'pending'
            assert latest['item']['value_watch']['goal'] == latest['values']['current']['identity']
            storm_result = {'successful_automatic_replacements': 3, 'fourth_goal': 'paused_restart_storm',
                            'fifth_goal_after_4_5_seconds': 'watcher_current_goal_still_paused_no_launch',
                            'records_before': before, 'records_after': len(after), 'active': 1}
        row['storm'] = storm_result
        row['complete'] = True
    finally:
        cleaning = True
        try:
            api('project', 'down', '--request-id', str(uuid.uuid4()))
            assert not any(r['ended_at'] is None for r in api('process', 'list')['items'])
            retire_vaults(root / 'state' / 'data' / 'vault')
        except BaseException:
            row['retained_root'] = str(root)
            raise RuntimeError('Cleanup unconfirmed; owned private fixture retained at '+str(root)) from None
        shutil.rmtree(root)
        row['cleanup_confirmed'] = True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--sizes', default='10,100,1000')
    parser.add_argument('--roots', type=int, default=3)
    parser.add_argument('--budget-seconds', type=int, default=300)
    args = parser.parse_args()
    if not sys.platform.startswith('linux'):
        parser.error('This measurement harness supports Linux/WSL only')
    try:
        sizes = [int(x) for x in args.sizes.split(',')]
    except ValueError:
        parser.error('sizes must be distinct positive integers up to 1000')
    if not sizes or len(set(sizes)) != len(sizes) or any(not 1 <= x <= 1000 for x in sizes) or args.roots < 1 or args.budget_seconds < 1:
        parser.error('Invalid sizes, roots or run budget')
    binary = args.binary.resolve()
    if not binary.is_file():
        parser.error('Binary does not exist')
    output = args.output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    artifact = output.open('x', encoding='utf-8')
    result = {'measurement': 'managed-value-convergence-v1', 'complete': False, 'rows': [],
              'kernel': platform.release(), 'machine': platform.machine(), 'python': platform.python_version(),
              'run_budget_seconds': args.budget_seconds, 'poll_seconds': .05,
              'command': [sys.executable, *sys.argv],
              'limits': ['Notify pending and automatic applied are different outcomes, not equal-work throughput arms.',
                         'Observation includes CLI queries and polling; it is an upper bound after commit return, not an internal event timestamp.',
                         'Setup/unlock/manual apply/cleanup are separate from automatic convergence; all raw samples retained.',
                         'Single Linux/WSL host, uncontrolled external load, no SLA or statistical significance claim.']}
    deadline = time.monotonic() + args.budget_seconds
    try:
        result['binary_sha256'] = hashlib.sha256(binary.read_bytes()).hexdigest()
        result['harness_sha256'] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        result['identity'] = json.loads(subprocess.check_output([str(binary), 'version'], timeout=10))['data']
        result['source_commit'] = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip()
        result['source_dirty'] = bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=REPO, text=True).strip())
        result['cpu_model'] = next((line.split(':', 1)[1].strip() for line in Path('/proc/cpuinfo').read_text().splitlines() if line.startswith('model name')), platform.processor())
        for size in sizes:
            for repeat in range(args.roots):
                for policy in (['notify', 'restart'] if repeat % 2 == 0 else ['restart', 'notify']):
                    row = {'base_public_keys': size, 'additional_revision_keys': 1, 'secrets': 1,
                           'independent_root': repeat, 'policy': policy, 'samples': [],
                           'complete': False, 'cleanup_confirmed': False}
                    result['rows'].append(row)
                    measure(binary, size, policy, deadline,
                            policy == 'restart' and size == sizes[-1] and repeat == args.roots-1, row)
                    print(json.dumps({'keys': size, 'root': repeat, 'policy': policy, 'result': 'pass'}), flush=True)
        result['complete'] = True
    finally:
        try:
            artifact.write(json.dumps(result, indent=2)+'\n')
        finally:
            artifact.close()


if __name__ == '__main__':
    main()
