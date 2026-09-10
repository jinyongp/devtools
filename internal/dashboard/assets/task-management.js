"use strict";

// Fetch all pages from one revision before presenting a definition editor.
async function taskChoices(current, command, expectedRevision, extra = {}) {
  const items = [];
  let cursor = "";
  do {
    const data = await api("/api/query?" + new URLSearchParams({profile:current, command, state:"all", limit:"200", ...extra, ...(cursor ? {cursor} : {})}));
    if (data.revision !== expectedRevision) throw Error("The profile changed. Refresh the item before editing.");
    items.push(...data.items);
    cursor = data.next_cursor;
  } while (cursor);
  return items;
}

function choiceField(parent, label, items, initial = []) {
  const group = text("fieldset", "", parent);
  group.className = "choice-field";
  text("legend", label, group);
  const search = field(group, "Find by title", "", "search");
  const list = text("div", "", group);
  list.className = "choice-list";
  const selected = new Set(initial);
  // Preserve references absent from the response so saving cannot silently drop them.
  const choices = [...items];
  for (const id of initial) if (!choices.some(item => item.id === id)) choices.push({id, title:"Unavailable item", state:"review required"});
  function render() {
    list.replaceChildren();
    for (const item of choices.filter(item => (item.title + " " + item.id).toLowerCase().includes(search.value.toLowerCase()))) {
      const row = text("label", "", list);
      const input = document.createElement("input");
      input.type = "checkbox"; input.checked = selected.has(item.id); row.append(input);
      const copy = text("span", item.title, row);
      text("small", `${item.state || ""} · ${item.id.slice(0, 8)}`, copy);
      input.onchange = () => input.checked ? selected.add(item.id) : selected.delete(item.id);
    }
    if (!list.children.length) text("p", "No matching items.", list);
  }
  search.oninput = render;
  render();
  return () => [...selected];
}

function specificationFields(parent, doc) {
  const body = field(parent, "Specification", doc.body || "", "textarea");
  const requirements = text("fieldset", "", parent);
  text("legend", "Requirements", requirements);
  const requirementRows = text("div", "", requirements), requirementsList = [];
  const acceptance = text("fieldset", "", parent);
  text("legend", "Acceptance criteria", acceptance);
  const acceptanceRows = text("div", "", acceptance), acceptanceList = [];
  function updateLinks() {
    for (const entry of acceptanceList) {
      entry.links.replaceChildren();
      text("legend", "Connected requirements", entry.links);
      const keys = [...new Set([...requirementsList.map(row => row.key.value).filter(Boolean), ...entry.selected])];
      for (const key of keys) {
        const input = field(entry.links, key, "", "checkbox");
        input.checked = entry.selected.has(key);
        input.onchange = () => input.checked ? entry.selected.add(key) : entry.selected.delete(key);
      }
      if (!keys.length) text("p", "Add a requirement to connect this criterion.", entry.links);
    }
  }
  function addRow(container, rows, initial, isAcceptance) {
    const row = text("div", "", container); row.className = "document-row";
    const entry = {key:field(row, "Key", initial.key || ""), value:field(row, "Description", initial.text || "", "textarea")};
    entry.key.required = true; entry.key.pattern = "[A-Za-z][A-Za-z0-9_-]{0,63}"; entry.value.required = true;
    rows.push(entry);
    if (isAcceptance) {
      entry.links = text("fieldset", "", row);
      entry.selected = new Set(initial.requirement_keys || []);
    }
    button(row, "Remove row", () => { rows.splice(rows.indexOf(entry), 1); row.remove(); updateLinks(); });
    entry.key.oninput = updateLinks;
    updateLinks();
  }
  for (const entry of doc.requirements || []) addRow(requirementRows, requirementsList, entry, false);
  for (const entry of doc.acceptance || []) addRow(acceptanceRows, acceptanceList, entry, true);
  button(requirements, "Add requirement", () => addRow(requirementRows, requirementsList, {}, false));
  button(acceptance, "Add acceptance criterion", () => addRow(acceptanceRows, acceptanceList, {}, true));
  return () => ({body:body.value, requirements:requirementsList.map(row => ({key:row.key.value, text:row.value.value})), acceptance:acceptanceList.map(row => ({key:row.key.value, text:row.value.value, requirement_keys:[...row.selected].filter(key => requirementsList.some(row => row.key.value === key))}))});
}

