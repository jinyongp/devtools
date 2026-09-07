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

function taskRequest(action, target, body, rev, extra = {}, targetProfile = profile) {
  return {domain:"task",profile:targetProfile,action,target,body,options:{"request-id":crypto.randomUUID(),"if-revision":String(rev),...extra}};
}
function createItem() {
  const current=profile, workstream=scope==="workstreams", owner=ws;
  edit(workstream?"Create workstream":"Create task",p=>{const title=field(p,"Title"),description=field(p,"Description","","textarea");return()=>({title:title.value,description:description.value,...(!workstream&&owner?{workstream_id:owner}:{})});},body=>taskRequest(workstream?"workstream.create":"task.add","",body,revision,{},current));
}
$("values-nav").onclick=()=>{scope="values";ws="";valueEnv="";load();};
$("create-item").onclick=createItem;
