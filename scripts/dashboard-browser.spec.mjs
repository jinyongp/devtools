import { test, expect } from "@playwright/test";
import { execFileSync } from "node:child_process";
import net from "node:net";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const repository = process.cwd();
const binary = process.env.DEVTOOLS_TEST_CLI_BINARY || join(repository, "bin", process.platform === "win32" ? "devtools.exe" : "devtools");
if (process.env.DEVTOOLS_BROWSER_EXECUTABLE) {
  test.use({ launchOptions: { executablePath: process.env.DEVTOOLS_BROWSER_EXECUTABLE } });
}

function runCLI(args, cwd, env, input = "") {
  return execFileSync(binary, args, {
    cwd,
    env,
    input,
    encoding: "utf8",
    stdio: ["pipe", "pipe", "pipe"],
    timeout: 15000,
  }).trim();
}

test("dashboard login, value mutation and navigation survive reload", async ({ page }) => {
  test.setTimeout(45000);
  const root = mkdtempSync(join(tmpdir(), "devtools-dashboard-browser-"));
  const home = join(root, "home");
  const project = join(root, "project");
  mkdirSync(home, { recursive: true });
  mkdirSync(project, { recursive: true });
  writeFileSync(join(project, "devtools.toml"), "profile = \"browser-smoke\"\n");

  const env = {
    ...process.env,
    HOME: home,
    DEVTOOLS_ISOLATED_ROOT: join(root, "isolation"),
    XDG_DATA_HOME: join(home, "data"),
    XDG_CONFIG_HOME: join(home, "config"),
    XDG_CACHE_HOME: join(home, "cache"),
  };

  try {
    const identity = JSON.parse(runCLI(["version"], project, env));
    expect(identity.data.protocol_version).toBe(8);
    if (process.env.DEVTOOLS_TEST_CLI_COMMIT) expect(identity.data.commit).toBe(process.env.DEVTOOLS_TEST_CLI_COMMIT);
    runCLI(["var", "set", "MESSAGE", "--value", "before"], project, env);
    const started = JSON.parse(runCLI(["dashboard"], project, env));
    const url = started.data.item.url;
    try {
      await page.goto(url, { waitUntil: "domcontentloaded" });
    } catch {
      throw new Error("Dashboard bootstrap navigation failed.");
    }
    await expect(page.locator("#profile")).toHaveValue("browser-smoke");
    await expect(page.locator("#title")).toHaveText("Workstreams");

    await page.locator("#values-nav").click();
    await expect(page.locator("#title")).toHaveText("Variables & secrets");
    await expect(page).toHaveURL(/profile=browser-smoke/);
    await expect(page).toHaveURL(/tab=values/);

    const edit = page.getByRole("button", { name: "Edit MESSAGE" });
    await expect(edit).toHaveText("before");
    await edit.click();
    const input = page.getByRole("textbox", { name: "New value for MESSAGE" });
    await input.fill("after");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("button", { name: "Edit MESSAGE" })).toHaveText("after");

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.locator("#profile")).toHaveValue("browser-smoke");
    await expect(page.locator("#title")).toHaveText("Variables & secrets");
    await expect(page.getByRole("button", { name: "Edit MESSAGE" })).toHaveText("after");

    await page.getByRole("button", { name: "Create bundle", exact: true }).focus();
    await page.keyboard.press("Enter");
    await page.getByLabel("Bundle name", { exact: true }).fill("frontend");
    await page.getByLabel("Value names (one per line)", { exact: true }).fill("MESSAGE");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("button", { name: "Edit members: frontend", exact: true })).toBeFocused();
    const bundles = JSON.parse(runCLI(["bundle", "list"], project, env)).data;
    expect(bundles.items[0].name).toBe("frontend");
    expect(bundles.items[0].value_ids).toHaveLength(1);
    await page.getByRole("button", { name: "Edit members: frontend", exact: true }).click();
    await page.getByLabel("Value names (one per line)", { exact: true }).fill("MESSAGE\nMISSING");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.locator("#editor-error")).toContainText("Choose existing value names");
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await page.getByRole("button", {name:"Import .env",exact:true}).click();
    await page.getByLabel("Or paste .env content",{exact:true}).fill("PUBLIC_URL=https://bundle.invalid\n");
    await page.getByLabel("Value bundle (optional)",{exact:true}).fill("frontend");
    await page.getByRole("button",{name:"Preview import",exact:true}).click();
    await expect(page.locator("#editor-fields")).toContainText("Bundle: frontend");
    await page.getByLabel("PUBLIC_URL — Store as public variable",{exact:true}).check();
    await page.getByRole("button",{name:"Import values",exact:true}).click();
    await expect(page.getByRole("button",{name:"Edit PUBLIC_URL",exact:true})).toHaveText("https://bundle.invalid");
    expect(JSON.parse(runCLI(["bundle","list"],project,env)).data.items[0].value_ids).toHaveLength(2);
    await page.getByRole("button", { name: "Rename bundle: frontend", exact: true }).click();
    const longBundle = "b".repeat(128);
    await page.getByLabel("New bundle name", {exact:true}).fill(longBundle);
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.setViewportSize({width:390,height:844});
    await expect(page.getByRole("button", { name: `Rename bundle: ${longBundle}`, exact: true })).toBeFocused();
    expect(await page.getByRole("region", {name:"Value bundles",exact:true}).evaluate(node=>node.scrollWidth<=node.clientWidth)).toBe(true);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if(process.env.DEVTOOLS_BUNDLE_SCREENSHOT_PREFIX) await page.screenshot({path:process.env.DEVTOOLS_BUNDLE_SCREENSHOT_PREFIX+"-mobile.png",fullPage:true});
    await page.setViewportSize({width:1280,height:720});
    if(process.env.DEVTOOLS_BUNDLE_SCREENSHOT_PREFIX) await page.screenshot({path:process.env.DEVTOOLS_BUNDLE_SCREENSHOT_PREFIX+"-desktop.png",fullPage:true});

    const value = JSON.parse(runCLI(["var", "get", "MESSAGE"], project, env));
    expect(value.data.value).toBe("after");
    const origin = new URL(page.url()).origin;
    const unauthorized = await page.request.get(origin + "/api/profiles");
    expect(unauthorized.status()).toBe(401);
    const foreign = await page.request.post(origin + "/api/actions", { headers: { Origin: "http://outside.invalid" }, data: {} });
    expect(foreign.status()).toBe(403);
    if (process.env.DEVTOOLS_TEST_CLI_BINARY) {
      let browserArch = "unknown";
      const browserPath = process.env.DEVTOOLS_BROWSER_EXECUTABLE;
      if (process.platform === "win32" && browserPath) {
        const image = readFileSync(browserPath);
        const machine = image.readUInt16LE(image.readUInt32LE(60) + 4);
        browserArch = ({ [0x8664]: "amd64", [0xaa64]: "arm64" })[machine] || "unknown";
        expect(browserArch).not.toBe("unknown");
      }
      writeFileSync("windows-browser-results.json", JSON.stringify({
        cli_commit: identity.data.commit, protocol: identity.data.protocol_version,
        node_arch: process.arch, browser_arch: browserArch,
        browser_version: page.context().browser().version(),
        login: "pass", mutation: "pass", reload: "pass", cli_confirmation: "pass",
        missing_session: "401", foreign_origin: "403", real_pc: "NOT RUN",
      }, null, 2) + "\n");
    }
  } finally {
    try {
      runCLI(["dashboard", "stop"], project, env);
    } catch {
      // Cleanup should not hide the primary smoke failure.
    }
    rmSync(root, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 });
  }
});


