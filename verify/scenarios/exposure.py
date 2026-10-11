"""Packaged auth/origin/provider supervision with a local fake executable."""
import base64
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import uuid

home=Path.home()
binary=home/".local/bin/devtools"
subprocess.run(["sh",os.environ["DEVTOOLS_TEST_INSTALLER"],"install","--version","0.0.0-test.1","--source",os.environ["DEVTOOLS_TEST_RELEASES"]],check=True,capture_output=True,timeout=30)
state=home/"isolated"
fakebin=home/"provider-bin";fakebin.mkdir()
provider=fakebin/"cloudflared"
provider.write_text('''#!/usr/bin/env python3
import sys,time
assert sys.argv[1:4]==['tunnel','--no-autoupdate','--url']
print('https://packaged-fixture.trycloudflare.com',file=sys.stderr,flush=True)
while True:time.sleep(1)
''');provider.chmod(0o700)
env=dict(os.environ,PATH=str(fakebin)+":"+os.environ["PATH"])
env.pop("DEVTOOLS_TASK_CONTEXT",None);env.pop("DEVTOOLS_ISOLATED_ROOT",None)
def api(*args,auth=None,error=None):
 p=subprocess.run([str(binary),"--isolated-root",str(state),*args],cwd=home,env=env,input=auth,text=True,capture_output=True,timeout=40)
 if error:
  assert p.returncode!=0 and json.loads(p.stderr)['error']['code']==error,(args,p.stderr)
  return json.loads(p.stderr)['error']
 assert p.returncode==0,(args,p.stderr)
 return json.loads(p.stdout)["data"]
def mutate(*args,**kwargs):return api(*args,"--request-id",str(uuid.uuid4()),**kwargs)
def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
backend_port,listener_port,tls_port=port(),port(),port()
server=home/"server.py"
server.write_text('''import http.server,json,sys,time
class H(http.server.BaseHTTPRequestHandler):
 protocol_version='HTTP/1.1'
 def do_GET(self):
  if self.path=='/stream':
   self.send_response(200);self.send_header('Transfer-Encoding','chunked');self.end_headers()
   try:
    while True:self.wfile.write(b'6\\r\\nalive\\n\\r\\n');self.wfile.flush();time.sleep(.1)
   except (BrokenPipeError,ConnectionResetError):pass
   return
  b=json.dumps(dict(self.headers)).encode();self.send_response(200);self.send_header('Content-Length',str(len(b)));self.end_headers();self.wfile.write(b)
 def log_message(self,*a):pass
http.server.ThreadingHTTPServer(('127.0.0.1',int(sys.argv[1])),H).serve_forever()
''')
backend_log=home/'backend.log'
backend_output=backend_log.open('w')
backend=subprocess.Popen(["python3",str(server),str(backend_port)],env=env,stdout=subprocess.DEVNULL,stderr=backend_output)
def wait_backend(port):
 deadline=time.monotonic()+10
 while time.monotonic()<deadline:
  c=http.client.HTTPConnection('127.0.0.1',port,timeout=.5)
  try:
   c.request('GET','/');r=c.getresponse();body=r.read()
   assert r.status==200 and isinstance(json.loads(body),dict)
   return
  except (OSError,http.client.HTTPException):time.sleep(.05)
  finally:c.close()
 raise AssertionError(f'Backend {port} never served HTTP; process={backend.poll()}; stderr={backend_log.read_text()}')
def request(origin,auth=None):
 c=http.client.HTTPConnection("127.0.0.1",origin,timeout=3)
 headers={"Host":"packaged-fixture.trycloudflare.com","X-Forwarded-Proto":"spoof","X-Forwarded-Host":"spoof.invalid"}
 if auth:headers['Authorization']='Basic '+base64.b64encode(auth.encode()).decode()
 c.request("GET","/",headers=headers);r=c.getresponse();body=r.read();code=r.status;c.close();return code,body
def stream(origin,auth=None):
 c=http.client.HTTPConnection('127.0.0.1',origin,timeout=3)
 headers={'Host':'packaged-fixture.trycloudflare.com'}
 if auth:headers['Authorization']='Basic '+base64.b64encode(auth.encode()).decode()
 c.request('GET','/stream',headers=headers);r=c.getresponse();assert r.status==200 and r.readline()==b'alive\n';return c,r
def assert_closed(response):
 deadline=time.monotonic()+4
 while time.monotonic()<deadline:
  try:
   if not response.readline():return
  except (http.client.IncompleteRead,ConnectionResetError):return
 raise AssertionError('target stream survived revocation')
