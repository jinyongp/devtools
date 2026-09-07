"use strict";
let valueEnv = "";

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
$("values-nav").onclick=()=>{scope="values";ws="";valueEnv="";load();};
$("create-item").onclick=createItem;
