import assert from "node:assert/strict";
import { readFileSync, realpathSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { isAbsolute, relative, resolve } from "node:path";
import test from "node:test";

const root = fileURLToPath(new URL("./", import.meta.url));
const manifest = JSON.parse(readFileSync(new URL("package.json", import.meta.url), "utf8"));
const lock = JSON.parse(readFileSync(new URL("package-lock.json", import.meta.url), "utf8"));

test("package resources are explicit, relative, present and never user configuration", () => {
  assert.equal(manifest.private, true);
  assert.equal(manifest.pi.extensions.length, 8);
  const paths = [...manifest.pi.extensions, ...manifest.pi.skills];
  assert.equal(new Set(paths).size, paths.length);
  for (const path of paths) {
    assert.equal(isAbsolute(path), false, path);
    assert.ok(existsSync(resolve(root, path)), path);
    assert.ok(!relative(realpathSync(root), realpathSync(resolve(root, path))).startsWith(".."), path);
    assert.doesNotMatch(path, /(?:settings|auth|models)\.json|\.test\./);
  }
  assert.ok(manifest.pi.extensions.includes("./pi-ui/index.ts"));
  assert.equal(manifest.pi.extensions.filter((path: string) => path.startsWith("./pi-ui/")).length, 1);
  assert.ok(!manifest.main);
  for (const name of ["preinstall", "install", "postinstall", "prepare"]) assert.equal(manifest.scripts[name], undefined);
});

test("manifest and lock metadata match without changing dependency versions", () => {
  assert.equal(manifest.name, lock.name);
  for (const key of ["name", "version", "license", "engines", "dependencies", "devDependencies"]) {
    assert.deepEqual(manifest[key], lock.packages[""][key], key);
  }
});
