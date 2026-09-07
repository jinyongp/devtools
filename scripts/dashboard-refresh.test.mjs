import {test} from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const source = readFileSync(new URL("../internal/dashboard/assets/refresh.js", import.meta.url), "utf8");
function browser() {
  const calls = [], events = {}, editor = {open:false};
  const context = vm.createContext({
    URLSearchParams, AbortController, navigationReady:true, sessionToken:"session", generation:1, detailGeneration:1, loadedPages:1, cursor:null,
    document:{hidden:false,body:{dataset:{connection:"connected"}},activeElement:{matches:() => false},querySelector:() => null,querySelectorAll:() => [],addEventListener:(name, fn) => events[name] = fn},
    $:() => editor, routeKey:() => "demo", setTimeout:() => 1, clearTimeout:() => {},
    api:async path => {calls.push(path); return {revision:2};},
    load:async () => {calls.push("load");context.generation++;},
  });
  vm.runInContext(source, context);
  return {context,calls,events,editor,run:code => vm.runInContext(code, context)};
}
test("changed metadata reloads once; unchanged metadata keeps the DOM", async () => {
  const b = browser();
  b.run('rememberRefreshRead("/api/values?profile=demo", {revision:1})');
  await b.run("refreshVisibleView()");
  assert.deepEqual(b.calls, ["/api/values?profile=demo", "load"]);
  b.calls.length = 0;
  b.run('rememberRefreshRead("/api/values?profile=demo", {revision:2})');
  await b.run("refreshVisibleView()");
  assert.deepEqual(b.calls, ["/api/values?profile=demo"]);
});
test("hidden tabs, editing, focused inputs, disconnected sessions and active loads pause polling", async () => {
  for (const block of [b => b.context.document.hidden=true, b => b.editor.open=true, b => b.context.document.querySelector=() => ({}), b => b.context.document.activeElement.matches=() => true, b => b.context.sessionToken="", b => b.run("activeLoads=1")]) {
    const b = browser(); block(b);
    b.run('rememberRefreshRead("/api/values?profile=demo", {})');
    await b.run("refreshVisibleView()");
    assert.deepEqual(b.calls, []);
  }
});
test("an in-flight result cannot erase a newly opened editor or newer navigation", async () => {
  for (const change of [b => b.editor.open=true, b => b.context.generation++]) {
    const b = browser(); let resolve;
    b.context.api = () => new Promise(done => resolve=done);
    b.run('rememberRefreshRead("/api/values?profile=demo", {})');
    const pending = b.run("refreshVisibleView()");
    change(b); resolve({revision:2}); await pending;
    assert.deepEqual(b.calls, []);
  }
});
test("polls do not overlap, and hiding the tab aborts the request", async () => {
  const b = browser(); let resolve, signal, requests=0;
  b.context.api = (path, body, inputSignal) => {requests++;signal=inputSignal;return new Promise(done => resolve=done);};
  b.run('rememberRefreshRead("/api/processes?profile=demo", {})');
  const pending = b.run("refreshVisibleView()");
  await b.run("refreshVisibleView()");
  assert.equal(requests, 1);
  b.context.document.hidden=true; b.events.visibilitychange();
  assert.equal(signal.aborted, true);
  resolve({}); await pending;
});
test("raw logs, evidence and action payloads are excluded from polling", () => {
  const b = browser();
  for (const path of ["/api/process-logs?profile=demo", "/api/query?command=validation+show&id=one", "/api/query?command=list&cursor=old", "/api/actions"]) b.run(`rememberRefreshRead(${JSON.stringify(path)}, {})`);
  assert.equal(b.run("refreshReads.size"), 0);
});
test("management refresh restores scroll even without paginated task results", async () => {
  const b = browser(), panel = {scrollTop:140};
  b.context.loadedPages = 0;
  b.context.document.querySelectorAll = () => [panel];
  b.context.load = async () => {panel.scrollTop=0;b.context.generation++;};
  b.run('rememberRefreshRead("/api/values?profile=demo", {})');
  await b.run("refreshVisibleView()");
  assert.equal(panel.scrollTop, 140);
});
