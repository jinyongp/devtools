"use strict";
let valueEnv = "";
const executionContexts = new Map();

function field(parent, label, initial = "", type = "text") {
  const wrapper = text("label", label, parent);
  const input = document.createElement(type === "textarea" ? "textarea" : "input");
  if (type !== "textarea") input.type = type;
  input.value = initial; input.autocomplete = "off";
  if (type === "password") input.autocomplete = "new-password";
  wrapper.append(input); return input;
}
function button(parent, label, action) {
  const b = text("button", label, parent); b.type = "button"; b.onclick = action; return b;
}
function edit(title, build, request, after = () => load(), destructive = false) {
  const dialog = $("editor"), form = $("edit-form"), fields = $("editor-fields");
  fields.replaceChildren(); $("editor-title").textContent = title;
  $("editor-target").textContent = `Profile: ${profile || "new selection"}`;
  $("editor-error").textContent = "";
  $("editor-save").textContent = destructive ? "Confirm change" : "Save";
  $("editor-save").disabled = false; $("editor-cancel").disabled = false;
  const read = build(fields);
  let pending = null, busy = false;
  const controls = () => Array.from(form.querySelectorAll("input,textarea,select,button"));
  const fixed = new Set(controls().filter(c => c.disabled));
  dialog.oncancel = event => { if (busy) event.preventDefault(); };
  $("editor-cancel").onclick = () => dialog.close();
  dialog.onclose = () => { pending = null; fields.replaceChildren(); form.onsubmit = null; };
  form.onsubmit = async event => {
    event.preventDefault(); if (busy) return;
    try {
      if (!pending) pending = request(read());
      busy = true; controls().forEach(c => c.disabled = true);
      const response = pending.local ? pending.local() : (await api("/api/actions", pending)).data;
      pending = null; busy = false;
      await new Promise(resolve => { dialog.addEventListener("close", resolve, {once:true}); dialog.close(); });
      await after(response);
    } catch (error) {
      busy = false;
      if (error.responded) pending = null;
      $("editor-error").textContent = error.message + (pending ? " Retry sends the same request." : " Reopen after reloading if the profile changed.");
      controls().forEach(c => c.disabled = fixed.has(c) || (!!pending && c.id !== "editor-save" && c.id !== "editor-cancel"));
      $("editor-save").textContent = pending ? "Retry" : "Save";
    }
  };
  dialog.showModal(); fields.querySelector("input,textarea,select")?.focus();
}
function valueRequest(view, action, body) {
  return {domain:"values", profile:view.profile, change:{action, revision:view.revision, request_id:crypto.randomUUID(), ...body}};
}
async function loadValues(version) {
  const panel = $("values-panel"); panel.replaceChildren();
  if (!profile) {text("p", "Choose a profile above to begin.", panel); return;}
  try {
    const view = await api("/api/values?" + new URLSearchParams({profile, env:valueEnv}));
    if (generation !== version) return;
    const toolbar = text("div", "", panel); toolbar.className = "manage-toolbar";
    const label = text("label", "Environment ", toolbar), select = document.createElement("select"); label.append(select);
    for (const env of ["", ...view.envs]) {const option = text("option", env || "Common", select); option.value = env;}
    select.value = view.env; select.onchange = () => {valueEnv = select.value; load();};
    button(toolbar, "Create env", () => edit("Create environment", p => {const input=field(p,"Name");return()=>({env:input.value});}, body=>valueRequest(view,"env.create",body)));
    if(view.env) button(toolbar,"Remove env",()=>edit("Remove empty environment",p=>{text("p",`Remove ${view.env}. Remove its overrides first.`,p);return()=>({env:view.env});},body=>valueRequest(view,"env.remove",body),()=>{valueEnv="";load();},true));
    const setValue = item => edit(item ? `Edit ${item.key}` : "Add value", p => {
      const key=field(p,"Key",item?.key || ""); if(item) key.readOnly=true;
      const label=text("label","Kind",p), kind=document.createElement("select"); label.append(kind);
      for(const k of ["variable","secret"]){const o=text("option",k,kind);o.value=k;}
      kind.value=item?.kind || "variable"; if(item) kind.disabled=true;
      const value=field(p,item?.kind==="secret"?"New secret value":"Value",item?.value??"",kind.value==="secret"?"password":"text");
      kind.onchange=()=>{value.value="";value.type=kind.value==="secret"?"password":"text";};
      text("p",`Writes to ${view.env || "common"}. Secret values are available for replacement only.`,p);
      return()=>({action:kind.value+".set",key:key.value,value:value.value,env:view.env});
    },body=>{const {action,...rest}=body;return valueRequest(view,action,rest);});
    button(toolbar,"Add value",()=>setValue(null));
    const table=document.createElement("table");panel.append(table);
    const head=document.createElement("thead"), row=document.createElement("tr");head.append(row);table.append(head);
    for(const title of ["Key","Kind","Value","Source","Actions"])text("th",title,row);
    const body=document.createElement("tbody");table.append(body);
    for(const item of view.items){
      const row=document.createElement("tr");body.append(row);
      for(const value of [item.key,item.kind,item.kind==="secret"?"Stored":item.value,item.source])text("td",value,row);
      const actions=text("td","",row);button(actions,"Edit",()=>setValue(item));
      if(!view.env || item.source==="env") button(actions,"Remove",()=>edit(`Remove ${item.key}`,p=>{text("p",`Remove ${item.key} from ${view.env || "common"}. ${item.overrides?"The common value will apply afterward.":""}`,p);return()=>({key:item.key,env:view.env});},body=>valueRequest(view,item.kind+".unset",body),()=>load(),true));
    }
    if(!view.items.length)text("p","No values in this scope. Add a value or create an environment.",panel);
  }catch(error){if(generation===version)notice(error.message);}
}