async function retireTestVault(root) {
  // This test owns the isolated root. Use its existing value-free control
  // handshake for cleanup, without inferring a daemon PID from a file.
  let endpoint;
  try { endpoint=JSON.parse(readFileSync(join(root,"data","vault","session.json"),"utf8")); }
  catch { return; }
  const socket=net.createConnection(endpoint.endpoint);
  let buffered=Buffer.alloc(0), waiting, closed=false;
  const drain=()=>{
    if(!waiting||buffered.length<4)return;
    const size=buffered.readUInt32BE(0);
    if(!size||size>131072){waiting.reject(Error("Invalid private control frame"));waiting=null;return;}
    if(buffered.length<size+4)return;
    const frame=buffered.subarray(4,size+4);buffered=buffered.subarray(size+4);
    const owner=waiting;waiting=null;
    try {owner.resolve(JSON.parse(frame.toString("utf8")));} catch {owner.reject(Error("Invalid private control response"));}
  };
  socket.on("data",chunk=>{buffered=Buffer.concat([buffered,chunk]);drain();});
  socket.on("error",()=>{if(waiting){waiting.reject(Error("Private control unavailable"));waiting=null;}});
  socket.on("close",()=>{closed=true;if(waiting){waiting.reject(Error("Private control closed"));waiting=null;}});
  socket.setTimeout(5000,()=>socket.destroy(Error("Private control timeout")));
  const frame=()=>new Promise((resolve,reject)=>{if(closed){reject(Error("Private control closed"));return;}waiting={resolve,reject};drain();});
  try {
    const hello=await frame();
    for(const key of ["protocol","vault_id","session_id","endpoint"])expect(hello[key]).toBe(endpoint[key]);
    expect(hello.control_protocol).toBe(1);
    const body=Buffer.from(JSON.stringify({protocol:hello.protocol,vault_id:hello.vault_id,session_id:hello.session_id,lock_epoch:hello.lock_epoch,challenge:hello.challenge,operation:"retire"}));
    const size=Buffer.alloc(4);size.writeUInt32BE(body.length);socket.write(Buffer.concat([size,body]));
    const result=await frame();expect(result.error).toBeFalsy();expect(result.key).toBeUndefined();
  } finally {socket.destroy();}
}

