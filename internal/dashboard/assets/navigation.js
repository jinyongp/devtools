"use strict";
// Only navigation metadata belongs in the URL. Forms and API payloads stay in memory.
let navigationReady = false, navigationRoute = "", detailTab = "Overview", graphPosition = null;
let expandedProcesses = new Set(), projectsExpanded = false;
function routeKey() { return JSON.stringify([profile, scope, ws]); }
function rememberGraphPosition() {
  graphPosition = [transform.x, transform.y, transform.k].map(value => Math.round(value * 1000) / 1000);
  saveNavigation();
}
function readNavigation(fallbackProfile = "") {
  const p = new URLSearchParams(location.search);
  profile = p.get("profile") ?? fallbackProfile;
  scope = ["workstreams", "tasks", "independent", "values", "processes", "storage"].includes(p.get("tab")) ? p.get("tab") : "workstreams";
  ws = scope === "tasks" ? p.get("workstream") || "" : "";
  if (scope === "tasks" && !ws) scope = "workstreams";
  selected = p.get("item") || "";
  viewMode = p.get("view") === "graph" ? "graph" : "list";
  detailTab = ["Overview", "Documents", "Activity"].includes(p.get("detail")) ? p.get("detail") : "Overview";
  valueEnv = p.get("env") || "";
  $("search").value = p.get("search") || "";
  valueFilters.search = p.get("key") || "";
  valueFilters.kind = ["variable", "secret"].includes(p.get("kind")) ? p.get("kind") : "all";
  valueFilters.source = ["common", "inherited", "override", "local"].includes(p.get("source")) ? p.get("source") : "all";
  processFilters.search = p.get("process-search") || "";
  processFilters.state = p.get("process-state") || "all";
  expandedProcesses = new Set(p.getAll("process-detail"));
  projectsExpanded = p.get("projects") === "open";
  const position = (p.get("graph") || "").split(",").map(Number);
  graphPosition = position.length === 3 && position.every(Number.isFinite) && position[2] >= .15 && position[2] <= 3 ? position : null;
  navigationRoute = routeKey();
  navigationReady = true;
}
function saveNavigation(push = false) {
  if (!navigationReady) return;
  const route = routeKey();
  if (route !== navigationRoute) {
    selected = ""; detailTab = "Overview"; graphPosition = null;
    expandedProcesses.clear(); projectsExpanded = false;
    navigationRoute = route;
  }
  const p = new URLSearchParams();
  const put = (key, value, defaultValue = "") => { if (value !== defaultValue) p.set(key, value); };
  put("profile", profile); put("tab", scope, "workstreams"); put("workstream", ws);
  put("item", selected); put("view", viewMode, "list"); put("detail", detailTab, "Overview");
  put("env", valueEnv); put("search", $("search").value);
  put("key", valueFilters.search); put("kind", valueFilters.kind, "all"); put("source", valueFilters.source, "all");
  put("process-search", processFilters.search); put("process-state", processFilters.state, "all");
  for (const id of expandedProcesses) p.append("process-detail", id);
  if (projectsExpanded) p.set("projects", "open");
  if (graphPosition) p.set("graph", graphPosition.join(","));
  const url = location.pathname + (p.size ? "?" + p : "");
  if (url !== location.pathname + location.search) history[push ? "pushState" : "replaceState"](null, "", url);
}
window.addEventListener("popstate", () => {
  if (!navigationReady || !sessionToken) return;
  readNavigation();
  $("profile").value = profile;
  $("editor").close();
  saveNavigation();
  load();
});
