"use strict";
const valueFilters = {search:"", kind:"all"};

function bulkValues(view) {
  let input;
  edit("Import .env values", p => {
    text("p", `Target: ${view.profile} / ${view.env || "Common"}. New keys default to secret.`, p);
    const file = field(p, ".env file", "", "file");
    const content = field(p, "Or paste .env content", "", "textarea");
    content.spellcheck = false; content.autocomplete = "off";
    file.onchange = async () => {
      const selected = file.files[0]; if (!selected) { $("editor-save").disabled = false; return; }
      if (selected.size > 500000) { $("editor-error").textContent = "Choose a file smaller than 500 KB."; file.value = ""; $("editor-save").disabled = false; return; }
      $("editor-save").disabled = true;
      try { const value = await selected.text(); if (file.isConnected && file.files[0] === selected) content.value = value; }
      catch { if (file.isConnected && file.files[0] === selected) $("editor-error").textContent = "Could not read this file."; }
      finally { if (file.isConnected && file.files[0] === selected) $("editor-save").disabled = false; }
    };
    const overwrite = field(p, "Overwrite existing values", "", "checkbox");
    text("p", "Keeping existing values also preserves inherited common values. Overwriting writes to the selected environment.", p);
    return () => {
      if (!content.value.trim()) throw Error("Choose a file or paste .env assignments.");
      if (new TextEncoder().encode(content.value).length > 500000) throw Error("Use at most 500 KB of .env content.");
      input = {content:content.value, variables:[], overwrite:overwrite.checked};
      return {env:view.env, import:{...input, preview:true}};
    };
  }, body => valueRequest(view,"import",body), result => {
    edit("Review import", p => {
      text("p", `${view.profile} / ${view.env || "Common"} · ${input.overwrite ? "Overwrite enabled" : "Keep existing values"}`, p);
      text("p", "Select public variables. Secret values stay hidden in this preview. Existing keys retain their kind.", p);
      const rows = result.items || [];
      const controls = rows.map(item => {
        const check = field(p, item.action === "add" ? `${item.key} — Store as public variable` : `${item.key} — ${item.action} · ${item.kind}`, "", "checkbox");
        check.checked = item.kind === "variable"; check.disabled = item.action !== "add";
        return {item, check};
      });
      return () => ({env:view.env, import:{...input, variables:controls.filter(row => row.check.checked).map(row => row.item.key), preview:false}});
    }, body => valueRequest(view,"import",body), () => { input = null; load(); });
    $("editor-save").textContent = "Import values";
  });
  $("editor-save").textContent = "Preview import";
}

function inlineValue(cell, item, view, version) {
  const inherited = !!view.env && item.source === "common";
  cell.replaceChildren();
  const form = text("form", "", cell); form.className = "inline-value";
  const input = document.createElement("input"); input.type = item.kind === "secret" ? "password" : "text";
  input.value = item.value ?? ""; input.setAttribute("aria-label", `New value for ${item.key}`);
  input.autocomplete = "off"; form.append(input);
  if (item.source === "common" && view.env) text("small", `Creates an override in ${view.env}.`, form);
  const actions = text("div", "", form); actions.className = "manage-toolbar";
  const saveLabel = inherited ? "Create override" : "Save";
  const save = button(actions,saveLabel,()=>{}); save.type="submit";
  const cancel = button(actions,"Cancel",()=>load());
  const error = text("p","",form); error.setAttribute("role","alert");
  let pending=null, busy=false;
  form.onsubmit=async event=>{
    event.preventDefault(); if(busy)return;
    pending ||= valueRequest(view,item.kind+".set",{key:item.key,value:input.value,env:view.env});
    busy=true; input.disabled=save.disabled=cancel.disabled=true;
    try { await api("/api/actions",pending); input.value=""; pending=null; if(generation===version)load(); }
    catch(e){
      if(e.responded)pending=null;
      error.textContent=e.message+(pending?" Retry sends the same change.":"");
      input.disabled=!!pending; save.disabled=cancel.disabled=false; save.textContent=pending?"Retry":saveLabel;
    } finally{busy=false;}
  };
  input.onkeydown=e=>{if(e.key==="Escape"&&!busy){e.preventDefault();load();}};
  input.focus();
}

