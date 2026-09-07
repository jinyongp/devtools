"use strict";
const processFilters = {search:"",state:"all"};
function projectName(directory) { return directory.split(/[\\/]/).filter(Boolean).pop() || directory; }
function processRequest(current, body) {
  return {domain:"process", profile:current, process:{...body, request_id:crypto.randomUUID()}};
}
async function loadProcesses(version) {
  const panel=$("values-panel"),current=profile;panel.replaceChildren();
  if(!current){text("p","Choose a profile above to begin.",panel);return;}
  try {
    const data=await api("/api/processes?"+new URLSearchParams({profile:current}));
    if(generation!==version)return;
    button(panel,"Start command",()=>edit("Start named command",p=>{
      const directory=field(p,"Project directory",data.instances[0]?.directory||""),command=field(p,"Command name"),env=field(p,"Environment (blank uses command configuration)");
      const capture=field(p,"Capture raw output (may contain secrets)","","checkbox");
      return()=>({action:"start",directory:directory.value,command:command.value,capture_logs:capture.checked,...(env.value?{env:env.value}:{})});
    },body=>processRequest(current,body)));
    const projects=text("details","",panel);projects.className="process-projects";
    text("summary",`Projects & worktrees (${data.instances.length})`,projects);
    for(const instance of data.instances){
      const section=text("section","",projects);text("strong",instance.alias||projectName(instance.directory),section);text("p",instance.directory,section);
      const available=text("div","",section);
      button(section,"Show commands & ports",async()=>{try{const details=await api("/api/project?"+new URLSearchParams({profile:current,instance:instance.instance_id}));if(generation!==version)return;
        available.replaceChildren();for(const port of details.ports)text("p",`${port.name}: 127.0.0.1:${port.port}`,available);
        for(const command of details.commands)button(available,`Start ${command.name}`,()=>edit(`Start ${command.name}`,p=>{text("p",`${instance.directory} · ${command.env||"common"}`,p);return()=>null;},()=>processRequest(current,{action:"start",directory:instance.directory,command:command.name}),()=>load()));
      }catch(e){notice(e.message);}});
    }
    const table=text("table","",panel);table.className="process-table";
    const header=text("tr","",text("thead","",table));
    const columns=["Command","State","Environment","Project / worktree","Actions"].map(label=>{const th=text("th",label,header);th.scope="col";return th;});
    const search=text("input","",columns[0]);search.type="search";search.placeholder="Search commands";search.setAttribute("aria-label","Search processes");search.value=processFilters.search;
    const filter=text("select","",columns[1]);filter.setAttribute("aria-label","Filter process state");
    for(const state of ["all",...new Set(data.items.map(item=>item.state))])text("option",state==="all"?"All states":state,filter).value=state;
    if(!Array.from(filter.options).some(option=>option.value===processFilters.state))processFilters.state="all";
    filter.value=processFilters.state;
    const body=text("tbody","",table), entries=[];
    for(const item of data.items){
      const row=text("tr","",body);
      const command=text("td","",row);text("strong",item.command,command);
      const section=text("details","",command);section.className="process-details";text("summary","Details",section);
      text("p",item.directory,section);text("p",`Execution ${item.id}`,section);
      if(item.reason)text("p",`${item.reason.replaceAll("_"," ")}${item.exit_code==null?"":` · exit ${item.exit_code}`}`,section);
      const stateCell=text("td","",row),badge=text("span",item.state,stateCell);badge.className="process-state";
      badge.classList.toggle("active",!item.ended_at);badge.classList.toggle("failed",item.state==="exited"&&item.exit_code!=null&&item.exit_code!==0);
      text("td",item.env||"Common",row);
      const project=text("td",data.instances.find(instance=>instance.directory===item.directory)?.alias||projectName(item.directory),row);project.title=item.directory;
      const actions=text("div","",text("td","",row));actions.className="manage-toolbar process-actions";
      entries.push({row,item});
      if(item.ready_configured&&!item.ended_at){
        const readiness=text("p","",stateCell);readiness.className="process-readiness";
        const check=button(actions,"Check readiness",async()=>{
          check.disabled=true;
          try{const result=(await api("/api/actions",processRequest(current,{action:"check",id:item.id}))).data;
            if(generation!==version)return;
            readiness.textContent=`${result.readiness.ready?"Ready":"Not ready"} · ${result.readiness.reason} · ${result.readiness.checked_at}`;
          }catch(e){if(generation===version)notice(e.message);}finally{check.disabled=false;}
        });
      }
      for(const action of ["stop","restart"]){
        if(action==="stop"&&item.ended_at)continue;
        button(actions,action==="stop"?"Stop":"Restart",()=>edit(`${action} ${item.command}`,p=>{
          text("p",`Execution ${item.id} in ${item.directory}.`,p);
          if(action==="restart")text("p","Starts a new execution with the current project configuration and values.",p);
          return()=>({action,id:item.id});
        },body=>processRequest(current,body),()=>load(),true));
      }
      const logs=text("div","",section);
      if(item.capture_logs)button(section,"Read raw logs",()=>edit("Read raw output",p=>{text("p","Captured output may contain secret values printed by the command.",p);return()=>null;},()=>({local:()=>null}),async()=>{
        try{const result=await api("/api/process-logs?"+new URLSearchParams({profile:current,id:item.id}));if(generation!==version)return;logs.replaceChildren();const pre=text("pre",result.content,logs);pre.className="raw-output";}catch(e){if(generation===version)notice(e.message);}
      }));
    }
    const count=text("p","",panel);count.setAttribute("role","status");
    function render(){let visible=0;for(const {row,item} of entries){row.hidden=!(item.command+" "+item.directory+" "+(item.env||"")).toLowerCase().includes(processFilters.search.toLowerCase())||(processFilters.state!=="all"&&processFilters.state!==item.state);if(!row.hidden)visible++;}count.textContent=visible?`${visible} of ${entries.length} executions`:entries.length?"No matching executions.":"Start a named command from devtools.toml to manage it here.";}
    search.oninput=()=>{processFilters.search=search.value;render();};filter.onchange=()=>{processFilters.state=filter.value;render();};render();
  } catch(e){if(generation===version)notice(e.message);}
}
$("processes-nav").onclick=()=>{scope="processes";ws="";load();};
