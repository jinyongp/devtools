import {test} from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const source=readFileSync(new URL("../internal/dashboard/assets/network.js",import.meta.url),"utf8");
function fixture() {
  const nodes=[],buttons=[],requests=[];
  const state={ca:{present:true,not_after:"2036-01-01"},trusted:false,isolated:true,
    listeners:[{name:"preview",state:"running",running:true,url:"https://app.localhost:18443"}],
    exposures:[{id:"exposure-id",provider:"lan",url:"https://app.local:18443",state:"active",auth:true,client_setup:["Configure client DNS."]}]};
  const context=vm.createContext({generation:1,URLSearchParams,crypto:{randomUUID:()=>"request-id"},
    text:(tag,value,parent)=>{const node={tag,value,parent};nodes.push(node);return node;},
    button:(parent,label,click)=>{buttons.push({label,click});},load:()=>{},
    edit:(label,fields,build)=>requests.push(build(fields({})())),
    api:async()=>({state,routes:[{route:{scope:"global",host:"app.localhost",status:"ready"},endpoint:{route_url:"https://app.localhost:18443"}}]})});
  vm.runInContext(source,context);
  return {context,nodes,buttons,requests};
}
test("network panel shows client setup and targets only the selected resource",async()=>{
  const f=fixture();await f.context.loadNetwork({},"app",1);
  assert.ok(f.nodes.some(n=>n.value.includes("client trust required")));
  assert.ok(f.nodes.some(n=>n.value.includes("Configure client DNS")));
  f.buttons.find(b=>b.label==="Stop listener preview").click();
  f.buttons.find(b=>b.label==="Stop lan exposure").click();
  assert.equal(f.requests[0].network.listener,"preview");
  assert.equal(f.requests[0].network.action,"stop");
  assert.equal(f.requests[1].network.id,"exposure-id");
  assert.equal(f.requests[1].network.action,"exposure.stop");
  assert.ok(f.requests.every(r=>r.profile==="app"&&r.network.request_id==="request-id"));
});
test("stale network response cannot add controls to a different profile view",async()=>{
  const f=fixture();await f.context.loadNetwork({},"app",0);assert.equal(f.buttons.length,0);
});
