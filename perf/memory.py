#!/usr/bin/env python3
"""Matched Linux/WSL process RSS measurements using disposable owned data."""

import argparse
import concurrent.futures
import datetime
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

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "scripts"))
from vault_test_support import retire_vaults

PASSWORD='synthetic-disposable-memory-unlock'

def identity(binary):
    r=subprocess.run([str(binary),'version'],capture_output=True,check=True,timeout=10)
    return {'sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'version':json.loads(r.stdout)['data']}

def measure(binary, protected, size, repeat):
    scratch=tempfile.mkdtemp(prefix='devtools-memory-')
    root=Path(scratch).resolve();root.chmod(0o700)
    env={k:v for k,v in os.environ.items() if not k.startswith('DEVTOOLS_')}
    env.update(DEVTOOLS_ISOLATED_ROOT=str(root),LC_ALL='C')
    data=root/'data';vault=data/'vault';worker=None
    def cli(*args,input=None):
        r=subprocess.run([str(binary),*map(str,args)],cwd=root,env=env,input=input,capture_output=True,timeout=30)
        if r.returncode or r.stderr:raise RuntimeError('Memory fixture command failed: '+args[0])
        return json.loads(r.stdout)['data']
    def proc_memory():
        if worker.poll() is not None:raise RuntimeError('Owned worker ended during memory sampling')
        fields={}
        for line in Path('/proc',str(worker.pid),'status').read_text().splitlines():
            key,_,value=line.partition(':')
            if key in ('VmRSS','VmHWM'):fields[key+'_KiB']=int(value.strip().split()[0])
        if len(fields)!=2 or worker.poll() is not None:raise RuntimeError('Missing live owned-worker RSS metrics')
        return fields
    def timed_cli(args,index,input=None):
        path=root/('rss-'+str(index)+'.txt')
        begin=time.perf_counter_ns()
        r=subprocess.run(['/usr/bin/time','-f','%M','-o',str(path),str(binary),*map(str,args)],cwd=root,env=env,input=input,capture_output=True,timeout=30)
        if r.returncode or r.stderr:raise RuntimeError('Timed memory fixture failed')
        return {'peak_cli_and_waited_children_RSS_KiB':int(path.read_text().strip()),'wall_including_gnu_time_ns':time.perf_counter_ns()-begin,'stdout':r.stdout}
    try:
        cli('init','--profile','memory-bench')
        if protected:cli('vault','init','--backend','passphrase','--passphrase-stdin',input=(PASSWORD+'\n').encode())
        source=root/'original.env'
        source.write_text(''.join('TOKEN_%04d=synthetic-memory-%04d-abcdefghijklmnopqrstuv\n'%(i,i) for i in range(size)))
        source.chmod(0o600)
        cli('import','--file',source)
        memory=[];cold=[];status=None
        if protected:
            previous=json.loads((vault/'session.json').read_text())['session_id']
            retire_vaults(vault)
            worker=subprocess.Popen([str(binary),'__vault-session',str(vault)],cwd=root,env=env,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            deadline=time.monotonic()+8
            while True:
                if worker.poll() is not None:raise RuntimeError('Owned worker start failed')
                try:ready=json.loads((vault/'session.json').read_text())['session_id']!=previous
                except (OSError,ValueError):ready=False
                if ready:break
                if time.monotonic()>deadline:raise RuntimeError('Owned worker endpoint not ready')
                time.sleep(.01)
            cold_start=proc_memory()
            for sample in range(3):
                cli('vault','lock')
                m=timed_cli(['vault','unlock','--passphrase-stdin'],'cold-'+str(sample),(PASSWORD+'\n').encode())
                status=json.loads(m.pop('stdout'))['data']['item']
                if status['state']!='unlocked':raise RuntimeError('Cold explicit unlock not successful')
                cold.append(m)
            memory.append({'phase':'after_three_cold_explicit_unlocks',**proc_memory()})
        for sample in range(5):
            m=timed_cli(['run','--','/usr/bin/true'],'warm-'+str(sample))
            if m.pop('stdout'):raise RuntimeError('Noop child unexpectedly emitted output')
            memory_row=m
            memory_row['phase']='warm_run' if protected else 'legacy_run'
            memory.append(memory_row)
        concurrent_samples=[]
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            samples=list(pool.map(lambda n:timed_cli(['run','--','/usr/bin/true'],'concurrent-'+str(n)),range(4)))
        for m in samples:
            if m.pop('stdout'):raise RuntimeError('Concurrent noop emitted output')
            concurrent_samples.append(m)
        holding=None
        if protected:
            before=cli('vault','status')['item']
            observed=datetime.datetime.now(datetime.timezone.utc)
            until=datetime.datetime.fromisoformat(before['idle_expires_at'])
            begin=time.perf_counter_ns();locked=cli('vault','lock')['item'];lock_ns=time.perf_counter_ns()-begin
            if locked['state']!='locked_manual' or locked.get('idle_expires_at') is not None:raise RuntimeError('Manual lock did not clear cache status')
            holding={'observed_at':observed.isoformat(),'idle_expires_at':before['idle_expires_at'],'observed_remaining_idle_seconds':(until-observed).total_seconds(),'explicit_lock_cli_wall_ns':lock_ns,'after_lock':proc_memory(),'cache_state_after_lock':locked['state'],'secure_heap_erasure_claimed':False}
        return {'arm':'protected' if protected else 'legacy','key_count':size,'synthetic_value_bytes':len('synthetic-memory-0000-abcdefghijklmnopqrstuv'),'repeat':repeat,'cold_before':cold_start if protected else None,'cold_unlock_cli':cold,'samples':memory,'concurrent_four_cli':concurrent_samples,'holding':holding}
    finally:
        cleanup_error=None
        if protected:
            try:retire_vaults(vault)
            except BaseException as error:cleanup_error=error
            finally:
                if worker is not None:
                    try:worker.wait(timeout=5)
                    except subprocess.TimeoutExpired:worker.kill();worker.wait(timeout=5)
        if cleanup_error is not None:
            raise RuntimeError('Cannot confirm daemon retirement; private fixture retained at '+str(root)) from None
        shutil.rmtree(root)


def main():
    parser=argparse.ArgumentParser(description="Measure matched Linux/WSL CLI and owned key-daemon process RSS with disposable data.")
    parser.add_argument('--baseline',type=Path,required=True)
    parser.add_argument('--candidate',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--sizes',default='10,100,1000')
    parser.add_argument('--repeats',type=int,default=3)
    args=parser.parse_args()
    if not sys.platform.startswith('linux') or not Path('/proc/self/status').is_file() or not Path('/usr/bin/time').is_file():
        parser.error('Requires Linux/WSL /proc and GNU /usr/bin/time')
    if args.repeats<1:parser.error('repeats must be positive')
    try:sizes=[int(n) for n in args.sizes.split(',')]
    except ValueError:parser.error('sizes must be positive comma-separated integers')
    if not sizes or any(n<1 for n in sizes) or len(set(sizes))!=len(sizes):parser.error('sizes must be distinct and positive')
    baseline=args.baseline.resolve();candidate=args.candidate.resolve();out=args.output.resolve()
    if not baseline.is_file() or not candidate.is_file():parser.error('Both binary files must exist')
    if out.exists():parser.error('Refusing to overwrite prior memory evidence')
    out.parent.mkdir(parents=True,exist_ok=True)
    result={'measurement':'linux-process-rss-v1','harness_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'kernel':platform.release(),'machine':platform.machine(),'python':platform.python_version(),'baseline':identity(baseline),'candidate':identity(candidate),'rows':[],'complete':False,'limitations':['GNU time reports maximum RSS of CLI and waited children; a detached owned daemon is measured separately from /proc/<owned PID>/status VmHWM.','Independent disposable roots/workers per size; worker VmHWM after three cold unlock cycles is one high-water mark, not three independent peaks.','No native OS key API count inference. RSS after lock does not establish secret retention or erasure.','Linux/WSL only; unrelated host load is not controlled. No RSS acceptance threshold is invented.']}
    artifact=out.open('x',encoding='utf-8')
    try:
        for size in sizes:
            for repeat in range(args.repeats):
                arms=[False,True] if repeat%2==0 else [True,False]
                for protected in arms:
                    result['rows'].append(measure(candidate if protected else baseline,protected,size,repeat))
                    print(json.dumps({'key_count':size,'repeat':repeat,'arm':'protected' if protected else 'legacy','result':'pass'}),flush=True)
        result['complete']=True
    finally:
        # Preserve completed measurements if a later sample fails.
        try:artifact.write(json.dumps(result,indent=2)+'\n')
        finally:artifact.close()

if __name__=='__main__':main()
