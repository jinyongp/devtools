"use strict";
async function loadNetwork(panel, current, version) {
  const section=text("section","",panel);section.className="network-status";
  text("h2","Local network",section);
  try {
    const data=await api("/api/network?"+new URLSearchParams({profile:current}));
    if(generation!==version)return;
    const state=data.state;
    text("p",state.ca.present?`HTTPS CA: ${state.trusted?"trusted by this OS":"client trust required"} · expires ${state.ca.not_after}`:"HTTPS CA has not been initialized.",section);
    if(state.isolated)text("p","Isolated data root. Kernel ports and external provider configuration remain shared.",section);
    const act=(label,body)=>button(section,label,()=>edit(label,p=>{
      text("p","Applies only to this listener or exposure. Backend processes keep running.",p);return()=>body;
    },network=>({domain:"network",profile:current,network:{...network,request_id:crypto.randomUUID()}}),()=>load(),true));
    for(const listener of state.listeners){
      text("p",`${listener.name}: ${listener.state} · ${listener.url||"no URL"}`,section);
      act(`${listener.running?"Stop":"Start"} listener ${listener.name}`,{action:listener.running?"stop":"start",listener:listener.name});
    }
    for(const {route,endpoint,error_code} of data.routes){
      text("p",`${route.scope==="global"?"Global":route.profile} / ${route.proxy||route.host}: ${endpoint.route_url||route.host||"pending"} · ${error_code||route.status}`,section);
    }
    for(const exposure of state.exposures){
      text("p",`${exposure.provider}: ${exposure.url||exposure.host} · ${exposure.state} · ${exposure.auth?"authentication enabled":"no authentication"}`,section);
      for(const instruction of exposure.client_setup||[])text("p",instruction,section);
      if(exposure.state!=="stopped")act(`Stop ${exposure.provider} exposure`,{action:"exposure.stop",id:exposure.id});
    }
    const guide=text("details","",section);text("summary","Start routes and prepare this machine",guide);
    text("p","Run from the project terminal. Starting with --proxy prepares host mappings and selected listeners before launching servers. OS authentication stays in that terminal.",guide);
    text("pre","devtools project up web api --proxy --request-id UUID\ndevtools proxy up ROUTE --request-id UUID\ndevtools proxy setup --check\ndevtools proxy trust --request-id UUID",guide);
    text("p","For WSL, Windows browser DNS, reachability and trust are separate from Linux. LAN clients configure their own name resolution and trust.",guide);
  } catch(e){if(generation===version)text("p",`Network status unavailable: ${e.message}`,section);}
}
