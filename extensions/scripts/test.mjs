import { readdir, mkdtemp, mkdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const extensions = fileURLToPath(new URL("../", import.meta.url));
async function tests(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name === ".git") continue;
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await tests(path));
    else if (entry.name.endsWith(".test.ts")) files.push(path);
  }
  return files.sort();
}
const temporary = await mkdtemp(join(tmpdir(), "pi-capabilities-tests-"));
try {
  const home = join(temporary, "home"), agent = join(temporary, "agent");
  await Promise.all([mkdir(home), mkdir(agent)]);
  // Do not inherit provider keys, integration credentials, NODE_OPTIONS, or
  // frontend/session markers from the caller's active Pi process.
  const env = { HOME: home, USERPROFILE: home, PI_CODING_AGENT_DIR: agent, PI_WORKER_DEPTH: "0" };
  for (const key of ["PATH", "SystemRoot", "SYSTEMROOT", "COMSPEC", "PATHEXT", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "TERM", "CI"]) {
    if (process.env[key] !== undefined) env[key] = process.env[key];
  }
  const result = spawnSync(process.execPath, ["--experimental-transform-types", "--test", ...await tests(extensions)], {
    // Historical Worker fixtures resolve extensions/worker from repository cwd.
    cwd: dirname(extensions.replace(/[\\/]$/, "")), env, stdio: "inherit",
  });
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
} finally {
  await rm(temporary, { recursive: true, force: true });
}
