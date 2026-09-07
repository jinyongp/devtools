import {test} from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const source = readFileSync(new URL("../internal/dashboard/assets/navigation.js", import.meta.url), "utf8");
function browser(search = "") {
  const fields = {search:{value:""},profile:{value:""},editor:{close:() => {}}};
  const calls = [], events = {};
  const context = vm.createContext({
    URLSearchParams, location:{pathname:"/", search},
    profile:"", scope:"workstreams", ws:"", selected:"", viewMode:"list", valueEnv:"",
    valueFilters:{}, processFilters:{}, sessionToken:"private-session",
    $:id => fields[id], load:() => calls.push("load"),
    window:{addEventListener:(name, fn) => events[name] = fn},
  });
  context.history = Object.fromEntries(["pushState", "replaceState"].map(method => [method, (_, unused, url) => {
    calls.push(method); context.location.search = new URL(url, "http://localhost").search;
  }]));
  vm.runInContext(source, context);
  return {context, calls, events, run:code => vm.runInContext(code, context)};
}

test("reload restores environment, filters and detail navigation without credentials", () => {
  const b = browser("?profile=demo&tab=values&env=dev&key=GOOGLE&kind=variable&source=override&token=untrusted&value=private");
  b.run("readNavigation(); saveNavigation();");
  assert.equal(b.context.valueEnv, "dev");
  assert.equal(b.context.scope, "values");
  assert.equal(b.context.valueFilters.search, "GOOGLE");
  assert.equal(b.context.valueFilters.kind, "variable");
  assert.equal(b.context.valueFilters.source, "override");
  assert.doesNotMatch(b.context.location.search, /token|private|value=/);
  const reloaded = browser(b.context.location.search);
  reloaded.run("readNavigation();");
  assert.equal(reloaded.context.valueEnv, "dev");
  assert.equal(reloaded.context.valueFilters.source, "override");
});

test("tab navigation pushes history; filters replace; back restores the route", () => {
  const b = browser("?profile=demo&tab=values&env=dev");
  b.run("readNavigation(); valueFilters.search='API'; saveNavigation();");
  assert.deepEqual(b.calls, ["replaceState"]);
  const previous = b.context.location.search;
  b.run("scope='processes'; saveNavigation(true);");
  assert.equal(b.calls.at(-1), "pushState");
  b.context.location.search = previous;
  b.events.popstate();
  assert.equal(b.context.scope, "values");
  assert.equal(b.context.valueEnv, "dev");
  assert.equal(b.calls.at(-1), "load");
});

test("task detail and graph transform survive refresh, then clear for another route", () => {
  const b = browser("?profile=demo&tab=tasks&workstream=ws1&item=task1&view=graph&detail=Activity&graph=20,-30,1.5");
  b.run("readNavigation(); saveNavigation();");
  assert.equal(b.context.selected, "task1");
  assert.equal(b.run("detailTab"), "Activity");
  assert.equal(b.run("JSON.stringify(graphPosition)"), "[20,-30,1.5]");
  b.run("scope='independent'; ws=''; saveNavigation(true);");
  assert.equal(b.context.selected, "");
  assert.equal(b.run("graphPosition"), null);
});

test("expanded processes survive refresh and invalid options use defaults", () => {
  const b = browser("?tab=processes&projects=open&process-detail=one&process-detail=two&process-state=running");
  b.run("readNavigation(); saveNavigation();");
  assert.equal(b.run("expandedProcesses.size"), 2);
  assert.equal(b.run("projectsExpanded"), true);
  assert.equal(b.context.processFilters.state, "running");
  b.context.location.search = "?tab=invalid&view=other&kind=other&source=other&graph=0,0,Infinity";
  b.run("readNavigation();");
  assert.equal(b.context.scope, "workstreams");
  assert.equal(b.context.viewMode, "list");
  assert.equal(b.context.valueFilters.kind, "all");
  assert.equal(b.run("graphPosition"), null);
});
