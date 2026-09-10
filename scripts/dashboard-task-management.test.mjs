import {test} from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";

const source = readFileSync(new URL("../internal/dashboard/assets/task-management.js", import.meta.url), "utf8");

test("workstream metadata editor uses previewed atomic edit", () => {
  assert.match(source, /node\.kind === "workstream"[^]*Edit workstream metadata/);
  assert.match(source, /editPlan\("Update workstream metadata", \[\{op:"workstream\.update", value:body\}\]\)/);
  assert.match(source, /const title = field\(p, "Title", item\.title\), description = field\(p, "Description", item\.description, "textarea"\); title\.required = true/);
});
