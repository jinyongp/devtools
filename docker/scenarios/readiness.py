"""Installed readiness probes: snapshots, waiting, cancellation and API access."""
import concurrent.futures
import json
import os
from pathlib import Path
import signal
import subprocess
import time
import urllib.parse
import urllib.request
import uuid

home=Path.home();root=home/'project';root.mkdir()
env=dict(os.environ,PATH=str(home/'.local/bin')+':'+os.environ['PATH'])
canary='readiness-fixture-secret'
def execute(*args,input=None,expected=0):
    p=subprocess.run(args,cwd=root,env=env,input=input,text=True,capture_output=True,timeout=40)
    assert p.returncode==expected,(args,p.stdout,p.stderr)
    assert canary not in p.stdout+p.stderr
    return json.loads(p.stdout if expected==0 else p.stderr)
execute('sh',os.environ['DEVTOOLS_TEST_INSTALLER'],'install','--version','0.0.0-test.1','--source',os.environ['DEVTOOLS_TEST_RELEASES'])
def api(*args,**kw):return execute('devtools',*args,**kw)['data' if kw.get('expected',0)==0 else 'error']
def mutate(action,*args):return api('process',action,*args,'--request-id',str(uuid.uuid4()))['item']
config='''profile="readiness"
[ports.web]
range=[27100,27199]
[commands.web]
exec=["python3","server.py"]
inject=true
serve=["web"]
[commands.web.bind]
PORT={port="web"}
[commands.web.ready]
exec=["python3","probe.py"]
timeout="2s"
[commands.plain]
exec=["python3","-c","import time;time.sleep(60)"]
[commands.slow]
exec=["python3","-c","import time;time.sleep(60)"]
[commands.slow.ready]
exec=["python3","-c","import time;time.sleep(60)"]
timeout="100ms"
'''
(root/'devtools.toml').write_text(config)
(root/'server.py').write_text('''import http.server,os,socketserver
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200);self.end_headers();self.wfile.write(b'OK')
 def log_message(self,*args):pass
socketserver.TCPServer.allow_reuse_address=True
socketserver.TCPServer(('127.0.0.1',int(os.environ['PORT'])),Handler).serve_forever()
''')
(root/'probe.py').write_text('''import os,time,urllib.request
from pathlib import Path
fd=os.open('probe.lock',os.O_CREAT|os.O_EXCL|os.O_WRONLY)
try:
 with open('checks','a') as f:f.write('check\\n')
 print(os.environ['TOKEN'])
 time.sleep(.05)
 assert os.environ['PUBLIC']=='initial'
 assert Path('ready').exists()
 assert urllib.request.urlopen('http://127.0.0.1:'+os.environ['PORT']+'/health',timeout=.5).read()==b'OK'
finally:
 os.close(fd);os.unlink('probe.lock')
''')
api('var','set','PUBLIC','--value','initial')
api('sec','set','TOKEN','--stdin',input=canary)
live=[]
try:
    item=mutate('start','web');live.append(item['id']);id=item['id']
    assert item['ready_configured']
    api('process','status',id);api('process','list')
    assert not (root/'checks').exists(), 'Metadata query executed a probe'
    assert not api('process','check',id)['readiness']['ready']
    assert api('process','wait',id,'--timeout','100ms',expected=3)['code']=='readiness_timeout'
    assert api('process','status',id)['state']=='running'
    (root/'ready').touch()
    ready=api('process','wait',id,'--timeout','5s')['readiness']
    assert ready['ready'] and ready['exit_code']==0 and ready['checked_at']
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        results=list(pool.map(lambda _:api('process','check',id),range(4)))
    assert all(result['readiness']['ready'] for result in results)
    api('var','set','PUBLIC','--value','changed')
    changed=config.replace('exec=["python3","probe.py"]','exec=["sh","-c","exit 9"]')
    (root/'devtools.toml').write_text(changed)
    assert api('process','check',id)['readiness']['ready'], 'Running execution lost its snapshot'
    url=urllib.parse.urlsplit(api('dashboard')['url']);origin=f'{url.scheme}://{url.netloc}'
    def http(path,body,token=''):
        headers={'Origin':origin,'Content-Type':'application/json'}
        if token:headers['Authorization']='Bearer '+token
        request=urllib.request.Request(origin+path,data=json.dumps(body).encode(),headers=headers)
        return json.load(urllib.request.urlopen(request,timeout=10))
    token=http('/session',{'token':urllib.parse.parse_qs(url.fragment)['token'][0]})['token']
    action={'domain':'process','profile':'readiness','process':{'action':'check','id':id}}
    assert http('/api/actions',action,token)['data']['readiness']['ready']
    count=(root/'checks').read_text()
    try:
        http('/api/actions',{**action,'profile':'different'},token)
        raise AssertionError('Cross-profile readiness accepted')
    except urllib.error.HTTPError as error:
        assert error.code==400
    assert (root/'checks').read_text()==count
    next_item=mutate('restart',id);live.append(next_item['id'])
    assert api('process','check',next_item['id'])['readiness']['exit_code']==9
    assert api('process','check',id,expected=3)['code']=='process_not_running'
    assert api('process','wait',next_item['id'],'--timeout','100ms',expected=3)['code']=='readiness_timeout'
    plain=mutate('start','plain');live.append(plain['id'])
    assert api('process','check',plain['id'],expected=3)['code']=='readiness_not_configured'
    slow=mutate('start','slow');live.append(slow['id'])
    assert api('process','check',slow['id'])['readiness']['reason']=='probe_timeout'
    waiter=subprocess.Popen(['devtools','process','wait',slow['id'],'--timeout','10s'],cwd=root,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
    time.sleep(.2);waiter.send_signal(signal.SIGTERM)
    out,err=waiter.communicate(timeout=5)
    assert waiter.returncode==130 and canary not in out+err,(out,err)
    assert api('process','status',slow['id'])['state']=='running'
    waiter=subprocess.Popen(['devtools','process','wait',slow['id'],'--timeout','10s'],cwd=root,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
    time.sleep(.05)
    mutate('stop',slow['id'])
    out,err=waiter.communicate(timeout=5)
    assert waiter.returncode==3 and json.loads(err)['error']['code'] in ('process_not_running','readiness_unavailable'),(out,err)
finally:
    for id in live:
        api('process','stop',id,'--request-id',str(uuid.uuid4()))
    api('dashboard','stop')
print('Installed readiness checks, delayed success, timeout, cancellation, snapshots, serialization and dashboard verified')
