"use strict";
async function loadStorage(version) {
  const panel=$("values-panel"),current=profile;panel.replaceChildren();
  const send=body=>({domain:"cleanup",profile:current,cleanup:body});
  const toolbar=text("div","",panel);toolbar.className="manage-toolbar";
  if(current){
    let restoreInput;
    button(toolbar,"Create encrypted backup",()=>edit("Back up profile",p=>{text("p",`Encrypt ${current} using the configured backup directory and recipient.`,p);return()=>null;},()=>({domain:"backup",profile:current,backup:{action:"create",request_id:crypto.randomUUID()}}),result=>{notice(`Backup saved: ${result.path}`);}));
    button(toolbar,"Restore encrypted backup",()=>edit("Preview profile restore",p=>{
      const file=field(p,"Backup file"),identity=field(p,"Identity file path"),source=field(p,"Source profile",current),replace=field(p,"Replace existing profile after safety backup","","checkbox");
      return()=>({action:"restore",file:file.value,identity_file:identity.value,source_profile:source.value,replace:replace.checked});
    },body=>{restoreInput=body;return{domain:"backup",profile:current,backup:body};},plan=>{
      // Preserve the exact inspected inputs; the next dialog applies this digest.
      const inspected=restoreInput;
      if(!inspected){notice("Open the restore preview again.");return;}
      edit("Apply profile restore",p=>{for(const target of plan.targets)text("p",`${target.source} → ${target.profile}${target.exists?" · replaces existing profile":" · creates profile"}`,p);text("p","Existing profiles receive a safety backup before replacement.",p);return()=>null;},()=>({domain:"backup",profile:current,backup:{...inspected,digest:plan.digest,request_id:crypto.randomUUID()}}),()=>load(),true);
    }));
  }
  button(toolbar,"Preview cleanup",async()=>{
    try{
      const plan=await api("/api/cleanup-preview?"+new URLSearchParams({profile:current}));if(generation!==version)return;
      edit("Choose cleanup candidates",p=>{
        text("p","Selected data moves to the private recovery archive. The preview is valid for 10 minutes.",p);
        const inputs=plan.items.map(item=>({item,input:field(p,`${item.kind} · ${item.profile||"shared"} · ${item.source} · ${item.bytes} bytes`,"","checkbox")}));
        if(!inputs.length)text("p","No eligible cleanup candidates.",p);
        return()=>{const ids=inputs.filter(x=>x.input.checked).map(x=>x.item.id);if(!ids.length)throw Error("Select at least one candidate.");return{action:"apply",plan:plan.id,ids,request_id:crypto.randomUUID()};};
      },send,()=>load(),true);
    }catch(e){notice(e.message);}
  });
  text("p","Archives preserve retired data until explicitly purged after the 30-day recovery period.",panel);
  try{
    const data=await api("/api/archives?"+new URLSearchParams({profile:current}));if(generation!==version)return;
    for(const item of data.items){const section=text("section","",panel);text("h3",`${item.kind} · ${item.profile||"shared"}`,section);text("p",item.source,section);text("p",`Archived ${item.archived_at}`,section);
      if(item.purged_at){text("p","Purged",section);continue;}
      if(item.restored_at)text("p","Restored",section);else button(section,"Restore archive",()=>edit("Restore archived data",p=>{text("p",`Restore ${item.source}. Existing changed data is protected.`,p);return()=>null;},()=>send({action:"restore",id:item.id}),()=>load(),true));
      if(Date.now()-Date.parse(item.archived_at)>=30*86400000)button(section,"Permanently purge",()=>edit("Permanently purge archive",p=>{text("p",`Delete the archived payload for ${item.source}. This releases its storage permanently.`,p);return()=>null;},()=>send({action:"purge",id:item.id}),()=>load(),true));
    }
  }catch(e){if(generation===version)notice(e.message);}
}
$("storage-nav").onclick=()=>{scope="storage";ws="";load();};