project=home/'project';project.mkdir()
project_backend=port()
(project/'devtools.toml').write_text(f'''profile='exposure'
env_files=['.env']
[ports.app]
port={project_backend}
strict=true
[proxies.app]
host='${{DOMAIN:-project.localhost}}'
port='app'
[commands.app]
exec=['python3',{json.dumps(str(server))},'{project_backend}']
inject=false
serve=['app']
''')
(project/'.env').write_text('DOMAIN=project.localhost\n')
try:
 wait_backend(backend_port)
 mutate('proxy','configure','--http-address',f'127.0.0.1:{listener_port}','--https-address',f'127.0.0.1:{tls_port}')
 mutate('proxy','add','app',str(backend_port),'--global','--host','app.localhost')
 mutate('proxy','up','app','--global')
 operation=str(uuid.uuid4())
 first=api('expose','start','app','--global','--provider','cloudflared','--auth-stdin','--request-id',operation,auth='reader:PACKAGED_AUTH_CANARY\n')['item']
 assert first['state']=='active'
 assert request(first['origin_port'])[0]==401
 try:code,body=request(first['origin_port'],'reader:PACKAGED_AUTH_CANARY')
 except Exception:
  print('Exposure request failed; backend process:',backend.poll(),flush=True)
  print('Backend stderr:',backend_log.read_text(),flush=True)
  print('Route metadata:',json.dumps(api('proxy','list')),flush=True)
  print('Exposure metadata:',json.dumps(api('expose','status',first['id'])),flush=True)
  wait_backend(backend_port)
  raise
 observed=json.loads(body)
 assert code==200 and observed['X-Forwarded-Proto']=='https' and observed['X-Forwarded-Host']=='packaged-fixture.trycloudflare.com'
 assert 'Authorization' not in observed
 replay=api('expose','start','app','--global','--provider','cloudflared','--auth-stdin','--request-id',operation,auth='reader:PACKAGED_AUTH_CANARY\n')
 assert replay['replayed'] and replay['item']['id']==first['id']
 api('expose','start','app','--global','--provider','cloudflared','--auth-stdin','--request-id',operation,auth='reader:CHANGED_AUTH_CANARY\n',error='request_conflict')
 mutate('proxy','down','app','--global',error='pinned')
 second=mutate('expose','start','app','--global','--provider','cloudflared','--no-auth')['item']
 first_connection,first_stream=stream(first['origin_port'],'reader:PACKAGED_AUTH_CANARY')
 second_connection,second_stream=stream(second['origin_port'])
 mutate('expose','stop',first['id'])
 assert_closed(first_stream)
 assert second_stream.readline()==b'alive\n','sibling stream was revoked'
 first_connection.close();second_connection.close()
 assert api('expose','status',first['id'])['item']['state']=='stopped'
 assert request(second['origin_port'])[0]==200
 # Project and global routes with the same name retain independent identities.
 mutate('project','up','app','--proxy','--dir',str(project))
 wait_backend(project_backend)
 for source in (project/'.env',project/'devtools.toml'):
  mutate('proxy','up','app','--dir',str(project))
  item=mutate('expose','start','app','--dir',str(project),'--provider','cloudflared','--no-auth')['item']
  assert item['route_ref']['scope']=='project' and second['route_ref']['scope']=='global'
  target_connection,target_stream=stream(item['origin_port'])
  sibling_connection,sibling_stream=stream(second['origin_port'])
  source.write_text(source.read_text()+'\n# source changed\n')
  assert_closed(target_stream)
  assert sibling_stream.readline()==b'alive\n','global sibling was revoked by project source change'
  target_connection.close();sibling_connection.close()
  for _ in range(60):
   if api('expose','status',item['id'])['item']['state']=='recovery_required':break
   time.sleep(.05)
  assert api('expose','status',item['id'])['item']['state']=='recovery_required'
  mutate('expose','stop',item['id'])
 mutate('project','down','--dir',str(project))
 # Auth is absent from all persisted state, including provider recovery receipts.
 assert all(b'PACKAGED_AUTH_CANARY' not in p.read_bytes() and b'CHANGED_AUTH_CANARY' not in p.read_bytes() for p in state.rglob('*') if p.is_file())
 mutate('expose','stop',second['id'])
 lost_operation=str(uuid.uuid4())
 third=api('expose','start','app','--global','--provider','cloudflared','--auth-stdin','--request-id',lost_operation,auth='reader:PACKAGED_AUTH_CANARY\n')['item']
 control=json.loads((state/'data'/'proxy'/'control.json').read_text())
 from urllib.parse import urlparse
 address=urlparse(control['address'])
 connection=http.client.HTTPConnection(address.hostname,address.port,timeout=5)
 connection.request('POST','/stop',headers={'Authorization':'Bearer '+control['token']})
 response=connection.getresponse();response.read();assert response.status==200;connection.close()
 for _ in range(80):
  if not api('proxy','status')['item']['running']:break
  time.sleep(.05)
 assert api('expose','status',third['id'])['item']['state']=='auth_required'
 replay=api('expose','start','app','--global','--provider','cloudflared','--auth-stdin','--request-id',lost_operation,auth='reader:PACKAGED_AUTH_CANARY\n')
 assert replay['replayed'] and replay['item']['state']=='auth_required'
 mutate('proxy','start')
 mutate('expose','stop',third['id'])
 markers=list((state/'data'/'exposure-providers').glob('*.json'))
 assert len(markers)==5 and all(json.loads(p.read_text())['state']=='stopped' for p in markers)
 print('Packaged provider supervision, auth loss/replay/redaction, route scope, one-second source watcher, selected stream revocation and HTTPS authority verified; live provider acceptance remains separate')
finally:
 for item in api('expose','list')['items']:
  if item['state']!='stopped':mutate('expose','stop',item['id'])
 mutate('project','down','--dir',str(project))
 plan=api('proxy','teardown','--all')['item'];mutate('proxy','teardown','--all','--apply',plan['digest'])
 backend.terminate();backend.wait(timeout=5);backend_output.close()
