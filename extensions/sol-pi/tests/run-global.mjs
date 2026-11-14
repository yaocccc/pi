// Isolated copy + symlinks to the EXISTING global Pi dependencies. Never installs or edits packages.
import { execFileSync, spawnSync } from "node:child_process";
import { access, cp, mkdir, mkdtemp, readdir, readFile, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = process.env.SOL_PI_GLOBAL_ROOT ?? execFileSync("npm", ["root", "-g"], { encoding: "utf8" }).trim();
const pi = join(root, "@earendil-works/pi-coding-agent");
const { version } = JSON.parse(await readFile(join(pi, "package.json"), "utf8"));
// Behavior is checked by the native differential/session suites, not an old
// exact-version gate that prevents those assertions from running.
if (typeof version !== "string" || !version) throw new Error("Installed Pi package has no version");
const dir = await mkdtemp(join(tmpdir(), "sol-pi-global-"));
try {
  const modules = join(dir, "node_modules");
  await mkdir(modules);
  for (const item of await readdir(join(pi, "node_modules"), { withFileTypes: true })) {
    if (item.name === ".bin") continue;
    if (item.name.startsWith("@")) {
      await mkdir(join(modules, item.name));
      for (const child of await readdir(join(pi, "node_modules", item.name))) {
        await symlink(join(pi, "node_modules", item.name, child), join(modules, item.name, child));
      }
    } else await symlink(join(pi, "node_modules", item.name), join(modules, item.name));
  }
  await mkdir(join(modules, "@earendil-works"), { recursive: true });
  await symlink(pi, join(modules, "@earendil-works/pi-coding-agent"));
  const source = resolve(dirname(fileURLToPath(import.meta.url)), "..");
  await cp(source, join(dir, "sol-pi"), { recursive: true });
  await cp(join(source, "../shared"), join(dir, "shared"), { recursive: true });
  const requested = process.argv.slice(2);
  let args;
  if (requested[0] === "--typecheck") {
    let compiler;
    for (const candidate of [join(root, "typescript/lib/tsc.js"), join(root, "ts-node/node_modules/typescript/lib/tsc.js")]) {
      try { await access(candidate); compiler = candidate; break; } catch {}
    }
    if (!compiler) throw new Error("No existing global TypeScript compiler; nothing was installed");
    args = [compiler, "--noEmit", "--strict", "--skipLibCheck", "--target", "ES2022", "--module", "ESNext",
      "--moduleResolution", "bundler", "--allowImportingTsExtensions", "--types", "node", join(dir, "sol-pi/index.ts")];
    console.log(`Global Pi ${version}; isolated strict TypeScript check`);
  } else {
    const available = (await readdir(join(dir, "sol-pi/tests"))).filter(name => name.endsWith(".test.mjs")).sort();
    const files = requested.length ? requested : available;
    if (files.some(name => !available.includes(name))) throw new Error("Select existing SoL .test.mjs filenames only");
    console.log(`Global Pi ${version}; isolated offline tests (${files.length} files)`);
    args = ["--experimental-transform-types", "--test", ...files.map(name => join(dir, "sol-pi/tests", name))];
  }
  // Do not forward provider keys, live-session markers or NODE_OPTIONS.
  const home = join(dir, "home"), agent = join(dir, "agent");
  await Promise.all([mkdir(home), mkdir(agent)]);
  const env = { HOME: home, USERPROFILE: home, PI_WORKER_DEPTH: "0", PI_CODING_AGENT_DIR: agent, PI_OFFLINE: "1" };
  for (const key of ["PATH", "SystemRoot", "SYSTEMROOT", "COMSPEC", "PATHEXT", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "TERM", "CI"]) {
    if (process.env[key] !== undefined) env[key] = process.env[key];
  }
  const result = spawnSync(process.execPath, args, {
    env, stdio: "inherit", cwd: dir, timeout: 120_000,
  });
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
} finally { await rm(dir, { recursive: true, force: true }); }
