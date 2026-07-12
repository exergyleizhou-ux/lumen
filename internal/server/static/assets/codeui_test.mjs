#!/usr/bin/env node
import fs from "fs";
import vm from "vm";

const source = fs.readFileSync(new URL("./app.js", import.meta.url), "utf8");
const marker = "window.CodeUI = { buildWorkbenchSnapshotV2, workbenchTargetOrigin };";
const end = source.indexOf(marker);
if (end < 0) throw new Error("CodeUI export missing");
const sandbox = { window: { location: { origin: "https://lumen.test" } }, location: { pathname: "/" }, document: { getElementById: () => null }, URL };
vm.createContext(sandbox);
vm.runInContext(source.slice(0, end + marker.length), sandbox);
const build = sandbox.window.CodeUI.buildWorkbenchSnapshotV2;
const snapshot = build({
  workspace_id: "workspace-a", run_id: "run-a", last_seq: 4,
  status: "succeeded", pending_approvals: 2, verification: "passed", artifact_count: 3,
  prompt: "SECRET", reasoning: "SECRET", args: "SECRET", key: "SECRET", content: "SECRET",
});
const expected = {
  kind: "lumen.workbench.snapshot", version: 2, surface: "code",
  workspace: { id: "workspace-a" }, project: null,
  run: { id: "run-a", last_seq: 4, status: "succeeded", terminal: true },
  pending_approvals: 2, verification: "passed", artifact_count: 3,
};
if (JSON.stringify(snapshot) !== JSON.stringify(expected)) throw new Error("strict snapshot mismatch: " + JSON.stringify(snapshot));
for (const forbidden of ["prompt", "reasoning", "args", "key", "content", "SECRET"]) {
  if (JSON.stringify(snapshot).includes(forbidden)) throw new Error("snapshot leaked " + forbidden);
}
if (sandbox.window.CodeUI.workbenchTargetOrigin() !== "https://lumen.test") throw new Error("same-origin fallback failed");
sandbox.window.__LUMEN_WORKBENCH_ORIGIN__ = "https://oasis.test";
if (sandbox.window.CodeUI.workbenchTargetOrigin() !== "https://oasis.test") throw new Error("configured target failed");
sandbox.window.__LUMEN_WORKBENCH_ORIGIN__ = "*";
if (sandbox.window.CodeUI.workbenchTargetOrigin() !== "https://lumen.test") throw new Error("wildcard must fail closed");
console.log("OK Code WorkbenchSnapshotV2 strict whitelist");
