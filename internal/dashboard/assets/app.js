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
function rememberSession(value) {
  sessionToken = value;
  try {
    if (value) sessionStorage.setItem("devtools.session", value);
    else sessionStorage.removeItem("devtools.session");
  } catch {}
}
async function api(path) {
  const response = await fetch(path, {
    credentials: "omit",
    redirect: "error",
    headers: { Authorization: "Bearer " + sessionToken },
  });
  if (response.status === 401) rememberSession("");
  if (!response.ok) {
    let message = await response.text();
    try {
      message = JSON.parse(message).error.message;
    } catch {}
    throw Error(message);
  }
  return response.json();
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
  if (!nodes.length) return;
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
  } else if (event.key.startsWith("Arrow")) {
    event.preventDefault();
    d3.select(canvas).call(
      zoom.translateBy,
      event.key === "ArrowLeft" ? 40 : event.key === "ArrowRight" ? -40 : 0,
      event.key === "ArrowUp" ? 40 : event.key === "ArrowDown" ? -40 : 0,
    );
  }
});
function renderList() {
  const root = $("items");
  root.replaceChildren();
  const search = $("search").value.toLowerCase();
  for (const n of nodes) {
    if (!(n.title + " " + n.id).toLowerCase().includes(search)) continue;
    const entry = text("div", "", root);
    entry.setAttribute("role", "listitem");
    const b = text("button", n.title, entry);
    b.className = n.id === selected ? "selected" : "";
    text("small", state(n), b);
    b.onclick = () => select(n);
  }
  $("count").textContent = `${nodes.length} items · revision ${revision}`;
  $("more").hidden = !cursor;
  $("empty").hidden = nodes.length > 0;
  $("empty").textContent = profile
    ? "This view is ready for your first item."
    : "Choose a profile to get started.";
  draw();
}
async function select(node) {
  if (node.kind === "profile") {
    profile = node.id;
    $("profile").value = profile;
    scope = "workstreams";
    ws = "";
    return load();
  }
  selected = node.id;
  renderList();
  $("detail-title").textContent = node.title;
  $("detail-hint").textContent =
    node.description || `${node.kind} · ${state(node)}`;
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
    const dl = text("dl", "", root);
    for (const [label, value] of [
      ["ID", node.id],
      ["State", state(node)],
      ["Updated", node.updated_at],
    ]) {
      text("dt", label, dl);
      text("dd", value, dl);
    }
    if (node.blockers?.length) {
      text("h3", "Blockers", root);
      const list = text("ul", "", root);
      for (const b of node.blockers) text("li", b.message, list);
    }
    for (const [name, doc] of Object.entries(data.documents || {})) {
      if (doc?.body) {
        text("h3", name, root);
        text("pre", doc.body, root);
      }
    }
    const history = data.history || [];
    if (history.length) {
      text("h3", "Recent activity", root);
      for (const h of history.slice(-5).reverse()) {
        text("p", `${h.action} · ${h.occurred_at}`, root);
        if (h.data.summary) text("pre", h.data.summary, root);
      }
    }
    if (data.truncated)
      text(
        "p",
        "Additional context is available through the CLI context and document commands.",
        root,
      );
  } catch (e) {
    if (version === detailGeneration) notice(e.message);
  }
}
async function load(more = false) {
  const version = ++generation;
  notice("");
  if (!more) {
    nodes = [];
    cursor = null;
    selected = "";
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
    ? `${profile} · Select an item for its specification and activity.`
    : "Choose a profile to explore its workstreams and tasks.";
  $("overview").classList.toggle("active", scope === "workstreams");
  $("independent").classList.toggle("active", scope === "independent");
  if (!profile) {
    $("title").textContent = "Profiles";
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
    cursor = data.next_cursor;
    revision = data.revision;
    layout();
    renderList();
    if (!more) fit();
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
$("fit").onclick = fit;
$("search").oninput = renderList;
(async () => {
  try {
    const params = new URLSearchParams(location.hash.slice(1));
    history.replaceState(null, "", location.pathname);
    try { sessionToken = sessionStorage.getItem("devtools.session") || ""; } catch {}
    if (params.has("token")) {
      const response = await fetch("/session", {
        method: "POST",
        credentials: "omit",
        redirect: "error",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: params.get("token") }),
      });
      if (!response.ok) throw Error(await response.text());
      rememberSession((await response.json()).token);
    }
    const result = await api("/api/profiles");
    for (const p of result.profiles) {
      const option = text("option", p, $("profile"));
      option.value = p;
    }
    profile = params.get("profile") || "";
    if (profile && !result.profiles.includes(profile)) {
      const option = text("option", profile, $("profile"));
      option.value = profile;
    }
    $("profile").value = profile;
    await load();
  } catch (e) {
    notice(e.message);
  }
})();