function itemActions(node, data, parent) {
  const current = profile, rev = data.revision, item = data.item || node;
  const send = (action, body, extra = {}) => taskRequest(action, node.id, body, rev, extra, current);
  const editPlan = (reason, operations) => taskRequest("workstream.edited", node.kind === "workstream" ? node.id : item.workstream_id, {reason, operations}, rev, {}, current);
  const actions = text("div", "", parent); actions.className = "manage-toolbar";
  const terminal = ["done", "canceled"].includes(item.state);
  const run = item.current_run;
  const version = detailGeneration;
  const fresh = () => current === profile && version === detailGeneration;
  const prepare = async action => {
    try { await action(); } catch (error) { if (fresh()) notice(error.message); }
  };
  if (terminal) {
    button(actions, "Reopen", () => edit(`Reopen ${node.kind}`, p => {
      text("p", node.kind === "workstream" ? "Returns this workstream to draft. Its tasks keep their current states." : "Returns this task to open.", p);
      const reason = field(p, "Reason", "", "textarea"); reason.required = true;
      return () => ({reason:reason.value});
    }, body => send(`${node.kind}.reopen`, body), () => load(), true));
  }
  if (run) {
    const summary = text("section", "", parent); summary.className = "execution-summary";
    text("h3", "Current execution", summary);
    text("p", `Started ${readableDate(run.started_at)} · Last activity ${readableDate(run.last_activity_at)}`, summary);
    if (run.directory) text("p", run.directory, summary).className = "prose";
    text("p", `Run ${run.id}`, summary).className = "muted";
    button(actions, "Revoke claim", () => edit("Revoke execution claim", p => {
      text("p", `Task: ${item.title}`, p);
      text("p", `Observed run: ${run.id}`, p);
      text("p", "Invalidates this execution context. The task stays open and can be claimed again. Running commands continue until stopped separately. Coordinate with the agent before reassignment.", p);
      const reason = field(p, "Reason", "", "textarea"); reason.required = true;
      return () => ({reason:reason.value});
    }, body => send("run.revoked", body, {"expected-run":run.id}), () => load(), true));
  } else if (node.kind === "task") {
    text("p", "An agent claims this task through the CLI and records progress and completion.", parent).className = "muted";
  }
  const menu = text("details", "", parent); menu.className = "item-management";
  text("summary", "Manage item", menu);
  const secondary = text("div", "", menu); secondary.className = "manage-toolbar";
  if (node.kind === "workstream") button(secondary, "Edit", () => edit("Edit workstream metadata", p => {
    const title = field(p, "Title", item.title), description = field(p, "Description", item.description, "textarea"); title.required = true;
    return () => ({title:title.value, description:description.value});
  }, body => editPlan("Update workstream metadata", [{op:"workstream.update", value:body}])));
  if (node.kind === "task" && (item.workstream_id || (!run && !terminal))) button(secondary, "Edit", () => edit("Edit task", p => {
    const title = field(p, "Title", item.title), description = field(p, "Description", item.description, "textarea"); title.required = true;
    return () => ({title:title.value, description:description.value});
  }, body => item.workstream_id ? editPlan("Update task definition", [{op:"task.update", id:item.id, value:body}]) : send("task.update", body)));
  if (node.kind === "task" && item.workstream_id) button(secondary, item.scope === "removed" ? "Restore to plan" : "Remove from plan", () => edit("Change plan scope", p => {
    text("p", "History is retained. Related links and current completion checks will be shown in the preview.", p);
    const reason = field(p, "Reason", "", "textarea"); reason.required = true;
    return () => ({reason:reason.value});
  }, body => editPlan(body.reason, [{op:item.scope === "removed" ? "task.restore" : "task.remove", id:item.id}])));
  if (node.kind === "workstream" || item.workstream_id) button(secondary, "Dependencies", () => prepare(async () => {
    const candidates = await taskChoices(current, node.kind === "workstream" ? "workstream list" : "list", rev, item.workstream_id ? {workstream:item.workstream_id} : {});
    const successors = new Set([item.id]);
    let changed;
    do { changed = false; for (const candidate of candidates) if (!successors.has(candidate.id) && candidate.depends_on?.some(id => successors.has(id))) { successors.add(candidate.id); changed = true; } } while (changed);
    if (!fresh()) return;
    edit("Choose prerequisites", p => {
      text("p", "This item becomes ready after every selected prerequisite is complete.", p);
      const read = choiceField(p, "Prerequisites", candidates.filter(candidate => !successors.has(candidate.id)), item.depends_on || []);
      return () => ({depends_on:read()});
    }, body => send(`${node.kind}.depends`, body));
  }));
  if (node.kind === "workstream") {
    button(secondary, "Specification", () => {
      if (data.truncated) { notice("The document context is too large. Use the CLI document commands to edit it."); return; }
      edit("Edit specification", p => specificationFields(p, data.documents?.spec || {}), body => send("spec.set", body));
    });
    button(secondary, "Plan", () => {
      if (data.truncated) { notice("The plan context is too large. Use the CLI document commands to edit it."); return; }
      const doc = data.documents?.plan || {};
      edit("Edit plan", p => {
        const body = field(p, "Implementation plan", doc.body || "", "textarea");
        text("p", "Manage scope from each task using Remove from plan or Restore to plan.", p);
        return () => ({body:body.value});
      }, body => editPlan("Update implementation plan", [{op:"plan.update", value:body}]));
    });
    button(secondary, "Restore excluded tasks", () => prepare(async () => {
      const excluded = await taskChoices(current, "list", rev, {workstream:item.id, scope:"removed"});
      if (!fresh()) return;
      edit("Restore excluded tasks", p => {
        const selected = choiceField(p, "Excluded tasks", excluded);
        const reason = field(p, "Reason", "", "textarea"); reason.required = true;
        return () => ({ids:selected(), reason:reason.value});
      }, body => {
        if (!body.ids.length) throw Error("Select at least one task.");
        return editPlan(body.reason, body.ids.map(id => ({op:"task.restore", id})));
      });
    }));
    const label = item.state === "draft" ? "Activate" : "Close workstream";
    if (item.state !== "canceled" && (item.state !== "done" || item.completion_status === "stale")) button(actions, label, () => edit(label, p => {
      text("p", `${label}: ${item.title}. Coverage and completion checks apply.`, p); return () => ({});
    }, body => send(item.state === "draft" ? "workstream.activate" : "workstream.close", body), () => load(), true));
  }
  if (!run && !terminal) button(secondary, "Cancel", () => prepare(async () => {
    const impact = await api("/api/query?" + new URLSearchParams({profile:current, command:node.kind === "workstream" ? "workstream impact" : "impact", id:node.id}));
    if (!fresh()) return;
    if (impact.revision !== rev) throw Error("The profile changed. Refresh the item before canceling.");
    if (impact.running_ids.length || impact.completed_ids.length) throw Error("Cancellation is blocked by active executions or completed dependent work. Review their status first.");
    if (data.truncated) throw Error("The context is too large to preview cancellation. Review it with the CLI.");
    const affected = node.kind === "workstream" ? (data.tasks || []).filter(task => task.state === "open") : [];
    edit(`Cancel ${node.kind}`, p => {
      text("p", `Cancel: ${item.title}`, p);
      if (node.kind === "workstream") {
        text("p", `${affected.length} open tasks will also be canceled. Completed and canceled tasks keep their state.`, p);
        const list = text("ul", "", p);
        for (const task of affected) text("li", task.title, list);
      }
      text("p", "Dependent work remains blocked until its prerequisites are resolved.", p);
      const reason = field(p, "Reason", "", "textarea"); reason.required = true;
      return () => ({reason:reason.value});
    }, body => send(`${node.kind}.cancel`, body), () => load(), true);
  }));
  menu.hidden = !secondary.children.length;
}
