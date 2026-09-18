import { test, expect } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const repository = process.cwd();
const binary = join(repository, "bin", "devtools");

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
    XDG_DATA_HOME: join(home, "data"),
    XDG_CONFIG_HOME: join(home, "config"),
    XDG_CACHE_HOME: join(home, "cache"),
  };

  try {
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
  } finally {
    try {
      runCLI(["dashboard", "stop"], project, env);
    } catch {
      // Cleanup should not hide the primary smoke failure.
    }
    rmSync(root, { recursive: true, force: true });
  }
});
