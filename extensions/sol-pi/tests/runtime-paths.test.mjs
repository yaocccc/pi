import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import test from "node:test";
import { SessionManager, SettingsManager, VERSION } from "@earendil-works/pi-coding-agent";
import { runtimeRoot } from "../runtime-paths.ts";

const context = sessionManager => ({ sessionManager });
async function sandbox(t) {
  const dir = await mkdtemp(join(tmpdir(), "runtime-paths-test-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  return dir;
}
function temporaryRoot(t, manager) {
  const root = runtimeRoot(context(manager));
  t.after(() => rm(root, { recursive: true, force: true }));
  return root;
}

test("Pi 1.0.2 persistent session/resume paths are unchanged and lazily created", async t => {
  assert.equal(VERSION, "1.0.4");
  const cwd = await sandbox(t);
  const manager = SessionManager.create(cwd, join(cwd, "sessions"));
  const root = runtimeRoot(context(manager));
  assert.equal(root, join(manager.getSessionDir(), "sol-pi", manager.getSessionId()));
  await assert.rejects(stat(root), { code: "ENOENT" });
  manager.appendMessage({ role: "user", content: "persistent fixture", timestamp: 1 });
  const resumed = SessionManager.open(manager.getSessionFile());
  assert.equal(runtimeRoot(context(resumed)), root);
  manager.newSession();
  assert.notEqual(runtimeRoot(context(manager)), root);
});

test("real SDK in-memory roots are private, stable across contexts and retained across newSession", async t => {
  const cwd = await sandbox(t);
  const manager = SessionManager.inMemory(cwd);
  assert.equal(manager.getSessionDir(), "", "must not silently use the default persistent session directory");
  assert.equal(manager.getSessionFile(), undefined);
  assert.equal(manager.isPersisted(), false);
  const oldId = manager.getSessionId();
  const root = temporaryRoot(t, manager);
  assert.equal(dirname(root), tmpdir());
  assert.ok((await stat(root)).isDirectory());
  if (process.platform !== "win32") assert.equal((await stat(root)).mode & 0o777, 0o700);
  assert.equal(runtimeRoot(context(manager)), root);
  assert.equal(runtimeRoot(context(SessionManager.inMemory(cwd, { id: oldId }))), root);
  await writeFile(join(root, "retained.txt"), "parent-readable fixture");
  manager.newSession();
  assert.notEqual(temporaryRoot(t, manager), root);
  assert.equal(await readFile(join(root, "retained.txt"), "utf8"), "parent-readable fixture");
  assert.notEqual(temporaryRoot(t, SessionManager.inMemory(cwd)), root);
  await assert.rejects(stat(join(cwd, "sol-pi")), { code: "ENOENT" });
});

test("real CLI --no-session construction ignores even a nonempty configured sessionDir", async t => {
  const cwd = await sandbox(t);
  // Exercise the installed CLI factory, not a fake getSessionDir implementation.
  const sdkEntry = fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent"));
  const { createSessionManager } = await import(pathToFileURL(join(dirname(sdkEntry), "main.js")));
  const configured = join(cwd, "never-create-persistent-sessions");
  const settings = SettingsManager.inMemory({ sessionDir: configured });
  const manager = await createSessionManager({ noSession: true }, cwd, configured, settings);
  assert.equal(manager.getSessionDir(), "");
  assert.equal(manager.getSessionFile(), undefined);
  assert.equal(manager.isPersisted(), false);
  assert.equal(dirname(temporaryRoot(t, manager)), tmpdir());
  await assert.rejects(stat(configured), { code: "ENOENT" });
});

test("temporary roots are unpredictable across processes even for the same explicit ID", async t => {
  const dir = await sandbox(t);
  const script = `import { runtimeRoot } from ${JSON.stringify(new URL("../runtime-paths.ts", import.meta.url).href)};
    console.log(runtimeRoot({ sessionManager: { getSessionDir: () => "", getSessionId: () => "same-id" } }));`;
  const roots = [0, 1].map(() => execFileSync(process.execPath, ["--experimental-strip-types", "--input-type=module", "-e", script], {
    encoding: "utf8", env: { ...process.env, TMPDIR: dir, TMP: dir, TEMP: dir },
  }).trim());
  assert.notEqual(roots[0], roots[1]);
  for (const root of roots) {
    assert.equal(dirname(root), dir);
    assert.match(root, /sol-pi-same-id-[a-zA-Z0-9]+$/);
    assert.ok((await stat(root)).isDirectory(), "not automatically removed on process exit; parents may read it");
  }
});

test("unsafe session IDs are rejected before any persistent/temporary path is used", () => {
  for (const id of ["", ".", "..", "../escape", "nested/session", "nested\\session", "/absolute", "a\0b"]) {
    for (const dir of ["sessions", ""]) {
      assert.throws(() => runtimeRoot({ sessionManager: { getSessionDir: () => dir, getSessionId: () => id } }), /safe Pi session id/);
    }
  }
});
