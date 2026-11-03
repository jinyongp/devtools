"use strict";
const processFilters = {search:"",state:"all",view:"current",cursor:"",cursorProfile:""};
function projectName(directory) { return directory.split(/[\\/]/).filter(Boolean).pop() || directory; }
function processRequest(current, body) {
  return {domain:"process", profile:current, process:{...body, request_id:crypto.randomUUID()}};
}
function startProcess(panel,current,directory="",name="") {
  return button(panel,name?"Start "+name:"Start command",()=>edit("Start named command",p=>{
    const dir=field(p,"Project directory",directory),command=field(p,"Command name",name),env=field(p,"Environment (blank uses command configuration)");
    const capture=field(p,"Capture raw output (may contain secrets)","","checkbox");
    return()=>({action:"start",directory:dir.value,command:command.value,capture_logs:capture.checked,...(env.value?{env:env.value}:{})});
  },body=>processRequest(current,body)));
}
function processDetailPath(current,id) { return "/api/process?"+new URLSearchParams({profile:current,id}); }
async function processDetails(section,current,id,version,visible) {
  try {
    const path=processDetailPath(current,id);
    const result=await api(path,undefined,undefined,false);
    if(generation!==version||!visible())return;
    rememberRefreshRead(path,result);
    const item=result.item,input=result.values;
    text("p","Execution "+id,section);
    text("p","Value selection: "+(item.selection||"all"),section);
    if(input){
      const labels={current:"Current values",pending:"Value changes pending",failed:"Cannot apply current values",unknown:"Value delivery unknown",verification_pending:"Value verification pending"};
      text("p",(labels[input.state]||"Value delivery unknown")+" · "+(input.reason||"").replaceAll("_"," "),section);
      const cause=input.diagnostic?.details;
      if(cause){
        const parts=[cause.reason,cause.state,cause.retry].filter(Boolean).map(value=>value.replaceAll("_"," "));
        if(parts.length)text("p","Storage: "+parts.join(" · "),section);
      }
      if(input.delivered)text("p","Recorded delivery: "+input.delivered.slots.length+" values. Delivery records do not show which values the application read.",section);
    }
    if(item.value_apply){
      text("p","Automatic value update: "+item.value_apply.state+" · "+(item.value_apply.reason||"").replaceAll("_"," "),section);
      if(item.value_apply.state==="failed"||item.value_apply.state==="paused")text("p","Automatic updates paused. Fix the cause and use Restart to retry explicitly.",section);
      if(item.value_apply.next_id)text("p","Replacement execution: "+item.value_apply.next_id,section);
    }
    if(item.value_watch)text("p","Background value check: "+item.value_watch.state.replaceAll("_"," ")+" · "+(item.value_watch.reason||"").replaceAll("_"," "),section);
    if(item.output)text("p","Output: stdout "+item.output.stdout+", stderr "+item.output.stderr,section);
    if(item.reason)text("p",item.reason.replaceAll("_"," ")+(item.exit_code==null?"":" · exit "+item.exit_code),section);
    if(item.ready_configured&&!item.ended_at){
      button(section,"Check readiness",async()=>{
        try{const data=(await api("/api/actions",processRequest(current,{action:"check",id}))).data;if(generation===version)text("p",(data.readiness.ready?"Ready":"Not ready")+" · "+data.readiness.reason,section);}
        catch(e){if(generation===version)notice(e.message);}
      });
    }
    if(item.capture_logs)button(section,"Read raw logs",()=>edit("Read raw output",p=>{
      text("p","Captured output may contain secret values printed by the command.",p);return()=>null;
    },()=>({local:()=>null}),async()=>{
      try{const data=await api("/api/process-logs?"+new URLSearchParams({profile:current,id}));if(generation===version){const pre=text("pre",data.content,section);pre.className="raw-output";}}
      catch(e){if(generation===version)notice(e.message);}
    }));
  }catch(e){if(generation===version&&visible())text("p","Details unavailable: "+e.message,section);}
}
async function loadProcesses(version) {
  const panel=$("values-panel"),current=profile;panel.replaceChildren();
  if(!current){text("p","Choose a profile above to begin.",panel);return;}
  if(processFilters.cursorProfile!==current){processFilters.cursor="";processFilters.cursorProfile=current;}
  try {
    const query={profile:current,view:processFilters.view||"current",limit:"50"};
    if(processFilters.search)query.query=processFilters.search;
    if(processFilters.state!=="all")query.state=processFilters.state;
    if(processFilters.cursor)query.cursor=processFilters.cursor;
    const data=await api("/api/processes?"+new URLSearchParams(query));
    if(generation!==version)return;
    const controls=text("div","",panel);controls.className="manage-toolbar";
    const view=text("select","",controls);view.setAttribute("aria-label","Process view");
    for(const value of ["current","history"])text("option",value==="current"?"Current services":"Execution history",view).value=value;
    view.value=query.view;
    const search=text("input","",controls);search.type="search";search.placeholder="Search commands";search.setAttribute("aria-label","Search processes");search.value=processFilters.search;
    const filter=text("select","",controls);filter.setAttribute("aria-label","Filter process state");
    for(const state of ["all","running","starting","stopped","exited","failed","interrupted","unknown","not_started","conflict"])text("option",state==="all"?"All states":state,filter).value=state;
    filter.value=processFilters.state;
    const refresh=()=>{processFilters.view=view.value;processFilters.search=search.value;processFilters.state=filter.value;processFilters.cursor="";saveNavigation();load();};
    view.onchange=refresh;filter.onchange=refresh;button(controls,"Search",refresh);search.onkeydown=event=>{if(event.key==="Enter")refresh();};
    startProcess(panel,current,data.instances[0]?.directory||"");
    const projects=text("details","",panel);projects.className="process-projects";
    text("summary",`Projects & worktrees (${data.instances.length})`,projects);
    projects.open=projectsExpanded;
    projects.ontoggle=()=>{if(generation===version){projectsExpanded=projects.open;saveNavigation();}};
    for(const instance of data.instances){
      const section=text("section","",projects);text("strong",instance.alias||projectName(instance.directory),section);text("p",instance.directory,section);
      const available=text("div","",section);
      button(section,"Show commands & ports",async()=>{try{
        const details=await api("/api/project?"+new URLSearchParams({profile:current,instance:instance.instance_id}));
        if(generation!==version)return;
        available.replaceChildren();
        for(const port of details.ports)text("p",`${port.name}: 127.0.0.1:${port.port}`,available);
        for(const command of details.commands)startProcess(available,current,instance.directory,command.name);
      }catch(e){if(generation===version)notice(e.message);}});
    }
    const table=text("table","",panel);table.className="process-table";
    const header=text("tr","",text("thead","",table));
    for(const label of ["Command",query.view==="current"?"Current state":"State","Last attempt / reason","Environment","Project / worktree","Actions"]){const th=text("th",label,header);th.scope="col";}
    const body=text("tbody","",table);
    for(const item of data.items){
      const row=text("tr","",body),command=text("td","",row);text("strong",item.command,command);
      const state=item.current_state||item.state;
      const badge=text("span",state,text("td","",row));badge.className="process-state";badge.classList.toggle("active",["running","starting"].includes(state));
      const last=item.last_attempt;
      text("td",last?last.action+" · "+last.state+(last.reason?" · "+last.reason.replaceAll("_"," "):""):(item.reason||"").replaceAll("_"," "),row);
      text("td",item.env||"Common",row);
      const project=text("td",data.instances.find(instance=>instance.directory===item.directory)?.alias||projectName(item.directory),row);project.title=item.directory;
      const actions=text("div","",text("td","",row));actions.className="manage-toolbar process-actions";
      const active=item.active_execution_ids||[];
      const ids=active.length?active:[item.execution_id||last?.execution_id].filter(Boolean);
      if(state==="conflict")text("p",active.length+" active executions: "+active.join(", "),command);
      if(ids.length===0)startProcess(actions,current,item.directory,item.command);
      for(const id of ids){
        for(const action of ["stop","restart"]){
          if(action==="stop" && !(active.includes(id)||item.kind==="execution"&&!item.ended_at))continue;
          button(actions,(action==="stop"?"Stop":"Restart")+(ids.length>1?" "+id.slice(0,8):""),()=>edit(action+" "+item.command,p=>{
            text("p","Execution "+id+" in "+item.directory+".",p);
            if(action==="restart")text("p","Starts a new execution with the current project configuration and values.",p);
            return()=>({action,id});
          },body=>processRequest(current,body),()=>load(),true));
        }
      }
      const details=text("details","",command);details.className="process-details";text("summary","Details",details);
      text("p",item.directory,details);
      if(item.created_at)text("p","Created "+item.created_at,details);
      if(item.ended_at)text("p","Ended "+item.ended_at,details);
      const content=text("div","",details);
      let fetched=false,detailVersion=0;
      details.open=expandedProcesses.has(item.id);
      const open=()=>{
        if(generation!==version)return;
        if(details.open){
          expandedProcesses.add(item.id);
          if(!fetched){
            fetched=true;const request=++detailVersion;content.replaceChildren();
            for(const id of ids)processDetails(content,current,id,version,()=>details.open&&detailVersion===request);
          }
        }else{
          expandedProcesses.delete(item.id);fetched=false;detailVersion++;
          for(const id of ids)refreshReads.delete(processDetailPath(current,id));
        }
        saveNavigation();
      };
      details.ontoggle=open;if(details.open)open();
    }
    const count=text("p",data.items.length+" "+(query.view==="current"?"services":"executions")+" shown"+(data.next_cursor?". More results available.":"."),panel);count.setAttribute("role","status");
    if(processFilters.cursor)button(panel,"First page",()=>{processFilters.cursor="";load();});
    if(data.next_cursor)button(panel,"Next page",()=>{processFilters.cursor=data.next_cursor;load();});
    await loadNetwork(panel,current,version);
  } catch(e){if(generation===version)notice(e.message);}
}
$("processes-nav").onclick=()=>{scope="processes";ws="";processFilters.cursor="";load();};