test("vault controls keep metadata passive and clear unlock input",async({page})=>{
  test.setTimeout(60000);
  const root=mkdtempSync(join(tmpdir(),"devtools-vault-browser-"));
  const isolation=join(root,"isolation"),project=join(root,"project");mkdirSync(project,{recursive:true});
  writeFileSync(join(project,"devtools.toml"),"profile='vault-browser'\n");
  const env={...process.env,DEVTOOLS_ISOLATED_ROOT:isolation};
  const passphrase="synthetic-browser-vault-passphrase";
  try {
    runCLI(["vault","init","--backend","passphrase","--passphrase-stdin"],project,env,passphrase+"\n");
    runCLI(["var","set","MESSAGE","--value","visible public value"],project,env);
    runCLI(["sec","set","TOKEN","--stdin"],project,env,"synthetic-browser-secret-canary");
    const started=JSON.parse(runCLI(["dashboard"],project,env));
    await page.goto(started.data.item.url,{waitUntil:"domcontentloaded"});
    await page.locator("#values-nav").click();
    await expect(page.getByText("Vault unlocked",{exact:true})).toBeVisible();
    await expect(page.locator("#values-panel")).not.toContainText("synthetic-browser-secret-canary");
    await page.getByRole("button",{name:"Lock vault for all profiles"}).focus();
    await page.keyboard.press("Enter");
    await expect(page.getByText("Vault manually locked",{exact:true})).toBeVisible();
    await expect(page.getByRole("button",{name:"Edit MESSAGE",exact:true})).toBeDisabled();
    const status=JSON.parse(runCLI(["vault","status"],project,env)).data.item;
    expect(status.state).toBe("locked_manual");
    await page.getByRole("button",{name:"Unlock vault",exact:true}).focus();await page.keyboard.press("Enter");
    const input=page.getByLabel("Vault passphrase",{exact:true});await expect(input).toBeFocused();
    await input.fill("synthetic-wrong-passphrase");await page.keyboard.press("Enter");
    await expect(page.locator("#editor-error")).toContainText("Passphrase cleared");await expect(input).toHaveValue("");
    await input.fill(passphrase);await page.getByRole("button",{name:"Unlock vault",exact:true}).last().click();
    await expect(page.locator("#editor")).not.toBeVisible();
    await expect(page.getByText("Vault unlocked",{exact:true})).toBeVisible();
    await expect(page.getByRole("button",{name:"Lock vault for all profiles"})).toBeFocused();
    expect(page.url()).not.toContain(passphrase);
    const storage=await page.evaluate(()=>JSON.stringify({local:{...localStorage},session:{...sessionStorage}}));
    expect(storage).not.toContain(passphrase);expect(storage).not.toContain("synthetic-browser-secret-canary");
    const before=JSON.parse(runCLI(["vault","status"],project,env)).data.item.idle_expires_at;
    await page.waitForTimeout(5500);
    expect(JSON.parse(runCLI(["vault","status"],project,env)).data.item.idle_expires_at).toBe(before);
    await page.getByRole("button",{name:"Add value",exact:true}).click();
    await page.getByLabel("Key",{exact:true}).fill("VITE_TOKEN");
    await expect(page.getByText("This name may be published by frontend builds. Keep credentials out of browser bundles.",{exact:true})).toBeVisible();
    await page.getByRole("combobox",{name:"Kind",exact:true}).selectOption("variable");
    await expect(page.getByText("This name may be published by frontend builds. Keep credentials out of browser bundles.",{exact:true})).not.toBeVisible();
    await page.getByRole("button",{name:"Cancel",exact:true}).click();
    await page.setViewportSize({width:390,height:844});
    await expect(page.getByRole("button",{name:"Lock vault for all profiles"})).toBeVisible();
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
    await page.screenshot({path:process.env.DEVTOOLS_VAULT_SCREENSHOT||join(root,"vault.png")});
  } finally {
    try {runCLI(["dashboard","stop"],project,env);}catch{}
    try {await retireTestVault(isolation);} finally {rmSync(root,{recursive:true,force:true,maxRetries:20,retryDelay:100});}
  }
});

