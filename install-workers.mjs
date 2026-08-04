#!/usr/bin/env node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const args = process.argv.slice(2);
function option(name) {
  const index = args.indexOf(name);
  return index >= 0 ? args[index + 1] : undefined;
}

const source = path.resolve(option("--source") || scriptDir);
const target = path.resolve(option("--target") || process.env.PI_CODING_AGENT_DIR || path.join(os.homedir(), ".pi", "agent"));
const files = [
  "extensions/worker.ts",
  "skills/worker-orchestration/SKILL.md",
  "agents/worker.md",
  "README-workers.md",
  "install-workers.mjs"
];

function mergeMissing(current, defaults) {
  if (Array.isArray(defaults)) return current === undefined ? defaults : current;
  if (defaults && typeof defaults === "object") {
    const result = current && typeof current === "object" && !Array.isArray(current) ? { ...current } : {};
    for (const [key, value] of Object.entries(defaults)) result[key] = mergeMissing(result[key], value);
    return result;
  }
  return current === undefined ? defaults : current;
}

fs.mkdirSync(target, { recursive: true });
const created = [];
const preserved = [];
for (const relative of files) {
  const from = path.join(source, relative);
  const to = path.join(target, relative);
  if (!fs.existsSync(from)) throw new Error(`安装源缺少 ${from}`);
  if (fs.existsSync(to)) {
    preserved.push(relative);
    continue;
  }
  fs.mkdirSync(path.dirname(to), { recursive: true });
  fs.copyFileSync(from, to, fs.constants.COPYFILE_EXCL);
  fs.chmodSync(to, relative === "install-workers.mjs" ? 0o755 : 0o644);
  created.push(relative);
}

const sourceRoutingPath = path.join(source, "worker-routing.json");
const targetRoutingPath = path.join(target, "worker-routing.json");
if (!fs.existsSync(sourceRoutingPath)) throw new Error(`安装源缺少 ${sourceRoutingPath}`);
const defaults = JSON.parse(fs.readFileSync(sourceRoutingPath, "utf8"));
if (!fs.existsSync(targetRoutingPath)) {
  fs.writeFileSync(targetRoutingPath, `${JSON.stringify(defaults, null, 2)}\n`, { mode: 0o600 });
  created.push("worker-routing.json");
} else {
  let current;
  try {
    current = JSON.parse(fs.readFileSync(targetRoutingPath, "utf8"));
  } catch (error) {
    throw new Error(`目标 worker-routing.json 无法解析；原文件未修改：${error instanceof Error ? error.message : String(error)}`);
  }
  const merged = mergeMissing(current, defaults);
  if (JSON.stringify(merged) !== JSON.stringify(current)) {
    const temp = `${targetRoutingPath}.tmp-${process.pid}`;
    fs.writeFileSync(temp, `${JSON.stringify(merged, null, 2)}\n`, { mode: 0o600 });
    fs.renameSync(temp, targetRoutingPath);
    created.push("worker-routing.json（仅补充缺失字段）");
  } else preserved.push("worker-routing.json");
}

console.log(JSON.stringify({ target, created, preserved }, null, 2));
