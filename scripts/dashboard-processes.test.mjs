import {test} from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const source=readFileSync(new URL("../internal/dashboard/assets/processes.js",import.meta.url),"utf8");
function fixture() {
  const nodes=[],buttons=[],reads=[],writes=[];
  const refreshReads=new Map(),detail={item:{id:"execution-id",selection:"all"}};
  const elements=new Map();
  const node=(tag,value="",parent)=>{
    const result={tag,value,parent,classList:{toggle(){}},setAttribute(name,value){this[name]=value;},replaceChildren(){}};
    nodes.push(result);return result;
  };
  const context=vm.createContext({generation:1,profile:"example",scope:"processes",ws:"",projectsExpanded:false,
    refreshReads,rememberRefreshRead:(path,data)=>refreshReads.set(path,data),
    expandedProcesses:new Set(),URLSearchParams,crypto:{randomUUID:()=>"new-request"},
    $:id=>{if(!elements.has(id))elements.set(id,node("div"));return elements.get(id);},text:node,
    button:(parent,label,click)=>{buttons.push({label,click});},saveNavigation(){},notice:message=>assert.fail(message),
    edit:(_label,fields,build)=>writes.push(build(fields({})())),loadNetwork:async()=>{},
    api:async path=>{
      reads.push(path);
      if(path.startsWith("/api/process?"))return detail;
      return {instances:[{directory:"/project",instance_id:"instance",alias:"Project"}],next_cursor:"snapshot-page-2",
        items:[{id:"SERVICE-KEY",kind:"service",command:"web",directory:"/project",current_state:"running",active_execution_ids:["execution-id"],last_attempt:{action:"restart",state:"failed",execution_id:"failed-execution"}}]};
    }});
  vm.runInContext(source,context);
  context.load=()=>context.loadProcesses(1);
  return {context,nodes,buttons,reads,writes,refreshReads,detail};
}

test("current services use actual execution IDs and load execution detail on demand",async()=>{
  const f=fixture();await f.context.loadProcesses(1);
  const query=new URL(f.reads[0],"http://test").searchParams;
  assert.equal(query.get("profile"),"example");assert.equal(query.get("view"),"current");assert.equal(query.get("limit"),"50");
  assert.equal(f.reads.length,1);
  f.buttons.find(b=>b.label==="Stop").click();
  assert.equal(f.writes[0].process.id,"execution-id");assert.equal(f.writes[0].process.request_id,"new-request");
  const details=f.nodes.find(n=>n.className==="process-details");details.open=true;details.ontoggle();
  await new Promise(setImmediate);
  assert.ok(f.reads.some(path=>path.includes("id=execution-id")));
  assert.ok(f.reads.every(path=>!path.includes("SERVICE-KEY")&&!path.includes("failed-execution")));
  assert.equal(f.refreshReads.size,1);
  details.open=false;details.ontoggle();
  assert.equal(f.refreshReads.size,0);
  details.open=true;details.ontoggle();await new Promise(setImmediate);
  assert.equal(f.reads.filter(path=>path.startsWith("/api/process?")).length,2);
  assert.equal(f.refreshReads.size,1);
});

test("closing and reopening details excludes the earlier response from rendering and polling",async()=>{
  const f=fixture(),original=f.context.api,pending=[];
  f.context.api=path=>path.startsWith("/api/process?")?new Promise(resolve=>pending.push(resolve)):original(path);
  await f.context.loadProcesses(1);
  const details=f.nodes.find(n=>n.className==="process-details");
  details.open=true;details.ontoggle();
  details.open=false;details.ontoggle();
  details.open=true;details.ontoggle();
  pending[0](f.detail);await new Promise(setImmediate);
  assert.equal(f.refreshReads.size,0);
  assert.ok(!f.nodes.some(n=>n.value==="Execution execution-id"));
  pending[1](f.detail);await new Promise(setImmediate);
  assert.equal(f.refreshReads.size,1);
  assert.ok(f.nodes.some(n=>n.value==="Execution execution-id"));
});

test("lazy execution details retain watcher, paused replacement and diagnostic explanations",async()=>{
  const f=fixture();
  f.detail.item.value_watch={state:"verification_pending",reason:"vault_locked"};
  f.detail.item.value_apply={state:"failed",reason:"requirements_failed",next_id:"replacement-id"};
  f.detail.values={state:"verification_pending",reason:"vault_locked",diagnostic:{details:{reason:"vault_locked",retry:"after_dependency_change"}}};
  await f.context.loadProcesses(1);
  const details=f.nodes.find(n=>n.className==="process-details");details.open=true;details.ontoggle();await new Promise(setImmediate);
  const text=f.nodes.map(n=>n.value).join("\n");
  assert.match(text,/Background value check: verification pending/);
  assert.match(text,/Automatic updates paused\. Fix the cause and use Restart to retry explicitly\./);
  assert.match(text,/Replacement execution: replacement-id/);
  assert.match(text,/Storage: vault locked · after dependency change/);
});

test("history and snapshot controls preserve selection and reset on profile changes",async()=>{
  const f=fixture();await f.context.loadProcesses(1);
  const view=f.nodes.find(n=>n["aria-label"]==="Process view");view.value="history";view.onchange();
  await Promise.resolve();
  assert.equal(new URL(f.reads.at(-1),"http://test").searchParams.get("view"),"history");
  f.buttons.find(b=>b.label==="Next page").click();await Promise.resolve();
  assert.equal(new URL(f.reads.at(-1),"http://test").searchParams.get("cursor"),"snapshot-page-2");
  f.context.profile="different";await f.context.loadProcesses(1);
  const query=new URL(f.reads.at(-1),"http://test").searchParams;
  assert.equal(query.get("profile"),"different");assert.equal(query.get("cursor"),null);
});

test("stale process response cannot publish controls to another generation",async()=>{
  const f=fixture();await f.context.loadProcesses(0);assert.equal(f.buttons.length,0);
});