test("process value delivery shows changes and locked verification without reading app values",async({page})=>{
  test.setTimeout(60000);
  const root=mkdtempSync(join(tmpdir(),"devtools-value-usage-browser-"));
  const project=join(root,"project"),isolation=join(root,"isolation");mkdirSync(project,{recursive:true});
  writeFileSync(join(project,"devtools.toml"),`profile = "usage-browser"\n[commands.web]\nexec = [${JSON.stringify(process.execPath)}, "-e", "setInterval(()=>{},1000)"]\ninject = true\n`);
  const env={...process.env,DEVTOOLS_ISOLATED_ROOT:isolation};
  let execution;
  try {
    runCLI(["vault","init","--backend","passphrase","--passphrase-stdin"],project,env,"synthetic-usage-browser-passphrase\n");
    runCLI(["sec","set","TOKEN","--stdin"],project,env,"synthetic-usage-browser-secret-canary");
    runCLI(["var","set","URL","--value","synthetic-usage-browser-public-before"],project,env);
    execution=JSON.parse(runCLI(["process","start","web","--request-id","00000000-0000-4000-8000-000000007001"],project,env)).data.item;
    expect(execution.input_version.slots.map(slot=>slot.name)).toEqual(["TOKEN","URL"]);
    const started=JSON.parse(runCLI(["dashboard"],project,env));
    await page.goto(started.data.item.url,{waitUntil:"domcontentloaded"});
    await page.locator("#processes-nav").click();
    await page.locator(".process-details summary").click();
    await expect(page.locator("#values-panel")).toContainText("Current values");
    await expect(page.locator("#values-panel")).toContainText("Recorded delivery: 2 values");
    await expect(page.locator("#values-panel")).toContainText("Delivery records do not show which values the application read.");
    runCLI(["var","set","URL","--value","synthetic-usage-browser-public-after"],project,env);
    await expect(page.locator("#values-panel")).toContainText("Value changes pending",{timeout:15000});
    await expect(page.locator("#values-panel")).toContainText("Background value check: pending",{timeout:15000});
    const uses=JSON.parse(runCLI(["values","uses"],project,env)).data.items;
    expect(uses.find(item=>item.execution_id===execution.id).values.state).toBe("pending");
    runCLI(["vault","lock"],project,env);
    await expect(page.locator("#values-panel")).toContainText("Value verification pending",{timeout:15000});
    await expect(page.locator("#values-panel")).toContainText("Background value check: verification pending",{timeout:15000});
    for(const canary of ["synthetic-usage-browser-secret-canary","synthetic-usage-browser-public-before","synthetic-usage-browser-public-after"])await expect(page.locator("#values-panel")).not.toContainText(canary);
    const stillRunning=JSON.parse(runCLI(["process","status",execution.id],project,env)).data;
    expect(stillRunning.item.id).toBe(execution.id);expect(stillRunning.item.ended_at).toBeNull();
    expect(stillRunning.values.state).toBe("verification_pending");
  } finally {
    try {if(execution)runCLI(["process","stop",execution.id,"--request-id","00000000-0000-4000-8000-000000007002"],project,env);} finally {
      try {runCLI(["dashboard","stop"],project,env);}catch{}
      try {await retireTestVault(isolation);} finally {rmSync(root,{recursive:true,force:true,maxRetries:20,retryDelay:100});}
    }
  }
});

