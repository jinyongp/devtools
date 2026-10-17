import { test, expect } from "@playwright/test";
import { execFileSync } from "node:child_process";
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
    expect(identity.data.protocol_version).toBe(7);
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