async function loadValues(version) {
  const panel=$("values-panel"), current=profile; panel.replaceChildren();
  if(!current){text("p","Choose a profile above to begin.",panel);return;}
  try {
    const view=await api("/api/values?"+new URLSearchParams({profile:current,env:valueEnv}));
    if(generation!==version)return;
    const toolbar=text("div","",panel);toolbar.className="manage-toolbar value-toolbar";
    const label=text("label","Environment ",toolbar), env=text("select","",label);
    for(const name of ["",...view.envs])text("option",name||"Common",env).value=name;
    env.value=view.env;env.onchange=()=>{valueEnv=env.value;load();};
    button(toolbar,"Create env",()=>edit("Create environment",p=>{const name=field(p,"Name");return()=>({env:name.value});},body=>valueRequest(view,"env.create",body)));
    if(view.env)button(toolbar,"Remove env",()=>edit("Remove empty environment",p=>{text("p",`Remove ${view.env} after removing its overrides.`,p);return()=>({env:view.env});},body=>valueRequest(view,"env.remove",body),()=>{valueEnv="";load();},true));
    button(toolbar,"Add value",()=>edit("Add value",p=>{
      const key=field(p,"Key"),kindLabel=text("label","Kind",p),kind=text("select","",kindLabel);
      for(const name of ["secret","variable"])text("option",name,kind).value=name;
      const value=field(p,"Value","","password");kind.onchange=()=>{value.value="";value.type=kind.value==="secret"?"password":"text";};
      return()=>({action:kind.value+".set",key:key.value,value:value.value,env:view.env});
    },body=>{const {action,...rest}=body;return valueRequest(view,action,rest);}));
    button(toolbar,"Import .env",()=>bulkValues(view));
    const filters=text("div","",panel);filters.className="value-filters";
    const search=field(filters,"Search keys","","search");search.value=valueFilters.search;
    const kindLabel=text("label","Kind ",filters),kind=text("select","",kindLabel);
    for(const [value,label] of [["all","All kinds"],["variable","Variables"],["secret","Secrets"]])text("option",label,kind).value=value;
    kind.value=valueFilters.kind;
    const count=text("p","",filters);count.setAttribute("role","status");
    const table=text("table","",panel),head=text("thead","",table),heading=text("tr","",head);
    for(const title of ["Key","Kind","Value — click to edit","Source",""])text("th",title,heading);
    const body=text("tbody","",table);
    const empty=text("p","No matching keys.",panel);
    function render(){
      body.replaceChildren();
      const items=view.items.filter(item=>item.key.toLowerCase().includes(valueFilters.search.toLowerCase())&&(valueFilters.kind==="all"||item.kind===valueFilters.kind)).sort((a,b)=>a.key.localeCompare(b.key));
      count.textContent=`${items.length} of ${view.items.length} keys`;empty.hidden=!!items.length;
      for(const item of items){
        const row=text("tr","",body);text("td",item.key,row);text("td",item.kind,row);
        const inherited=!!view.env&&item.source==="common";
        row.classList.toggle("value-inherited",inherited);
        const cell=text("td","",row);cell.className="value-cell";
        const edit=button(cell,item.kind==="secret"?"Replace secret":item.value===""?"Empty value":item.value,()=>inlineValue(cell,item,view,version));
        edit.className="value-edit";edit.setAttribute("aria-label",inherited?`Create override for ${item.key} in ${view.env}`:`Edit ${item.key}`);
        edit.title=inherited?`Create an override in ${view.env}`:`Edit ${item.key}`;
        const source=text("td","",row);
        const badge=text("span",!view.env?"Common":inherited?"Inherited common":item.overrides?"Overrides common":"Env only",source);
        badge.className="value-source "+(!view.env?"common":inherited?"inherited":item.overrides?"override":"local");
        const actions=text("td","",row);
        if(!view.env||item.source==="env")button(actions,view.env&&item.overrides?"Remove override":"Delete",()=>editRemoval(item));
      }
    }
    function editRemoval(item){
      const override=!!view.env&&item.overrides;
      edit(`${override?"Remove override for":"Delete"} ${item.key}`,p=>{
        text("p",override?`Remove the value stored in ${view.env}. This environment will inherit the common value again.`:`Delete the value from ${view.env||"Common"}.`,p);
        return()=>({key:item.key,env:view.env});
      },body=>valueRequest(view,item.kind+".unset",body),()=>load(),true);
      $("editor-save").textContent=override?"Remove override":"Delete value";
    }
    search.oninput=()=>{valueFilters.search=search.value;render();};kind.onchange=()=>{valueFilters.kind=kind.value;render();};render();
  }catch(e){if(generation===version)notice(e.message);}
}