test("automatic value replacement reports delivery and pauses after preparation failure",async({page})=>{
  test.setTimeout(60000);
  const root=mkdtempSync(join(tmpdir(),"devtools-auto-browser-"));
  const project=join(root,"project"),isolation=join(root,"isolation");mkdirSync(project,{recursive:true});
  const config=join(project,"devtools.toml");
  const write=(missing=false)=>writeFileSync(config,`profile = "auto-browser"\n[commands.web]\nexec = ${missing?'["missing-auto-browser-executable"]':`[${JSON.stringify(process.execPath)}, "-e", "setInterval(()=>{},1000)"]`}\ninject = true\nvalues_update = "restart"\n`);
  write();
  const env={...process.env,DEVTOOLS_ISOLATED_ROOT:isolation};
  try {
    runCLI(["vault","init","--backend","passphrase","--passphrase-stdin"],project,env,"synthetic-auto-browser-passphrase\n");
    runCLI(["sec","set","TOKEN","--stdin"],project,env,"synthetic-auto-browser-before");
    const first=JSON.parse(runCLI(["process","start","web","--request-id","00000000-0000-4000-8000-000000007101"],project,env)).data.item;
    const dashboard=JSON.parse(runCLI(["dashboard"],project,env));
    await page.goto(dashboard.data.item.url,{waitUntil:"domcontentloaded"});
    await page.locator("#processes-nav").click();
    await page.locator(".process-details summary").click();
    runCLI(["sec","set","TOKEN","--stdin"],project,env,"synthetic-auto-browser-after");
    await expect(page.locator("#values-panel")).toContainText("Automatic value update: applied",{timeout:15000});
    const prior=JSON.parse(runCLI(["process","status",first.id],project,env)).data.item;
    expect(prior.value_apply.next_id).toBeTruthy();expect(prior.ended_at).not.toBeNull();
    write(true);runCLI(["sec","set","TOKEN","--stdin"],project,env,"synthetic-auto-browser-third");
    await expect.poll(()=>JSON.parse(runCLI(["process","status",prior.value_apply.next_id],project,env)).data.item.value_apply?.state).toBe("failed");
    for(const details of await page.locator(".process-details").all())if(await details.getAttribute("open")===null)await details.locator("summary").click();
    await expect(page.locator("#values-panel")).toContainText("Automatic updates paused. Fix the cause and use Restart to retry explicitly.",{timeout:15000});
    for(const secret of ["synthetic-auto-browser-before","synthetic-auto-browser-after","synthetic-auto-browser-third"])await expect(page.locator("#values-panel")).not.toContainText(secret);
  } finally {
    try {runCLI(["project","down","--request-id","00000000-0000-4000-8000-000000007102"],project,env);}catch{}
    try {runCLI(["dashboard","stop"],project,env);}catch{}
    try {await retireTestVault(isolation);}finally {rmSync(root,{recursive:true,force:true,maxRetries:20,retryDelay:100});}
  }
});
