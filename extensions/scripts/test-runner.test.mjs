import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cp, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { collectTests } from "./test.mjs";

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "pi-test-discovery-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  return root;
}

test("default test discovery includes TypeScript and SoL mjs, not runners or dependencies", async t => {
  const root = await fixture(t);
  const names = ["ordinary.test.ts", "sol-pi/tests/session.test.mjs", "sol-pi/tests/run-global.mjs",
    "helper.mjs", "node_modules/ignored.test.ts", ".git/ignored.test.mjs"];
  for (const name of names) {
    const path = join(root, name);
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, "");
  }
  assert.deepEqual(await collectTests(root), [join(root, "ordinary.test.ts"), join(root, "sol-pi/tests/session.test.mjs")]);
});

test("importing discovery does not start another test run", () => {
  const result = spawnSync(process.execPath, ["--input-type=module", "-e",
    `await import(${JSON.stringify(new URL("./test.mjs", import.meta.url).href)}); console.log('discovery-only')`],
  { encoding: "utf8", timeout: 10_000 });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout.trim(), "discovery-only");
});

test("a failing mjs suite makes the default runner fail in an isolated environment", async t => {
  const root = await fixture(t);
  await mkdir(join(root, "scripts"));
  await cp(new URL("./test.mjs", import.meta.url), join(root, "scripts/test.mjs"));
  await writeFile(join(root, "failure.test.mjs"), `
import assert from 'node:assert/strict';
import { test } from 'node:test';
test('runner strips inherited credentials', () => {
  assert.equal(process.env.TEST_PROVIDER_SECRET, undefined);
  assert.equal(process.env.PI_OFFLINE, '1');
  assert.notEqual(process.env.HOME, ${JSON.stringify(process.env.HOME)});
});
test('intentional failure is propagated', () => { throw new Error('mjs-failure-propagated'); });
`);
  const result = spawnSync(process.execPath, [join(root, "scripts/test.mjs")], {
    env: { ...process.env, TEST_PROVIDER_SECRET: "not-a-real-secret" }, encoding: "utf8", timeout: 20_000,
  });
  assert.equal(result.status, 1, result.stderr);
  assert.match(result.stdout, /ok 1 - runner strips inherited credentials/);
  assert.match(result.stdout, /not ok 2 - intentional failure is propagated/);
  assert.match(result.stdout, /mjs-failure-propagated/);
});
