/* D3 7.9.0 handles graph transforms; Canvas renders nodes and directed edges. */
"use strict";
const $ = (id) => document.getElementById(id),
  canvas = $("graph"),
  ctx = canvas.getContext("2d");
let profile = "",
  scope = "workstreams",
  ws = "",
  nodes = [],
  selected = "",
  cursor = null,
  revision = 0,
  generation = 0,
  detailGeneration = 0;
let transform = d3.zoomIdentity,
  width = 600,
  height = 400;
const zoom = d3
  .zoom()
  .scaleExtent([0.15, 3])
  .on("zoom", (event) => {
    transform = event.transform;
    draw();
  });
d3.select(canvas).call(zoom);
function notice(message) {
  $("notice").textContent = message;
}
let sessionToken = "";
function connectionState(state, message = "") {
  document.body.dataset.connection = state;
  $("connection-status").hidden = state === "connected";
  $("connection-status").textContent = message;
}
function setAuthenticated(value) {
  document.querySelectorAll("main button, main input, main select").forEach(control => { control.disabled = !value; });
  $("graph").inert = !value;
  $("profile").disabled = !value;
  $("refresh").disabled = !value;
  if (value) connectionState("connected");
}
connectionState("connecting", "Connecting to dashboard…");
setAuthenticated(false);
function rememberSession(value) {
  sessionToken = value;
  try {
    if (value) sessionStorage.setItem("devtools.session", value);
    else sessionStorage.removeItem("devtools.session");
  } catch {}
}
async function api(path, body, signal, track = true) {
  const readVersion = generation, readDetailVersion = detailGeneration;
  if (!sessionToken) throw Error("Open the full link printed by devtools dashboard to connect this tab.");
  const credential = sessionToken;
  const response = await fetch(path, {
    signal,
    credentials: "omit",
    redirect: "error",
    method: body === undefined ? "GET" : "POST",
    headers: { Authorization: "Bearer " + sessionToken, ...(body === undefined ? {} : {"Content-Type":"application/json"}) },
    ...(body === undefined ? {} : {body: JSON.stringify(body)}),
  });
  if (response.status === 401 && credential === sessionToken) {
    rememberSession(""); setAuthenticated(false);
    connectionState("disconnected", "Session expired. Run devtools dashboard again and open the newly printed link.");
  }
  if (!response.ok) {
    let message = await response.text();
    let code;
    try {
      const failure = JSON.parse(message).error;
      message = failure.message; code = failure.code;
      const related = failure.details?.related || [];
      if (related.length) message += " " + related.map(item => `${item.title}: ${(item.blockers || []).map(blocker => `${blocker.title || ""} ${blocker.message || ""}`).join(" ")}`).join(" ");
    } catch {}
    const error = Error(message); error.responded = true; error.code = code; throw error;
  }
  const data = await response.json();
  if (track && body === undefined && generation === readVersion && detailGeneration === readDetailVersion) rememberRefreshRead(path, data);
  return data;
}
function query(command, extra = {}) {
  return api(
    "/api/query?" + new URLSearchParams({ profile, command, ...extra }),
  );
}
function text(tag, value, parent) {
  const element = document.createElement(tag);
  element.textContent = value ?? "";
  parent.append(element);
  return element;
}
function state(node) {
	if (node.scope === "removed") return node.running ? "removed-running" : "removed";
	if (node.execution_status === "stale") return "stale-running";
	if (node.completion_status === "stale") return "stale";
  return node.kind === "profile"
    ? "profile"
    : node.running
      ? "running"
      : node.state;
}
function color(node) {
  return (
    {
      done: "#278970",
      running: "#b88827",
      canceled: "#a1a5a1",
      draft: "#7e8b9e",
    }[state(node)] || "#598075"
  );
}
function layout() {
  const byId = new Map(nodes.map((n) => [n.id, n])),
    levels = new Map();
  function level(n, seen = new Set()) {
    if (levels.has(n.id)) return levels.get(n.id);
    if (seen.has(n.id)) return 0;
    seen.add(n.id);
    const parents = (n.depends_on || [])
      .map((id) => byId.get(id))
      .filter(Boolean);
    const value = parents.length
      ? 1 + d3.max(parents, (p) => level(p, new Set(seen)))
      : 0;
    levels.set(n.id, value);
    return value;
  }
  const columns = d3.group(nodes, (node) => level(node));
  for (const [column, group] of columns) {
    group.forEach((n, index) => {
      n.x = column * 260;
      n.y = index * 105;
    });
  }
  draw();
}
function draw() {
  const ratio = window.devicePixelRatio || 1;
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  ctx.clearRect(0, 0, width, height);
  ctx.save();
  ctx.translate(transform.x, transform.y);
  ctx.scale(transform.k, transform.k);
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const related = new Set(selected ? [selected] : []);
  for (const downstream of [false, true]) {
    const queue = selected ? [selected] : [];
    const seen = new Set();
    while (queue.length) {
      const id = queue.shift();
      if (seen.has(id)) continue;
      seen.add(id);
      related.add(id);
      if (downstream) {
        for (const node of nodes) {
          if (node.depends_on?.includes(id)) queue.push(node.id);
        }
      } else {
        queue.push(...(byId.get(id)?.depends_on || []));
      }
    }
  }
  for (const node of nodes) {
    for (const id of node.depends_on || []) {
      const parent = byId.get(id);
      if (!parent) continue;
      const x = parent.x + 200,
        y = parent.y + 32,
        end = node.x - 8,
        ey = node.y + 32;
      ctx.beginPath();
      ctx.moveTo(x, y);
      if (end - x > 100 && Math.abs(y - ey) < 40) {
        ctx.lineTo(x + 20, y);
        ctx.lineTo(x + 20, y - 70);
        ctx.lineTo(end - 20, ey - 70);
        ctx.lineTo(end - 20, ey);
        ctx.lineTo(end, ey);
      } else {
        ctx.bezierCurveTo(x + 30, y, end - 30, ey, end, ey);
      }
      ctx.strokeStyle =
        selected && related.has(id) && related.has(node.id)
          ? "#207767"
          : "#b1beb0";
      ctx.lineWidth =
        selected && related.has(id) && related.has(node.id) ? 2 : 1.3;
      ctx.stroke();
      ctx.beginPath();
      ctx.moveTo(end, ey);
      ctx.lineTo(end - 6, ey - 4);
      ctx.lineTo(end - 6, ey + 4);
      ctx.closePath();
      ctx.fillStyle = "#94a78f";
      ctx.fill();
    }
  }
  for (const n of nodes) {
    ctx.fillStyle = n.id === selected ? "#e6f3e9" : "#fff";
    ctx.strokeStyle = related.has(n.id) ? "#207767" : "#ccd7c9";
    ctx.lineWidth = n.id === selected ? 2 : 1;
    ctx.beginPath();
    ctx.roundRect(n.x, n.y, 200, 64, 6);
    ctx.fill();
    ctx.stroke();
    ctx.fillStyle = color(n);
    ctx.beginPath();
    ctx.arc(n.x + 15, n.y + 19, 3, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "#78857a";
    ctx.font = "10px system-ui";
    ctx.fillText(state(n).toUpperCase(), n.x + 25, n.y + 22);
    ctx.fillStyle = "#294139";
    ctx.font = "13px system-ui";
    let title = n.title;
    while (ctx.measureText(title).width > 175 && title.length > 1) {
      title = title.slice(0, -2) + "…";
    }
    ctx.fillText(title, n.x + 12, n.y + 46);
  }
  ctx.restore();
}
function fit() {
  if (!nodes.length || $("stage").hidden) return;
  width = $("stage").clientWidth;
  height = $("stage").clientHeight;
  if (!width || !height) return;
  const maxX = d3.max(nodes, (n) => n.x) + 200,
    maxY = d3.max(nodes, (n) => n.y) + 64;
  const k = Math.min(1.2, (width - 70) / maxX, (height - 80) / maxY);
  d3.select(canvas).call(
    zoom.transform,
    d3.zoomIdentity
      .translate((width - maxX * k) / 2, (height - maxY * k) / 2)
      .scale(Math.max(0.15, k)),
  );
}
new ResizeObserver((entries) => {
  const r = entries[0].contentRect;
  width = r.width;
  height = r.height;
  const ratio = window.devicePixelRatio || 1;
  canvas.width = Math.round(width * ratio);
  canvas.height = Math.round(height * ratio);
  draw();
}).observe($("stage"));
canvas.addEventListener("click", (event) => {
  if (event.defaultPrevented) return;
  const box = canvas.getBoundingClientRect(),
    [x, y] = transform.invert([
      event.clientX - box.left,
      event.clientY - box.top,
    ]);
  const n = nodes.find(
    (n) => x >= n.x && x <= n.x + 200 && y >= n.y && y <= n.y + 64,
  );
  if (n) select(n);
});
canvas.addEventListener("keydown", (event) => {
  if (["+", "=", "-"].includes(event.key)) {
    event.preventDefault();
    d3.select(canvas).call(zoom.scaleBy, event.key === "-" ? 0.8 : 1.25);
    rememberGraphPosition();
  } else if (event.key.startsWith("Arrow")) {
    event.preventDefault();
    d3.select(canvas).call(
      zoom.translateBy,
      event.key === "ArrowLeft" ? 40 : event.key === "ArrowRight" ? -40 : 0,
      event.key === "ArrowUp" ? 40 : event.key === "ArrowDown" ? -40 : 0,
    );
    rememberGraphPosition();
  }
});
let viewMode = "list";
function applyView() {
  const management = ["values", "processes", "storage"].includes(scope);
  const graph = viewMode === "graph";
  $("list-panel").hidden = management || graph;
  $("stage").hidden = management || !graph;
  $("fit").hidden = management || !graph;
  $("search").hidden = graph;
  $("graph-hint").hidden = !graph;
  $("list-view").setAttribute("aria-pressed", String(!graph));
  $("graph-view").setAttribute("aria-pressed", String(graph));
}
function readableDate(value) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Unknown date" : new Intl.DateTimeFormat(undefined, {dateStyle:"medium", timeStyle:"short"}).format(date);
}
function renderList() {
  const root = $("items");
  root.replaceChildren();
  const search = $("search").value.toLowerCase();
  for (const n of nodes) {
    if (!(n.title + " " + n.id).toLowerCase().includes(search)) continue;
    const entry = text("div", "", root);
    entry.setAttribute("role", "listitem");
    const b = text("button", "", entry);
    text("span", n.title, b);
    b.className = n.id === selected ? "selected" : "";
    b.setAttribute("aria-pressed", String(n.id === selected));
    const badge = text("small", state(n), b);
    badge.className = "state-badge " + state(n);
    b.onclick = () => select(n);
  }
  $("count").textContent = `${root.children.length} of ${nodes.length} items`;
  $("list-empty").hidden = root.children.length > 0;
  $("list-empty").textContent = search ? "No matching items. Try another title or ID." : "No items yet. Create one to get started.";
  $("more").hidden = !cursor;
  $("empty").hidden = nodes.length > 0;
  $("empty").textContent = profile
    ? "This view is ready for your first item."
    : "Choose a profile to get started.";
  draw();
}
async function select(node) {
  for (const path of refreshReads.keys()) {
    if (path.startsWith("/api/query?") && ["context", "workstream context", "validation show"].includes(new URLSearchParams(path.split("?")[1]).get("command"))) refreshReads.delete(path);
  }
  if (node.kind === "profile") {
    profile = node.id;
    $("profile").value = profile;
    scope = "workstreams";
    ws = "";
    return load();
  }
  selected = node.id;
  saveNavigation(true);
  renderList();
  $("detail-title").textContent = node.title;
  $("detail-hint").textContent =
    `${node.kind} · ${state(node)}`;
  const root = $("detail-content");
  root.replaceChildren();
  $("expand").hidden = node.kind !== "workstream";
  $("expand").onclick = () => {
    ws = node.id;
    scope = "tasks";
    load();
  };
  const version = ++detailGeneration;
  try {
    const data = await query(
      node.kind === "workstream" ? "workstream context" : "context",
      { id: node.id },
    );
    if (version !== detailGeneration) return;
    itemActions(node, data, root);
    const switches = text("div", "", root);
    switches.className = "detail-switch";
    switches.setAttribute("role", "group");
    switches.setAttribute("aria-label", "Detail sections");
    const panels = ["Overview", "Documents", "Activity"].map(label => {
      const panel = text("section", "", root);
      panel.setAttribute("aria-label", label);
      panel.className = "detail-section";
      const control = button(switches, label, () => {
        detailTab = label;
        saveNavigation(true);
        for (const entry of panels) {
          entry.panel.hidden = entry.panel !== panel;
          entry.control.setAttribute("aria-pressed", String(entry.panel === panel));
        }
      });
      panel.hidden = label !== detailTab;
      control.setAttribute("aria-pressed", String(!panel.hidden));
      return {panel, control};
    });
    const [overview, documents, activity] = panels.map(entry => entry.panel);
    text("p", data.item?.description || node.description || "No description yet.", overview).className = "prose";
    text("p", `Updated ${readableDate(node.updated_at)}`, overview).className = "muted";
    if (data.validations?.length) {
      text("h3", "Validations", overview);
      for (const validation of data.validations) {
        const section = text("section", "", overview);
        text("p", `${validation.title} · ${validation.required ? "Required" : "Optional"}`, section);
        const show = button(section, "View evidence", async () => {
          show.disabled = true;
          try {
            const evidence = await query("validation show", {id:validation.id});
            if (version !== detailGeneration) return;
            const records = evidence.records || [];
            text("p", "Recorded evidence is historical. Completion checks validate it against the current definition and execution.", section);
            const recent = records.slice(-5).reverse();
            if (!recent.length) text("p", "No recorded evidence yet.", section);
            for (const record of recent) {
              text("strong", record.result, section);
              text("p", record.summary || record.reason || "", section).className = "prose";
              for (const source of record.evidence || []) text("p", `${source.kind}: ${source.reference}\n${source.description}`, section).className = "prose";
            }
            if (records.length > 5) text("p", "Showing the latest five records. Full history is available through the CLI.", section);
            show.remove();
          } catch (error) { if (version === detailGeneration) notice(error.message); show.disabled = false; }
        });
      }
    }
    const metadata = text("details", "", overview);
    text("summary", "Technical details", metadata);
    const dl = text("dl", "", metadata);
    for (const [label, value] of [
      ["ID", node.id],
      ["Revision", data.revision],
    ]) {
      text("dt", label, dl);
      text("dd", value, dl);
    }
    if (node.blockers?.length) {
      text("h3", "Blockers", overview);
      const list = text("ul", "", overview);
      for (const b of node.blockers) text("li", b.message, list);
    }
    for (const [name, doc] of Object.entries(data.documents || {})) {
      if (doc?.body) {
        text("h3", name, documents);
        text("p", doc.body, documents).className = "prose";
      }
    }
    const history = data.history || [];
    if (history.length) {
      text("h3", "Recent activity", activity);
      for (const h of history.slice(-5).reverse()) {
        const entry = text("article", "", activity);
        entry.className = "activity-entry";
        text("strong", h.action.replaceAll(/[._]/g, " "), entry);
        const time = text("time", readableDate(h.occurred_at), entry);
        time.dateTime = h.occurred_at;
        if (h.data?.summary || h.data?.reason) text("p", h.data.summary || h.data.reason, entry).className = "prose";
      }
    }
    if (!documents.children.length) text("p", "No documents yet.", documents);
    if (!history.length) text("p", "No activity yet.", activity);
    if (data.truncated)
      text(
        "p",
        "Additional context is available through the CLI context and document commands.",
        activity,
      );
  } catch (e) {
    if (version === detailGeneration) notice(e.message);
  }
}
let loadedPages = 0;
async function load(more = false) {
  activeLoads++;
  if (!more) { refreshReads.clear(); loadedPages = 0; }
  try { return await loadView(more); }
  finally { activeLoads--; }
}
async function loadView(more = false) {
  saveNavigation(true);
  const restoreSelected = selected;
  const version = ++generation;
  notice("");
  const management = scope === "values" || scope === "processes" || scope === "storage";
  document.body.classList.toggle("management-view",management);
  $("eyebrow").textContent=management?"MANAGEMENT":"DEPENDENCIES";
  $("values-panel").hidden = !management;
  $("stage").hidden = management;
  $("fit").hidden = management;
  applyView();
  $("create-item").hidden = management || !profile;
  $("create-item").textContent = scope === "workstreams" ? "Create workstream" : "Create task";
  $("values-nav").classList.toggle("active", scope === "values");
  $("processes-nav").classList.toggle("active", scope === "processes");
  $("storage-nav").classList.toggle("active", scope === "storage");
  if (!more) {
    nodes = [];
    cursor = null;
    detailGeneration++;
    $("detail-title").textContent = "Select an item";
    $("detail-hint").textContent =
      "Explore a node or choose an item from the list to see its context.";
    $("detail-content").replaceChildren();
    $("expand").hidden = true;
  }
  $("title").textContent =
    scope === "workstreams"
      ? "Workstreams"
      : scope === "independent"
        ? "Independent tasks"
        : "Workstream tasks";
  $("subtitle").textContent = profile
    ? "Select an item to view details and manage its progress."
    : "Choose a profile to explore its workstreams and tasks.";
  $("overview").classList.toggle("active", scope === "workstreams");
  $("independent").classList.toggle("active", scope === "independent");
  if(scope === "values") {
    $("title").textContent = "Variables & secrets";
    $("subtitle").textContent = profile ? `${profile} · Common values and environment overrides.` : "Choose a profile to manage its values.";
    renderList();
    return loadValues(version);
  }
  if(scope === "processes") {
    $("title").textContent = "Processes";
    $("subtitle").textContent = profile ? `${profile} · Named development commands.` : "Choose a profile to manage its processes.";
    renderList();return loadProcesses(version);
  }
  if(scope === "storage") {
    $("title").textContent = "Storage & recovery";
    $("subtitle").textContent = `${profile || "All profiles"} · Backup, preview, archive and restore.`;
    renderList();return loadStorage(version);
  }
  if (!profile) {
    $("title").textContent = "Profiles";
    try {
      const data = await api("/api/profiles");
      if (version !== generation) return;
      $("profile").replaceChildren();
      text("option", "All profiles", $("profile")).value = "";
      for (const name of data.profiles) text("option", name, $("profile")).value = name;
    } catch (error) { if (version === generation) notice(error.message); return; }
    nodes = Array.from($("profile").options)
      .filter((o) => o.value)
      .map((o) => ({
        id: o.value,
        title: o.value,
        kind: "profile",
        depends_on: [],
      }));
    revision = 0;
    layout();
    renderList();
    fit();
    return;
  }
  try {
    const command = scope === "workstreams" ? "workstream list" : "list";
    const data = await query(command, {
      limit: "200",
      state: "all",
      ...(ws ? { workstream: ws } : {}),
      ...(more && cursor ? { cursor } : {}),
    });
    if (version !== generation) return;
    let incoming = data.items;
    if (scope === "independent")
      incoming = incoming.filter((n) => !n.workstream_id);
    nodes.push(...incoming);
    loadedPages++;
    cursor = data.next_cursor;
    revision = data.revision;
    layout();
    renderList();
    if (!more) {
      if (graphPosition) d3.select(canvas).call(zoom.transform, d3.zoomIdentity.translate(graphPosition[0], graphPosition[1]).scale(graphPosition[2]));
      else fit();
      const item = selected === restoreSelected ? nodes.find(node => node.id === restoreSelected) : undefined;
      if (item) await select(item);
      else if (restoreSelected && selected === restoreSelected) {
        try {
          const context = await query(scope === "workstreams" ? "workstream context" : "context", {id:restoreSelected});
          if (version !== generation) return;
          if (context.item) await select({...context.item, kind:scope === "workstreams" ? "workstream" : "task"});
        } catch { if (version === generation) { selected = ""; saveNavigation(); } }
      }
    }
    if (cursor)
      notice("More items are available. Load more to extend this graph.");
  } catch (e) {
    if (version === generation) {
      notice(e.message);
      renderList();
    }
  }
}
$("profile").onchange = () => {
  profile = $("profile").value;
  scope = "workstreams";
  ws = "";
  valueEnv = "";
  load();
};
$("overview").onclick = () => {
  scope = "workstreams";
  ws = "";
  load();
};
$("independent").onclick = () => {
  scope = "independent";
  ws = "";
  load();
};
$("refresh").onclick = () => load();
$("more").onclick = () => load(true);
$("fit").onclick = () => { graphPosition = null; fit(); saveNavigation(); };
$("search").oninput = () => { renderList(); saveNavigation(); };
$("list-view").onclick = () => { viewMode = "list"; applyView(); saveNavigation(true); };
$("graph-view").onclick = () => { viewMode = "graph"; applyView(); saveNavigation(true); if (!graphPosition) requestAnimationFrame(fit); };
zoom.on("end.navigation", event => {
  if (!event.sourceEvent) return;
  rememberGraphPosition();
});
window.addEventListener("hashchange", () => {
  if (new URLSearchParams(location.hash.slice(1).replaceAll("\\u0026", "&")).has("token")) location.reload();
});
(async () => {
  try {
    if (document.readyState !== "complete") await new Promise(resolve => document.addEventListener("DOMContentLoaded", resolve, {once:true}));
    const params = new URLSearchParams(location.hash.slice(1).replaceAll("\\u0026", "&"));
    history.replaceState(null, "", location.pathname + location.search);
    try { sessionToken = sessionStorage.getItem("devtools.session") || ""; } catch {}
    if (params.has("token")) {
      const response = await fetch("/session", {
        signal: AbortSignal.timeout(15000),
        method: "POST",
        credentials: "omit",
        redirect: "error",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: params.get("token") }),
      });
      if (!response.ok) {
        if (!sessionToken || response.status !== 401) throw Error(await response.text());
      } else rememberSession((await response.json()).token);
    }
    if (!sessionToken) throw Error("Open the full link printed by devtools dashboard to connect this tab.");
    const result = await api("/api/profiles", undefined, AbortSignal.timeout(15000));
    for (const p of result.profiles) {
      const option = text("option", p, $("profile"));
      option.value = p;
    }
    readNavigation(params.get("profile") || "");
    if (profile && !result.profiles.includes(profile)) {
      const option = text("option", profile, $("profile"));
      option.value = profile;
    }
    $("profile").value = profile;
    setAuthenticated(true);
    saveNavigation();
    await load();
  } catch (e) {
    setAuthenticated(false);
    const reason = e.name === "TimeoutError" ? "Dashboard connection timed out." : "Dashboard connection unavailable.";
    connectionState("disconnected", reason + " Run devtools dashboard again and open the newly printed link.");
  }
})();
