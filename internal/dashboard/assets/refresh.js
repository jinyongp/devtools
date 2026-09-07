"use strict";
const refreshReads = new Map();
let refreshTimer, refreshController, activeLoads = 0;
function rememberRefreshRead(path, data) {
  const params = new URLSearchParams(path.split("?")[1]);
  const queryRead = path.startsWith("/api/query?") && !params.has("cursor") && ["list", "workstream list", "context", "workstream context"].includes(params.get("command"));
  if (path === "/api/profiles" || path.startsWith("/api/values?") || path.startsWith("/api/processes?") || path.startsWith("/api/archives?") || queryRead) {
    refreshReads.set(path, JSON.stringify(data));
  }
}
function canRefresh() {
  return navigationReady && !!sessionToken && document.body.dataset.connection === "connected" &&
    !document.hidden && !activeLoads && !$("editor").open &&
    !document.querySelector(".inline-value") &&
    !document.activeElement?.matches("input, textarea, select, [contenteditable=true]");
}
function scheduleRefresh() {
  clearTimeout(refreshTimer);
  if (!document.hidden) refreshTimer = setTimeout(refreshVisibleView, 5000);
}
async function refreshVisibleView() {
  if (refreshController) return;
  if (!canRefresh()) { scheduleRefresh(); return; }
  const version = generation, detailVersion = detailGeneration;
  const controller = new AbortController(); refreshController = controller;
  const timeout = setTimeout(() => controller.abort(), 15000);
  try {
    let changed = false;
    for (const [path, previous] of [...refreshReads]) {
      const current = await api(path, undefined, controller.signal, false);
      if (!canRefresh() || generation !== version || detailGeneration !== detailVersion) return;
      changed ||= JSON.stringify(current) !== previous;
      if (changed) break;
    }
    if (changed && canRefresh() && generation === version && detailGeneration === detailVersion) {
      const pages = loadedPages;
      const route = routeKey();
      const scroll = [...document.querySelectorAll(".workspace, .details, #values-panel, #list-panel")].map(element => [element, element.scrollTop]);
      await load();
      let updated = generation;
      for (let page = 1; page < pages && cursor && generation === updated && routeKey() === route && canRefresh(); page++) { await load(true); updated++; }
      if (generation === updated && routeKey() === route) for (const [element, top] of scroll) element.scrollTop = top;
    }
  } catch (error) {
    // The next interval retries transient failures; api handles expired sessions.
    if (error.code === "env_not_found" && canRefresh() && generation === version) await load();
  } finally {
    clearTimeout(timeout); refreshController = null; scheduleRefresh();
  }
}
document.addEventListener("visibilitychange", () => {
  clearTimeout(refreshTimer);
  if (document.hidden) refreshController?.abort();
  else refreshVisibleView();
});
scheduleRefresh();
