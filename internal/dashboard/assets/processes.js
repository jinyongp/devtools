"use strict";
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
    if(data.instances.length)text("h3","Projects & worktrees",panel);
    for(const instance of data.instances){
      const section=text("section","",panel);text("p",`${instance.alias||instance.instance_id} · ${instance.directory}`,section);
      button(section,"Show commands & ports",async()=>{try{const details=await api("/api/project?"+new URLSearchParams({profile:current,instance:instance.instance_id}));if(generation!==version)return;
        const available=text("div","",section);for(const port of details.ports)text("p",`${port.name}: 127.0.0.1:${port.port}`,available);
        for(const command of details.commands)button(available,`Start ${command.name}`,()=>edit(`Start ${command.name}`,p=>{text("p",`${instance.directory} · ${command.env||"common"}`,p);return()=>null;},()=>processRequest(current,{action:"start",directory:instance.directory,command:command.name}),()=>load()));
      }catch(e){notice(e.message);}});
    }
    for(const item of data.items){
      const section=text("section","",panel);text("h3",`${item.command} · ${item.state}`,section);
      text("p",`${item.directory} · ${item.env||"common"}`,section);
      text("p",`Execution ${item.id}`,section);
      if(item.reason)text("p",`${item.reason}${item.exit_code===null?"":` · exit ${item.exit_code}`}`,section);
      const actions=text("div","",section);actions.className="manage-toolbar";
      if(item.ready_configured&&!item.ended_at){
        const readiness=text("p","Readiness: check on demand",section);
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
      if(item.capture_logs)button(actions,"Read raw logs",()=>edit("Read raw output",p=>{text("p","Captured output may contain secret values printed by the command.",p);return()=>null;},()=>({local:()=>null}),async()=>{
        try{const result=await api("/api/process-logs?"+new URLSearchParams({profile:current,id:item.id}));const pre=text("pre",result.content,section);pre.className="raw-output";}catch(e){notice(e.message);}
      }));
    }
    if(!data.items.length)text("p","Start a named command from devtools.toml to manage it here.",panel);
  } catch(e){if(generation===version)notice(e.message);}
}
$("processes-nav").onclick=()=>{scope="processes";ws="";load();};
