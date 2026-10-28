import { defineConfig } from "@playwright/test";

// Restrict discovery to this checkout, including when ignored worktrees exist.
export default defineConfig({
  testDir: "./scripts",
  testMatch: "dashboard-browser.spec.mjs",
});