function taskRequest(action, target, body, rev, extra = {}, targetProfile = profile) {
  return {domain:"task",profile:targetProfile,action,target,body,options:{"request-id":crypto.randomUUID(),"if-revision":String(rev),...extra}};
}
function createItem() {
  const current=profile, workstream=scope==="workstreams", owner=ws;
  edit(workstream?"Create workstream":"Create task",p=>{const title=field(p,"Title"),description=field(p,"Description","","textarea");return()=>({title:title.value,description:description.value,...(!workstream&&owner?{workstream_id:owner}:{})});},body=>taskRequest(workstream?"workstream.create":"task.add","",body,revision,{},current));
}
function itemActions(node, data, parent) {
  const current=profile, rev=data.revision, item=data.item || node;
  const actions=text("div","",parent);actions.className="manage-toolbar";
  const send=(action,body,extra={})=>taskRequest(action,node.id,body,rev,extra,current);
  if (["done", "canceled", "closed"].includes(item.state || node.state)) {
    button(actions,"Reopen",()=>edit(`Reopen ${node.kind}`,p=>{const reason=field(p,"Reason","","textarea");return()=>({reason:reason.value});},body=>send(`${node.kind}.reopen`,body),()=>load(),true));
    return;
  }
  const menu=text("details","",parent);menu.className="item-management";
  text("summary","Manage item",menu);
  const secondary=text("div","",menu);secondary.className="manage-toolbar";
  if(node.kind==="task") button(secondary,"Edit",()=>edit("Edit task",p=>{const title=field(p,"Title",item.title),description=field(p,"Description",item.description,"textarea");return()=>({title:title.value,description:description.value});},body=>send("task.update",body)));
  button(secondary,"Dependencies",()=>edit("Edit prerequisites",p=>{const ids=field(p,"Prerequisite IDs (one per line)",(item.depends_on||[]).join("\n"),"textarea");text("p","Dependencies use the existing workstream and cycle checks.",p);return()=>({depends_on:ids.value.split(/\s+/).filter(Boolean)});},body=>send(node.kind==="workstream"?"workstream.depends":"task.depends",body)));
  if(node.kind==="workstream") {
    for(const [label,action,name] of [["Specification","spec.set","spec"],["Plan","plan.set","plan"]])button(secondary,label,()=>edit(`Edit ${label.toLowerCase()}`,p=>{
      const doc=data.documents?.[name]||{},body=field(p,"Document",doc.body||"","textarea");
      const fields=action==="spec.set"?["requirements","acceptance"]:["task_ids","validation_ids"];
      const inputs=fields.map(key=>field(p,key.replaceAll("_"," ")+(action==="spec.set"?" (JSON array of key/text entries)":" (one ID per line)"),action==="spec.set"?JSON.stringify(doc[key]||[],null,2):(doc[key]||[]).join("\n"),"textarea"));
      return()=>{const out={body:body.value};fields.forEach((key,i)=>out[key]=action==="spec.set"?JSON.parse(inputs[i].value):inputs[i].value.split(/\s+/).filter(Boolean));return out;};
    },body=>send(action,body)));
    for(const [label,action] of [["Activate","workstream.activate"],["Close workstream","workstream.close"]].filter(([, action]) => action === ((item.state || node.state) === "draft" ? "workstream.activate" : "workstream.close")))button(actions,label,()=>edit(label,p=>{text("p",`${label}: ${node.title}. Coverage and completion checks apply.`,p);return()=>({});},body=>send(action,body),()=>load(),true));
  }
  for(const [label,suffix] of [["Cancel","cancel"]])button(secondary,label,()=>edit(`${label} ${node.kind}`,p=>{const reason=field(p,"Reason","","textarea");return()=>({reason:reason.value});},body=>send(`${node.kind}.${suffix}`,body),()=>load(),true));
  if(node.kind!=="task")return;
  const key=current+":"+node.id,run=item.current_run;
  const saved=executionContexts.get(key),owned=run && saved?.run===run.id ? saved : null;
  const accept=result=>{if(result.context&&result.run)executionContexts.set(key,{context:result.context,run:result.run.id});load();};
  if(!run && !node.blockers?.length) button(actions,"Claim",()=>edit("Claim task",p=>{text("p",`Claim ${node.title} for this browser tab.`,p);return()=>({});},body=>send("run.claimed",body),accept));
  if(run && !owned)button(actions,"Take over",()=>edit("Take over task",p=>{text("p","This invalidates the previous execution context. Review the latest checkpoint before continuing.",p);return()=>({});},body=>send("run.taken_over",body,{"expected-run":run.id}),accept,true));
  if(owned)for(const [label,action]of[["Checkpoint","run.checkpointed"],["Release","run.released"],["Complete","task.completed"]])button(actions,label,()=>edit(label,p=>{const summary=field(p,"Summary","","textarea");return()=>({summary:summary.value});},body=>taskRequest(action,action==="task.completed"?node.id:owned.run,body,rev,{context:owned.context},current),()=>{if(action!=="run.checkpointed")executionContexts.delete(key);load();},action!=="run.checkpointed"));
}
$("values-nav").onclick=()=>{scope="values";ws="";valueEnv="";load();};
$("create-item").onclick=createItem;
