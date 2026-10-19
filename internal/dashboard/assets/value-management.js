"use strict";
const valueFilters = {search:"", kind:"all", source:"all"};

function secretNameHint(parent,key,kind,view) {
  const hint=text("p","",parent);hint.className="value-warning";hint.setAttribute("role","status");
  const update=(name,type)=>{
    const exposed=type==="secret"&&(view.public_prefixes||[]).some(prefix=>name.toUpperCase().startsWith(prefix));
    const message=exposed ? "This name may be published by frontend builds. Keep credentials out of browser bundles." : "";
    if(hint.textContent!==message)hint.textContent=message;
    hint.hidden=!exposed;
  };
  update(key,kind);return update;
}

function variableValue(parent, label, original = "") {
  const input = field(parent, label, original, "textarea");
  input.spellcheck = false;
  // Textarea display normalizes CRLF/CR; saving untouched content must retain
  // the stored bytes, including those original line endings.
  const displayed = input.value;
  return {input, read: () => input.value === displayed ? original : input.value};
}

function overrideValue(item, view, version) {
  edit(`Override ${item.key}`, p => {
    text("p", "Choose the environment that will store its own value. Common stays unchanged.", p);
    const label = text("label", "Environment", p), target = text("select", "", label);
    for (const name of view.envs) text("option", name, target).value = name;
    return () => target.value;
  }, env => ({local:() => env}), async (env, stillCurrent) => {
    try {
      const target = await api("/api/values?" + new URLSearchParams({profile:view.profile, env}));
      if (generation !== version || !stillCurrent()) return;
      const existing = target.items.find(value => value.key === item.key);
      const replacing = existing?.source === "env";
      const kind = existing?.kind || item.kind;
      edit(`${replacing ? "Edit override" : "Create override"}: ${item.key}`, p => {
        text("p", `Target: ${view.profile} / ${env}. ${replacing ? "Saving replaces this environment’s existing override." : "Common stays unchanged."}`, p);
        secretNameHint(p,item.key,kind,target);
        const value = kind === "secret" ? field(p, "New secret value", "", "password") :
          variableValue(p, "Value", existing?.value ?? item.value ?? "");
        return () => ({key:item.key, env, value:kind === "secret" ? value.value : value.read()});
      }, body => valueRequest(target, kind + ".set", body), () => { valueEnv = env; valueFilters.source = "all"; load(); });
      $("editor-save").textContent = replacing ? "Save override" : "Create override";
    } catch (error) { if (generation === version && stillCurrent()) notice(error.message); }
  });
  $("editor-save").textContent = "Continue";
}

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
      for(const warning of result.warnings||[])text("p",`${warning.key}: ${warning.message}`,p).className="value-warning";
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
  secretNameHint(form,item.key,item.kind,view);
  const input = document.createElement(item.kind === "secret" ? "input" : "textarea");
  if (item.kind === "secret") input.type = "password";
  const original = item.value ?? "";
  input.value = original; const displayed = input.value;
  input.spellcheck = false; input.setAttribute("aria-label", `New value for ${item.key}`);
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
    pending ||= valueRequest(view,item.kind+".set",{key:item.key,value:input.value===displayed?original:input.value,env:view.env});
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
  try {
    const vaultState=await api("/api/vault");
    if(generation!==version)return;
    renderVaultState(panel,vaultState);
    if(!current){text("p","Choose a profile above to begin.",panel);return;}
    const view=await api("/api/values?"+new URLSearchParams({profile:current,env:valueEnv}));
    if(generation!==version)return;
    text("p",view.integrity_verified ? "Storage verified" : "Metadata view · storage integrity not checked",panel).className="value-count";
    const blocked=["locked_manual","locking","recovery_required"].includes(vaultState.state)||(vaultState.state==="locked"&&vaultState.backend==="passphrase");
    const mutationButton=(parent,label,action)=>{
      const control=button(parent,label,action);control.disabled=blocked;
      if(blocked)control.title="Unlock or recover the vault to change values.";
      return control;
    };
    const toolbar=text("div","",panel);toolbar.className="manage-toolbar value-toolbar";
    const label=text("label","Environment ",toolbar), env=text("select","",label);
    for(const name of ["",...view.envs])text("option",name||"Common",env).value=name;
    env.value=view.env;env.onchange=()=>{valueEnv=env.value;load();};
    mutationButton(toolbar,"Create env",()=>edit("Create environment",p=>{const name=field(p,"Name");return()=>({env:name.value});},body=>valueRequest(view,"env.create",body)));
    if(view.env)mutationButton(toolbar,"Remove env",()=>edit("Remove empty environment",p=>{text("p",`Remove ${view.env} after removing its overrides.`,p);return()=>({env:view.env});},body=>valueRequest(view,"env.remove",body),()=>{valueEnv="";load();},true));
    mutationButton(toolbar,"Add value",()=>edit("Add value",p=>{
      const key=field(p,"Key"),kindLabel=text("label","Kind",p),kind=text("select","",kindLabel);
      for(const name of ["secret","variable"])text("option",name,kind).value=name;
      const secret=field(p,"Value","","password"),variable=variableValue(p,"Value").input;
      variable.parentElement.hidden=true;
      const warn=secretNameHint(p,"",kind.value,view);
      key.addEventListener("input",()=>warn(key.value,kind.value));
      kind.onchange=()=>{
        secret.value=variable.value="";
        const isSecret=kind.value==="secret";
        secret.parentElement.hidden=!isSecret;
        variable.parentElement.hidden=isSecret;
        warn(key.value,kind.value);
      };
      return()=>({action:kind.value+".set",key:key.value,value:(kind.value==="secret"?secret:variable).value,env:view.env});
    },body=>{const {action,...rest}=body;return valueRequest(view,action,rest);}));
    mutationButton(toolbar,"Import .env",()=>bulkValues(view));
    const table=text("table","",panel);table.className="values-table";
    const head=text("thead","",table),heading=text("tr","",head);
    const columns=["Key","Kind","Value — click to edit","Source", "Actions"].map(title=>{
      const cell=text("th","",heading);cell.scope="col";text("span",title,cell);return cell;
    });
    const search=text("input","",columns[0]);search.type="search";search.placeholder="Search keys";search.setAttribute("aria-label","Search keys");search.value=valueFilters.search;
    const kind=text("select","",columns[1]);kind.setAttribute("aria-label","Filter by kind");
    for(const [value,label] of [["all","All kinds"],["variable","Variables"],["secret","Secrets"]])text("option",label,kind).value=value;
    kind.value=valueFilters.kind;
    const sourceFilter=text("select","",columns[3]);sourceFilter.setAttribute("aria-label","Filter by source");
    const sources=view.env?[["all","All sources"],["inherited","Inherited common"],["override","Overrides common"],["local","Env only"]]:[["all","All sources"],["common","Common"]];
    if(!sources.some(([value])=>value===valueFilters.source)){valueFilters.source="all";saveNavigation();}
    for(const [value,label] of sources)text("option",label,sourceFilter).value=value;
    sourceFilter.value=valueFilters.source;
    const sourceOf=item=>!view.env?"common":item.source==="common"?"inherited":item.overrides?"override":"local";
    const body=text("tbody","",table);
    const count=text("p","",panel);count.className="value-count";count.setAttribute("role","status");
    const empty=text("p","No matching keys.",panel);
    function render(){
      body.replaceChildren();
      const items=view.items.filter(item=>item.key.toLowerCase().includes(valueFilters.search.toLowerCase())&&(valueFilters.kind==="all"||item.kind===valueFilters.kind)&&(valueFilters.source==="all"||sourceOf(item)===valueFilters.source)).sort((a,b)=>a.key.localeCompare(b.key));
      count.textContent=`${items.length} of ${view.items.length} keys`;empty.hidden=!!items.length;
      for(const item of items){
        const row=text("tr","",body);text("td",item.key,row);text("td",item.kind,row);
        const inherited=!!view.env&&item.source==="common";
        row.classList.toggle("value-inherited",inherited);
        const cell=text("td","",row);cell.className="value-cell";
        const edit=mutationButton(cell,item.kind==="secret"?"Replace secret":item.value===""?"Empty value":item.value,()=>inlineValue(cell,item,view,version));
        edit.className="value-edit";edit.setAttribute("aria-label",inherited?`Create override for ${item.key} in ${view.env}`:`Edit ${item.key}`);
        edit.title=inherited?`Create an override in ${view.env}`:`Edit ${item.key}`;
        const source=text("td","",row);
        const badge=text("span",!view.env?"Common":inherited?"Inherited common":item.overrides?"Overrides common":"Env only",source);
        badge.className="value-source "+(!view.env?"common":inherited?"inherited":item.overrides?"override":"local");
        const actions=text("td","",row);
        if(!view.env){
          const override=mutationButton(actions,"Override in…",()=>overrideValue(item,view,version));
          override.className="value-override";
          override.disabled=blocked||!view.envs.length;
          override.title=view.envs.length?`Override ${item.key} in an environment`:"Create an environment first.";
        }
        if(!view.env||item.source==="env")mutationButton(actions,view.env&&item.overrides?"Remove override":"Delete",()=>editRemoval(item));
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
    search.oninput=()=>{valueFilters.search=search.value;render();saveNavigation();};kind.onchange=()=>{valueFilters.kind=kind.value;render();saveNavigation();};sourceFilter.onchange=()=>{valueFilters.source=sourceFilter.value;render();saveNavigation();};render();
  }catch(e){if(generation===version){
    if(e.code==="env_not_found"&&valueEnv){valueEnv="";saveNavigation();await load();notice("The selected environment was removed. Showing Common.");}
    else notice(e.message);
  }}
}


function renderVaultState(panel,state) {
  const section=text("section","",panel);section.className="vault-state";section.setAttribute("aria-label","Vault protection");
  const labels={uninitialized:"Public-only storage",unlocked:"Vault unlocked",locked:"Vault locked",locked_manual:"Vault manually locked",locking:"Vault lock pending",recovery_required:"Vault recovery required"};
  const status=text("p","",section);status.setAttribute("role","status");text("strong",labels[state.state]||"Vault state unavailable",status);
  const message=state.state==="uninitialized" ? "Secrets need one-time setup: devtools vault init. Without a keyring, use --backend passphrase." : state.state==="recovery_required" ? "Inspect existing protection with devtools vault status, then recover it. Do not initialize a replacement key." : state.state==="locked_manual"||state.state==="locking" ? "Running apps keep values already delivered. Unlock to allow new protected changes and executions." : state.backend==="passphrase" ? "Your passphrase session is reused while active." : "System protection can reopen the key silently when allowed.";
  text("p",message,section);
  if(state.state==="unlocked") {
    const lock=button(section,"Lock vault",async()=>{
      lock.disabled=true;
      try {await api("/api/actions",{domain:"vault",vault:{action:"lock"}});await load();document.querySelector(".vault-state button")?.focus();}
      catch(error){notice(error.message);if(lock.isConnected)lock.disabled=false;}
    });
    lock.setAttribute("aria-label","Lock vault for all profiles");
  } else if(["locked","locked_manual"].includes(state.state)) {
    button(section,"Unlock vault",()=>{
      edit("Unlock vault",parent=>{
        text("p","This key session is shared by all profiles.",parent);
        if(state.backend!=="passphrase") {
          text("p","System authentication may open a confirmation window.",parent);
          return()=>({action:"unlock",interactive:true});
        }
        const passphrase=field(parent,"Vault passphrase","","password");passphrase.autocomplete="current-password";
        return()=>{const value=passphrase.value;passphrase.value="";if(!value)throw Error("Enter the vault passphrase.");return {action:"unlock",passphrase:value};};
      },vault=>({domain:"vault",vault}),async()=>{await load();document.querySelector(".vault-state button")?.focus();});
      $("editor-target").textContent="All profiles";
      $("editor-save").textContent="Unlock vault";
    });
  }
}
